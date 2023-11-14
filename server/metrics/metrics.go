package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/argoproj/argo-cd/v2/util/profile"
)

type MetricsServer struct {
	*http.Server
	redisRequestCounter          *prometheus.CounterVec
	redisRequestHistogram        *prometheus.HistogramVec
	eventReportingCounter        *prometheus.GaugeVec
	eventReportingHistogram      *prometheus.HistogramVec
	channelSizeCounter           *prometheus.GaugeVec
	locksCounter                 *prometheus.GaugeVec
	amountOfChangesCounter       *prometheus.CounterVec
	amountOfIgnoredEventsCounter *prometheus.CounterVec
}

var (
	redisRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "argocd_redis_request_total",
			Help: "Number of kubernetes requests executed during application reconciliation.",
		},
		[]string{"initiator", "failed"},
	)
	redisRequestHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "argocd_redis_request_duration",
			Help:    "Redis requests duration.",
			Buckets: []float64{0.1, 0.25, .5, 1, 2},
		},
		[]string{"initiator"},
	)
	eventReportingCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argocd_event_reporting_total",
			Help: "Number of events",
		},
		[]string{"initiator", "failed", "cache", "application"},
	)

	eventReportingHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "argocd_event_reporting",
			Help:    "Application reporting performance.",
			Buckets: []float64{1, 2, 4, 8, 16, 32, 64, 128, 256},
		},
		[]string{"initiator"},
	)

	channelSizeCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argocd_event_reporting_channel_total",
			Help: "Number of events",
		},
		[]string{"channel"},
	)

	locksCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argocd_locks_total",
			Help: "Number of locks.",
		},
		[]string{},
	)
	amountOfChangesCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "argocd_amount_of_events_total",
			Help: "Number of events.",
		},
		[]string{},
	)
	amountOfIgnoredEventsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "argocd_amount_of_ignored_events_total",
			Help: "Number of ignored events.",
		},
		[]string{"application"},
	)
)

// NewMetricsServer returns a new prometheus server which collects api server metrics
func NewMetricsServer(host string, port int) *MetricsServer {
	mux := http.NewServeMux()
	registry := prometheus.NewRegistry()
	mux.Handle("/metrics", promhttp.HandlerFor(prometheus.Gatherers{
		registry,
		prometheus.DefaultGatherer,
	}, promhttp.HandlerOpts{}))
	profile.RegisterProfiler(mux)

	registry.MustRegister(redisRequestCounter)
	registry.MustRegister(redisRequestHistogram)
	registry.MustRegister(eventReportingCounter)
	registry.MustRegister(eventReportingHistogram)
	registry.MustRegister(channelSizeCounter)
	registry.MustRegister(locksCounter)
	registry.MustRegister(amountOfChangesCounter)
	registry.MustRegister(amountOfIgnoredEventsCounter)

	return &MetricsServer{
		Server: &http.Server{
			Addr:    fmt.Sprintf("%s:%d", host, port),
			Handler: mux,
		},
		redisRequestCounter:          redisRequestCounter,
		redisRequestHistogram:        redisRequestHistogram,
		eventReportingCounter:        eventReportingCounter,
		eventReportingHistogram:      eventReportingHistogram,
		channelSizeCounter:           channelSizeCounter,
		locksCounter:                 locksCounter,
		amountOfChangesCounter:       amountOfChangesCounter,
		amountOfIgnoredEventsCounter: amountOfIgnoredEventsCounter,
	}
}

func (m *MetricsServer) IncRedisRequest(failed bool) {
	m.redisRequestCounter.WithLabelValues("argocd-server", strconv.FormatBool(failed)).Inc()
}

// ObserveRedisRequestDuration observes redis request duration
func (m *MetricsServer) ObserveRedisRequestDuration(duration time.Duration) {
	m.redisRequestHistogram.WithLabelValues("argocd-server").Observe(duration.Seconds())
}

func (m *MetricsServer) IncEventReportingCounter(failed bool, cache bool, application string) {
	m.eventReportingCounter.WithLabelValues("argocd-server", strconv.FormatBool(failed), strconv.FormatBool(cache), application).Inc()
}

func (m *MetricsServer) IncEventReportingHistogram(duration time.Duration) {
	seconds := duration.Seconds()
	m.eventReportingHistogram.WithLabelValues("argocd-server").Observe(seconds)
}

func (m *MetricsServer) SetChannelSizeCounter(channel string, size float64) {
	m.channelSizeCounter.WithLabelValues(channel).Set(size)
}

func (m *MetricsServer) IncLocksCounter() {
	m.locksCounter.WithLabelValues().Inc()
}

func (m *MetricsServer) DecLocksCounter() {
	m.locksCounter.WithLabelValues().Dec()
}

func (m *MetricsServer) IncAmountOfChangesCounter() {
	m.amountOfChangesCounter.WithLabelValues().Inc()
}

func (m *MetricsServer) IncAmountOfIgnoredEventsCounter(application string) {
	m.amountOfIgnoredEventsCounter.WithLabelValues(application).Inc()
}
