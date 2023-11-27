package event_reporter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj/argo-cd/v2/common"
	codefresh "github.com/argoproj/argo-cd/v2/event_reporter/codefresh"
	event_reporter "github.com/argoproj/argo-cd/v2/event_reporter/controller"
	"github.com/argoproj/argo-cd/v2/event_reporter/metrics"
	sharding "github.com/argoproj/argo-cd/v2/event_reporter/sharding"
	applicationpkg "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	appinformer "github.com/argoproj/argo-cd/v2/pkg/client/informers/externalversions"
	applisters "github.com/argoproj/argo-cd/v2/pkg/client/listers/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v2/reposerver/apiclient"
	servercache "github.com/argoproj/argo-cd/v2/server/cache"
	"github.com/argoproj/argo-cd/v2/server/rbacpolicy"
	"github.com/argoproj/argo-cd/v2/server/repository"
	"github.com/argoproj/argo-cd/v2/util/assets"
	cacheutil "github.com/argoproj/argo-cd/v2/util/cache"
	"github.com/argoproj/argo-cd/v2/util/db"
	errorsutil "github.com/argoproj/argo-cd/v2/util/errors"
	"github.com/argoproj/argo-cd/v2/util/healthz"
	"github.com/argoproj/argo-cd/v2/util/io"
	"github.com/argoproj/argo-cd/v2/util/rbac"
	settings_util "github.com/argoproj/argo-cd/v2/util/settings"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// catches corrupted informer state; see https://github.com/argoproj/argo-cd/issues/4960 for more information
	notObjectErrMsg = "object does not implement the Object interfaces"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

type EventReporterServer struct {
	EventReporterServerOpts

	settings       *settings_util.ArgoCDSettings
	log            *log.Entry
	settingsMgr    *settings_util.SettingsManager
	enf            *rbac.Enforcer
	projInformer   cache.SharedIndexInformer
	projLister     applisters.AppProjectNamespaceLister
	policyEnforcer *rbacpolicy.RBACPolicyEnforcer
	appInformer    cache.SharedIndexInformer
	appLister      applisters.ApplicationLister
	db             db.ArgoDB

	// stopCh is the channel which when closed, will shutdown the Argo CD server
	stopCh     chan struct{}
	serviceSet *EventReporterServerSet
}

type EventReporterServerSet struct {
	RepoService   *repository.Server
	MetricsServer *metrics.MetricsServer
}

type EventReporterServerOpts struct {
	ListenPort               int
	ListenHost               string
	MetricsPort              int
	MetricsHost              string
	Namespace                string
	KubeClientset            kubernetes.Interface
	AppClientset             appclientset.Interface
	RepoClientset            repoapiclient.Clientset
	ApplicationServiceClient applicationpkg.ApplicationServiceClient
	Cache                    *servercache.Cache
	RedisClient              *redis.Client
	ApplicationNamespaces    []string
	BaseHRef                 string
	RootPath                 string
	CodefreshConfig          *codefresh.CodefreshConfig
}

type handlerSwitcher struct {
	handler              http.Handler
	urlToHandler         map[string]http.Handler
	contentTypeToHandler map[string]http.Handler
}

type Listeners struct {
	Main    net.Listener
	Metrics net.Listener
}

func (l *Listeners) Close() error {
	if l.Main != nil {
		if err := l.Main.Close(); err != nil {
			return err
		}
		l.Main = nil
	}
	if l.Metrics != nil {
		if err := l.Metrics.Close(); err != nil {
			return err
		}
		l.Metrics = nil
	}
	return nil
}

func (s *handlerSwitcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if urlHandler, ok := s.urlToHandler[r.URL.Path]; ok {
		urlHandler.ServeHTTP(w, r)
	} else if contentHandler, ok := s.contentTypeToHandler[r.Header.Get("content-type")]; ok {
		contentHandler.ServeHTTP(w, r)
	} else {
		s.handler.ServeHTTP(w, r)
	}
}

func (a *EventReporterServer) healthCheck(r *http.Request) error {
	if val, ok := r.URL.Query()["full"]; ok && len(val) > 0 && val[0] == "true" {
		argoDB := db.NewDB(a.Namespace, a.settingsMgr, a.KubeClientset)
		_, err := argoDB.ListClusters(r.Context())
		if err != nil && strings.Contains(err.Error(), notObjectErrMsg) {
			return err
		}
	}
	return nil
}

