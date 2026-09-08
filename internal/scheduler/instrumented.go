package scheduler

import (
	"context"
	"errors"

	"github.com/milosursulovic/nebula/internal/metrics"
	"github.com/milosursulovic/nebula/internal/node"
)

// instrumented wraps a Scheduler to record nebula_scheduler_decisions_total
// (spec section 35) in one place, rather than duplicating the metric call
// across all four strategy implementations.
type instrumented struct {
	inner Scheduler
	name  string
}

func newInstrumented(name string, inner Scheduler) Scheduler {
	return &instrumented{inner: inner, name: name}
}

func (s *instrumented) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	n, err := s.inner.Schedule(ctx, req)

	outcome := "success"
	if err != nil {
		outcome = "no_capacity"
		if !errors.Is(err, ErrNoCapacity) {
			outcome = "error"
		}
	}
	metrics.SchedulerDecisionsTotal.WithLabelValues(s.name, outcome).Inc()

	return n, err
}
