package commands

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/argoproj/argo-cd/v2/event_reporter"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient"

	"github.com/argoproj/pkg/stats"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	cmdutil "github.com/argoproj/argo-cd/v2/cmd/util"
	"github.com/argoproj/argo-cd/v2/common"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	repoapiclient "github.com/argoproj/argo-cd/v2/reposerver/apiclient"
	servercache "github.com/argoproj/argo-cd/v2/server/cache"
	"github.com/argoproj/argo-cd/v2/util/cli"
	"github.com/argoproj/argo-cd/v2/util/env"
	"github.com/argoproj/argo-cd/v2/util/errors"
	"github.com/argoproj/argo-cd/v2/util/kube"
	"github.com/argoproj/argo-cd/v2/util/tls"
)

const (
	failureRetryCountEnv              = "ARGOCD_K8S_RETRY_COUNT"
	failureRetryPeriodMilliSecondsEnv = "ARGOCD_K8S_RETRY_DURATION_MILLISECONDS"
)

var (
	failureRetryCount              = 0
	failureRetryPeriodMilliSeconds = 100
)

func init() {
	failureRetryCount = env.ParseNumFromEnv(failureRetryCountEnv, failureRetryCount, 0, 10)
	failureRetryPeriodMilliSeconds = env.ParseNumFromEnv(failureRetryPeriodMilliSecondsEnv, failureRetryPeriodMilliSeconds, 0, 1000)
}