// Init starts informers used by the API server
func (a *EventReporterServer) Init(ctx context.Context) {
	go a.appInformer.Run(ctx.Done())
	svcSet := newEventReporterServiceSet(a)
	a.serviceSet = svcSet
}

func (a *EventReporterServer) RunController(ctx context.Context) {
	running := false
	controllerCtx, cancel := context.WithCancel(ctx)
	controller := event_reporter.NewEventReporterController(a.appInformer, a.Cache, a.settingsMgr, a.ApplicationServiceClient, a.appLister, a.CodefreshConfig, a.serviceSet.MetricsServer)
	tick := time.Tick(5 * time.Second)

	for {
		select {
		case <-tick:
			{
				rVersion, err := a.settingsMgr.GetCodefreshReporterVersion()
				if !running && rVersion == string(settings_util.CodefreshV2ReporterVersion) {
					controllerCtx, cancel = context.WithCancel(ctx)
					log.Warnf("Reporter parameter (%s) detected - starting controller", rVersion)
					go controller.Run(controllerCtx)
					running = true
				}
				if running == true && err == nil && isOldReporterVersion(rVersion) {
					log.Warnf("Stopping reporter because version param changed to %s or missing", settings_util.CodefreshV1ReporterVersion)
					cancel()
					running = false
				}
			}
		}
	}
}

// newHTTPServer returns the HTTP server to serve HTTP/HTTPS requests. This is implemented
// using grpc-gateway as a proxy to the gRPC server.
func (a *EventReporterServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	endpoint := fmt.Sprintf("localhost:%d", port)
	mux := http.NewServeMux()
	httpS := http.Server{
		Addr: endpoint,
		Handler: &handlerSwitcher{
			handler: mux,
		},
	}

	healthz.ServeHealthCheck(mux, a.healthCheck)

	mux.HandleFunc("/app-distribution", a.getAppDistribution)

	return &httpS
}

func (a *EventReporterServer) checkServeErr(name string, err error) {
	if err != nil {
		if a.stopCh == nil {
			// a nil stopCh indicates a graceful shutdown
			log.Infof("graceful shutdown %s: %v", name, err)
		} else {
			log.Fatalf("%s: %v", name, err)
		}
	} else {
		log.Infof("graceful shutdown %s", name)
	}
}

func (a *EventReporterServer) getAppDistribution(w http.ResponseWriter, r *http.Request) {
	shardings := []string{""}
	shardingsParam := r.URL.Query().Get("shardings")
	if shardingsParam != "" {
		shardings = strings.Split(shardingsParam, ",")
	}

	apps, err := a.ApplicationServiceClient.List(r.Context(), &applicationpkg.ApplicationQuery{})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	shardingInstance := sharding.NewSharding()

	type ShardingAlgorithmData struct {
		Distribution map[string]int `json:"distribution"`
		Apps         map[string]int `json:"apps"`
	}

	res := map[string]ShardingAlgorithmData{}

	for _, shardingAlgorithm := range shardings {
		distributionMap := make(map[string]int)
		appsMap := make(map[string]int)
		distributionFunction := shardingInstance.GetDistributionFunction(shardingAlgorithm)

		for _, app := range apps.Items {
			expectedShard := distributionFunction(&app)
			distributionMap[strconv.Itoa(expectedShard)] += 1
			appsMap[app.QualifiedName()] = expectedShard
		}

		shardingAlgorithmDisplayName := shardingAlgorithm
		if shardingAlgorithm == "" {
			shardingAlgorithmDisplayName = "default"
		}

		res[shardingAlgorithmDisplayName] = ShardingAlgorithmData{
			Distribution: distributionMap,
			Apps:         appsMap,
		}
	}

	jsonBytes, err := json.Marshal(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonBytes)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func startListener(host string, port int) (net.Listener, error) {
	var conn net.Listener
	var realErr error
	_ = wait.ExponentialBackoff(backoff, func() (bool, error) {
		conn, realErr = net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if realErr != nil {
			return false, nil
		}
		return true, nil
	})
	return conn, realErr
}

func (a *EventReporterServer) Listen() (*Listeners, error) {
	mainLn, err := startListener(a.ListenHost, a.ListenPort)
	if err != nil {
		return nil, err
	}
	metricsLn, err := startListener(a.MetricsHost, a.MetricsPort)
	if err != nil {
		io.Close(mainLn)
		return nil, err
	}
	return &Listeners{Main: mainLn, Metrics: metricsLn}, nil
}

// Run runs the API Server
// We use k8s.io/code-generator/cmd/go-to-protobuf to generate the .proto files from the API types.
// k8s.io/ go-to-protobuf uses protoc-gen-gogo, which comes from gogo/protobuf (a fork of
// golang/protobuf).
func (a *EventReporterServer) Run(ctx context.Context, lns *Listeners) {
	var httpS = a.newHTTPServer(ctx, a.ListenPort)
	tlsConfig := tls.Config{}
	tlsConfig.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return a.settings.Certificate, nil
	}
	go func() { a.checkServeErr("httpS", httpS.Serve(lns.Main)) }()
	go func() { a.checkServeErr("metrics", a.serviceSet.MetricsServer.Serve(lns.Metrics)) }()
	go a.RunController(ctx)

	if !cache.WaitForCacheSync(ctx.Done(), a.projInformer.HasSynced, a.appInformer.HasSynced) {
		log.Fatal("Timed out waiting for project cache to sync")
	}

	a.stopCh = make(chan struct{})
	<-a.stopCh
}

