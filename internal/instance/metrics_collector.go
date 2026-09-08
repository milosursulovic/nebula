package instance

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

var instancesTotalDesc = prometheus.NewDesc(
	"nebula_instances_total",
	"Current instance count, by status, across all tenants.",
	[]string{"status"}, nil,
)

// metricsCollector is a live prometheus.Collector — it queries current DB
// state at scrape time rather than being incrementally updated from every
// status-changing code path (saga transitions, job retries, DELETE), which
// would be easy to get out of sync. Self-correcting, no drift risk.
type metricsCollector struct {
	repo   Repository
	logger *slog.Logger
}

// NewMetricsCollector returns a prometheus.Collector for
// nebula_instances_total (spec section 35). Register it once with the
// process's Prometheus registry.
func NewMetricsCollector(repo Repository, logger *slog.Logger) prometheus.Collector {
	return &metricsCollector{repo: repo, logger: logger}
}

func (c *metricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- instancesTotalDesc
}

func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	counts, err := c.repo.CountByStatus(context.Background())
	if err != nil {
		c.logger.Error("instance metrics collector: count by status failed", "error", err)
		return
	}
	for status, count := range counts {
		ch <- prometheus.MustNewConstMetric(instancesTotalDesc, prometheus.GaugeValue, float64(count), status)
	}
}
