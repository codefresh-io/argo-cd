package application_change_revision_controller

import (
	"context"
	"crypto/tls"
	"fmt"
	appclient "github.com/argoproj/argo-cd/v2/application_change_revision_controller/application"
	application_change_revision_controller "github.com/argoproj/argo-cd/v2/application_change_revision_controller/controller"
	"github.com/argoproj/argo-cd/v2/event_reporter/reporter"
	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/argoproj/argo-cd/v2/event_reporter/handlers"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	appinformer "github.com/argoproj/argo-cd/v2/pkg/client/informers/externalversions"
	applisters "github.com/argoproj/argo-cd/v2/pkg/client/listers/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v2/reposerver/apiclient"
	servercache "github.com/argoproj/argo-cd/v2/server/cache"
	"github.com/argoproj/argo-cd/v2/util/db"
	errorsutil "github.com/argoproj/argo-cd/v2/util/errors"
	"github.com/argoproj/argo-cd/v2/util/healthz"
	"github.com/argoproj/argo-cd/v2/util/io"
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

type ApplicationChangeRevisionServer struct {
	ApplicationChangeRevisionServerOpts

	settings             *settings_util.ArgoCDSettings
	log                  *log.Entry
	settingsMgr          *settings_util.SettingsManager
	appInformer          cache.SharedIndexInformer
	appLister            applisters.ApplicationLister
	applicationClientset appclientset.Interface
	db                   db.ArgoDB

	// stopCh is the channel which when closed, will shutdown the Event Reporter server
	stopCh         chan struct{}
	serviceSet     *ApplicationChangeRevisionServerSet
	featureManager *reporter.FeatureManager
}

type ApplicationChangeRevisionServerSet struct {
}

type ApplicationChangeRevisionServerOpts struct {
	ListenPort               int
	ListenHost               string
	MetricsPort              int
	MetricsHost              string
	Namespace                string
	KubeClientset            kubernetes.Interface
	AppClientset             appclientset.Interface
	RepoClientset            repoapiclient.Clientset
	ApplicationServiceClient appclient.ApplicationClient
	Cache                    *servercache.Cache
	RedisClient              *redis.Client
	ApplicationNamespaces    []string
	BaseHRef                 string
	RootPath                 string
	CodefreshConfig          *codefresh.CodefreshConfig
	RateLimiterOpts          *reporter.RateLimiterOpts
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

func (a *ApplicationChangeRevisionServer) healthCheck(r *http.Request) error {
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
func (a *ApplicationChangeRevisionServer) Init(ctx context.Context) {
	go a.appInformer.Run(ctx.Done())
	go a.featureManager.Watch()
	svcSet := newApplicationChangeRevisionServiceSet()
	a.serviceSet = svcSet
}

func (a *ApplicationChangeRevisionServer) RunController(ctx context.Context) {
	controller := application_change_revision_controller.NewApplicationChangeRevisionController(a.appInformer, a.Cache, a.settingsMgr, a.ApplicationServiceClient, a.appLister, a.applicationClientset)
	go controller.Run(ctx)
}

// newHTTPServer returns the HTTP server to serve HTTP/HTTPS requests. This is implemented
// using grpc-gateway as a proxy to the gRPC server.
func (a *ApplicationChangeRevisionServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	endpoint := fmt.Sprintf("localhost:%d", port)
	mux := http.NewServeMux()
	httpS := http.Server{
		Addr: endpoint,
		Handler: &handlerSwitcher{
			handler: mux,
		},
	}

	healthz.ServeHealthCheck(mux, a.healthCheck)

	rH := handlers.GetRequestHandlers(a.ApplicationServiceClient)
	mux.HandleFunc("/app-distribution", rH.GetAppDistribution)

	return &httpS
}

func (a *ApplicationChangeRevisionServer) checkServeErr(name string, err error) {
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

func (a *ApplicationChangeRevisionServer) Listen() (*Listeners, error) {
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
func (a *ApplicationChangeRevisionServer) Run(ctx context.Context, lns *Listeners) {
	var httpS = a.newHTTPServer(ctx, a.ListenPort)
	tlsConfig := tls.Config{}
	tlsConfig.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return a.settings.Certificate, nil
	}
	go func() { a.checkServeErr("httpS", httpS.Serve(lns.Main)) }()
	go a.RunController(ctx)

	if !cache.WaitForCacheSync(ctx.Done(), a.appInformer.HasSynced) {
		log.Fatal("Timed out waiting for project cache to sync")
	}

	a.stopCh = make(chan struct{})
	<-a.stopCh
}

// NewServer returns a new instance of the Event Reporter server
func NewApplicationChangeRevisionServer(ctx context.Context, opts ApplicationChangeRevisionServerOpts) *ApplicationChangeRevisionServer {
	settingsMgr := settings_util.NewSettingsManager(ctx, opts.KubeClientset, opts.Namespace)
	settings, err := settingsMgr.InitializeSettings(true)
	errorsutil.CheckError(err)

	appInformerNs := opts.Namespace
	if len(opts.ApplicationNamespaces) > 0 {
		appInformerNs = ""
	}
	appFactory := appinformer.NewSharedInformerFactoryWithOptions(opts.AppClientset, 0, appinformer.WithNamespace(appInformerNs), appinformer.WithTweakListOptions(func(options *metav1.ListOptions) {}))

	appInformer := appFactory.Argoproj().V1alpha1().Applications().Informer()
	appLister := appFactory.Argoproj().V1alpha1().Applications().Lister()

	dbInstance := db.NewDB(opts.Namespace, settingsMgr, opts.KubeClientset)

	server := &ApplicationChangeRevisionServer{
		ApplicationChangeRevisionServerOpts: opts,
		log:                                 log.NewEntry(log.StandardLogger()),
		settings:                            settings,
		settingsMgr:                         settingsMgr,
		appInformer:                         appInformer,
		appLister:                           appLister,
		db:                                  dbInstance,
		featureManager:                      reporter.NewFeatureManager(settingsMgr),
		applicationClientset:                opts.AppClientset,
	}

	if err != nil {
		// Just log. It's not critical.
		log.Warnf("Failed to log in-cluster warnings: %v", err)
	}

	return server
}

func newApplicationChangeRevisionServiceSet() *ApplicationChangeRevisionServerSet {
	return &ApplicationChangeRevisionServerSet{}
}