func isOldReporterVersion(reporterVersion string) bool {
	return reporterVersion == "" || reporterVersion == string(settings_util.CodefreshV1ReporterVersion)
}

// NewServer returns a new instance of the Argo CD API server
func NewEventReporterServer(ctx context.Context, opts EventReporterServerOpts) *EventReporterServer {
	settingsMgr := settings_util.NewSettingsManager(ctx, opts.KubeClientset, opts.Namespace)
	settings, err := settingsMgr.InitializeSettings(true)
	errorsutil.CheckError(err)

	appInformerNs := opts.Namespace
	if len(opts.ApplicationNamespaces) > 0 {
		appInformerNs = ""
	}
	projFactory := appinformer.NewSharedInformerFactoryWithOptions(opts.AppClientset, 0, appinformer.WithNamespace(opts.Namespace), appinformer.WithTweakListOptions(func(options *metav1.ListOptions) {}))
	appFactory := appinformer.NewSharedInformerFactoryWithOptions(opts.AppClientset, 0, appinformer.WithNamespace(appInformerNs), appinformer.WithTweakListOptions(func(options *metav1.ListOptions) {}))

	projInformer := projFactory.Argoproj().V1alpha1().AppProjects().Informer()
	projLister := projFactory.Argoproj().V1alpha1().AppProjects().Lister().AppProjects(opts.Namespace)

	appInformer := appFactory.Argoproj().V1alpha1().Applications().Informer()
	appLister := appFactory.Argoproj().V1alpha1().Applications().Lister()

	enf := rbac.NewEnforcer(opts.KubeClientset, opts.Namespace, common.ArgoCDRBACConfigMapName, nil)
	enf.EnableEnforce(false)
	err = enf.SetBuiltinPolicy(assets.BuiltinPolicyCSV)
	errorsutil.CheckError(err)
	enf.EnableLog(os.Getenv(common.EnvVarRBACDebug) == "1")

	policyEnf := rbacpolicy.NewRBACPolicyEnforcer(enf, projLister)
	enf.SetClaimsEnforcerFunc(policyEnf.EnforceClaims)

	dbInstance := db.NewDB(opts.Namespace, settingsMgr, opts.KubeClientset)

	server := &EventReporterServer{
		EventReporterServerOpts: opts,
		log:                     log.NewEntry(log.StandardLogger()),
		settings:                settings,
		settingsMgr:             settingsMgr,
		enf:                     enf,
		projInformer:            projInformer,
		projLister:              projLister,
		appInformer:             appInformer,
		appLister:               appLister,
		policyEnforcer:          policyEnf,
		db:                      dbInstance,
	}

	if err != nil {
		// Just log. It's not critical.
		log.Warnf("Failed to log in-cluster warnings: %v", err)
	}

	return server
}

func newEventReporterServiceSet(a *EventReporterServer) *EventReporterServerSet {
	repoService := repository.NewServer(a.RepoClientset, a.db, a.enf, a.Cache, a.appLister, a.projInformer, a.Namespace, a.settingsMgr)
	metricsServer := metrics.NewMetricsServer(a.MetricsHost, a.MetricsPort)
	if a.RedisClient != nil {
		cacheutil.CollectMetrics(a.RedisClient, metricsServer)
	}

	return &EventReporterServerSet{
		RepoService:   repoService,
		MetricsServer: metricsServer,
	}
}
