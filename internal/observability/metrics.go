package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/fheinfling/aicoldb/internal/version"
)

// Metrics bundles every prometheus instrument the gateway exposes.
//
// The naming follows the prometheus convention: aicoldb_<subsystem>_<name>_<unit>.
type Metrics struct {
	Registry *prometheus.Registry

	RequestDuration   *prometheus.HistogramVec // labels: route, method, status
	RequestsTotal     *prometheus.CounterVec   // labels: route, method, status
	SQLStatements     *prometheus.CounterVec   // labels: command, sqlstate
	RPCInvocations    *prometheus.CounterVec   // labels: name, status
	IdempotencyHits   prometheus.Counter
	IdempotencyMisses prometheus.Counter
	AuthCacheSize     prometheus.GaugeFunc
	QueueDepth        prometheus.Gauge // surfaced from the SDK via /metrics relay (set externally)
	BuildInfo         *prometheus.GaugeVec
}

// NewMetrics constructs and registers every instrument exactly once. The
// returned registry is what /metrics serves.
func NewMetrics(authCacheLen func() int) *Metrics {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector())
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		Registry: r,
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "aicoldb_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by route/method/status.",
			Buckets: prometheus.ExponentialBucketsRange(0.001, 10, 12),
		}, []string{"route", "method", "status"}),
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aicoldb_requests_total",
			Help: "Total HTTP requests, by route/method/status.",
		}, []string{"route", "method", "status"}),
		SQLStatements: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aicoldb_sql_statements_total",
			Help: "SQL statements forwarded by the executor, by command tag and SQLSTATE.",
		}, []string{"command", "sqlstate"}),
		RPCInvocations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aicoldb_rpc_invocations_total",
			Help: "RPC invocations, by name and status (ok|conflict|error).",
		}, []string{"name", "status"}),
		IdempotencyHits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "aicoldb_idempotency_hits_total",
			Help: "Idempotency-Key replays served from the server-side cache.",
		}),
		IdempotencyMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "aicoldb_idempotency_misses_total",
			Help: "Idempotency-Key requests that ran for the first time.",
		}),
		QueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "aicoldb_queue_depth",
			Help: "Number of pending writes in the SDK retry queue (relayed via the API).",
		}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aicoldb_build_info",
			Help: "Build identifiers as labels; the value is always 1.",
		}, []string{"version", "commit", "build_date"}),
	}
	m.AuthCacheSize = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "aicoldb_auth_cache_size",
			Help: "Current entries in the in-memory API key verification cache.",
		},
		func() float64 {
			if authCacheLen == nil {
				return 0
			}
			return float64(authCacheLen())
		},
	)

	r.MustRegister(
		m.RequestDuration,
		m.RequestsTotal,
		m.SQLStatements,
		m.RPCInvocations,
		m.IdempotencyHits,
		m.IdempotencyMisses,
		m.AuthCacheSize,
		m.QueueDepth,
		m.BuildInfo,
	)

	v := version.Get()
	m.BuildInfo.WithLabelValues(v.Version, v.Commit, v.BuildDate).Set(1)
	return m
}

// Handler returns the prometheus HTTP handler bound to this registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		Registry:          m.Registry,
		EnableOpenMetrics: true,
	})
}
