// Package metrics is a leaf package (imports nothing internal) holding
// the event-driven counters/histograms every other package records
// against directly (spec section 35: "Expose Prometheus metrics") — the
// same role internal/outbox's event-type constants play for events. The
// gauge metrics that reflect current DB/reader state instead live as
// custom prometheus.Collectors in the domain packages that own that
// state (internal/node, internal/instance) — a leaf package can't
// depend on their types, so those don't belong here.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Event counters/histograms — recorded at the call site, right next to
// the log line the same event already produces.
var (
	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nebula_api_requests_total",
		Help: "Total HTTP requests handled by nebula-api.",
	}, []string{"method", "path", "status"})

	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nebula_api_request_duration_seconds",
		Help: "HTTP request duration in seconds.",
	}, []string{"method", "path"})

	SchedulerDecisionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nebula_scheduler_decisions_total",
		Help: "Total scheduling decisions, by strategy and outcome.",
	}, []string{"strategy", "outcome"})

	JobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nebula_jobs_total",
		Help: "Total job attempts completed, by type and outcome status.",
	}, []string{"type", "status"})

	JobsFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nebula_jobs_failed_total",
		Help: "Total jobs that exhausted all retry attempts (entered the DLQ).",
	}, []string{"type"})
)

// Node gauges — set directly by internal/node.Service.Heartbeat, at the
// moment a heartbeat reports them. Not DB-backed (compute_nodes doesn't
// persist raw CPU%/memory-used, only load_average/running_instances), so
// a live-collector-at-scrape-time approach doesn't apply here the way it
// does for nebula_instances_total below — pushing from the event that
// actually has the data is simpler and just as fresh.
var (
	NodeCPUUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nebula_node_cpu_usage",
		Help: "Most recently reported CPU usage percentage, by node.",
	}, []string{"node_id"})

	NodeMemoryUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nebula_node_memory_usage",
		Help: "Most recently reported memory used (MB), by node.",
	}, []string{"node_id"})
)