// NewCommand returns a new instance of an argocd command
func NewCommand() *cobra.Command {
	var (
		redisClient              *redis.Client
		insecure                 bool
		listenHost               string
		listenPort               int
		metricsHost              string
		metricsPort              int
		glogLevel                int
		clientConfig             clientcmd.ClientConfig
		repoServerTimeoutSeconds int
		repoServerAddress        string
		applicationServerAddress string
		cacheSrc                 func() (*servercache.Cache, error)
		contentSecurityPolicy    string
		repoServerPlaintext      bool
		repoServerStrictTLS      bool
		applicationNamespaces    []string
		argocdToken              string
	)
	var command = &cobra.Command{
		Use:   cliName,
		Short: "Run the Event Reporter server",
		// TODO: [Event Reporter] change description
		Long:              "The API server is a gRPC/REST server which exposes the API consumed by the Web UI, CLI, and CI/CD systems.  This command runs API server in the foreground.  It can be configured by following options.",
		DisableAutoGenTag: true,
		Run: func(c *cobra.Command, args []string) {
			ctx := c.Context()

			vers := common.GetVersion()
			namespace, _, err := clientConfig.Namespace()
			errors.CheckError(err)
			vers.LogStartupInfo(
				"Event Reporter Server",
				map[string]any{
					"namespace": namespace,
					"port":      listenPort,
				},
			)

			cli.SetLogFormat(cmdutil.LogFormat)
			cli.SetLogLevel(cmdutil.LogLevel)
			cli.SetGLogLevel(glogLevel)

			config, err := clientConfig.ClientConfig()
			errors.CheckError(err)
			errors.CheckError(v1alpha1.SetK8SConfigDefaults(config))

			cache, err := cacheSrc()
			errors.CheckError(err)

			kubeclientset := kubernetes.NewForConfigOrDie(config)

			appclientsetConfig, err := clientConfig.ClientConfig()
			errors.CheckError(err)
			errors.CheckError(v1alpha1.SetK8SConfigDefaults(appclientsetConfig))
			config.UserAgent = fmt.Sprintf("argocd-server/%s (%s)", vers.Version, vers.Platform)

			if failureRetryCount > 0 {
				appclientsetConfig = kube.AddFailureRetryWrapper(appclientsetConfig, failureRetryCount, failureRetryPeriodMilliSeconds)
			}
			appClientSet := appclientset.NewForConfigOrDie(appclientsetConfig)
			tlsConfig := repoapiclient.TLSConfiguration{
				DisableTLS:       repoServerPlaintext,
				StrictValidation: repoServerStrictTLS,
			}

			// Load CA information to use for validating connections to the
			// repository server, if strict TLS validation was requested.
			if !repoServerPlaintext && repoServerStrictTLS {
				pool, err := tls.LoadX509CertPool(
					fmt.Sprintf("%s/server/tls/tls.crt", env.StringFromEnv(common.EnvAppConfigPath, common.DefaultAppConfigPath)),
					fmt.Sprintf("%s/server/tls/ca.crt", env.StringFromEnv(common.EnvAppConfigPath, common.DefaultAppConfigPath)),
				)
				if err != nil {
					log.Fatalf("%v", err)
				}
				tlsConfig.Certificates = pool
			}

			repoclientset := repoapiclient.NewRepoServerClientset(repoServerAddress, repoServerTimeoutSeconds, tlsConfig)

			applicationClientSet, err := apiclient.NewClient(&apiclient.ClientOptions{
				ServerAddr: applicationServerAddress,
				Insecure:   true,
				GRPCWeb:    true,
				PlainText:  true,
				AuthToken:  argocdToken,
			})

			errors.CheckError(err)

			closer, applicationClient, err := applicationClientSet.NewApplicationClient()

			errors.CheckError(err)

			defer func() {
				_ = closer.Close()
			}()

			eventReporterServerOpts := event_reporter.EventReporterServerOpts{
				ListenPort:               listenPort,
				ListenHost:               listenHost,
				Namespace:                namespace,
				KubeClientset:            kubeclientset,
				AppClientset:             appClientSet,
				RepoClientset:            repoclientset,
				Cache:                    cache,
				RedisClient:              redisClient,
				ApplicationNamespaces:    applicationNamespaces,
				ApplicationServiceClient: applicationClient,
			}

			stats.RegisterStackDumper()
			stats.StartStatsTicker(10 * time.Minute)
			stats.RegisterHeapDumper("memprofile")
			eventReporter := event_reporter.NewEventReporterServer(ctx, eventReporterServerOpts)
			eventReporter.Init(ctx)
			errors.CheckError(err)
			for {
				var closer func()
				ctx, cancel := context.WithCancel(ctx)
				eventReporter.Run(ctx)
				cancel()
				if closer != nil {
					closer()
				}
			}
		},
	}

	clientConfig = cli.AddKubectlFlagsToCmd(command)
	command.Flags().BoolVar(&insecure, "insecure", env.ParseBoolFromEnv("EVENT_REPORTER_INSECURE", false), "Run server without TLS")
	command.Flags().StringVar(&cmdutil.LogFormat, "logformat", env.StringFromEnv("EVENT_REPORTER_LOGFORMAT", "text"), "Set the logging format. One of: text|json")
	command.Flags().StringVar(&cmdutil.LogLevel, "loglevel", env.StringFromEnv("EVENT_REPORTER_LOG_LEVEL", "info"), "Set the logging level. One of: debug|info|warn|error")
	command.Flags().IntVar(&glogLevel, "gloglevel", 0, "Set the glog logging level")
	command.Flags().StringVar(&applicationServerAddress, "application-server", env.StringFromEnv("EVENT_REPORTER_APPLICATION_SERVER", common.DefaultApplicationServerAddr), "Application server address")
	command.Flags().StringVar(&argocdToken, "argocd-token", env.StringFromEnv("ARGOCD_TOKEN", ""), "ArgoCD server JWT token")
	command.Flags().StringVar(&repoServerAddress, "repo-server", env.StringFromEnv("EVENT_REPORTER_REPO_SERVER", common.DefaultRepoServerAddr), "Repo server address")
	command.AddCommand(cli.NewVersionCmd(cliName))
	command.Flags().StringVar(&listenHost, "address", env.StringFromEnv("EVENT_REPORTER_LISTEN_ADDRESS", common.DefaultAddressEventReporterServer), "Listen on given address")
	command.Flags().IntVar(&listenPort, "port", common.DefaultPortEventReporterServer, "Listen on given port")
	command.Flags().StringVar(&metricsHost, env.StringFromEnv("EVENT_REPORTER_METRICS_LISTEN_ADDRESS", "metrics-address"), common.DefaultAddressEventReporterServerMetrics, "Listen for metrics on given address")
	command.Flags().IntVar(&metricsPort, "metrics-port", common.DefaultPortEventReporterServerMetrics, "Start metrics on given port")
	command.Flags().IntVar(&repoServerTimeoutSeconds, "repo-server-timeout-seconds", env.ParseNumFromEnv("EVENT_REPORTER_REPO_SERVER_TIMEOUT_SECONDS", 60, 0, math.MaxInt64), "Repo server RPC call timeout seconds.")
	command.Flags().StringVar(&contentSecurityPolicy, "content-security-policy", env.StringFromEnv("EVENT_REPORTER_CONTENT_SECURITY_POLICY", "frame-ancestors 'self';"), "Set Content-Security-Policy header in HTTP responses to `value`. To disable, set to \"\".")
	command.Flags().BoolVar(&repoServerPlaintext, "repo-server-plaintext", env.ParseBoolFromEnv("EVENT_REPORTER_REPO_SERVER_PLAINTEXT", false), "Use a plaintext client (non-TLS) to connect to repository server")
	command.Flags().BoolVar(&repoServerStrictTLS, "repo-server-strict-tls", env.ParseBoolFromEnv("EVENT_REPORTER_REPO_SERVER_STRICT_TLS", false), "Perform strict validation of TLS certificates when connecting to repo server")
	cacheSrc = servercache.AddCacheFlagsToCmd(command, func(client *redis.Client) {
		redisClient = client
	})
	return command
}
