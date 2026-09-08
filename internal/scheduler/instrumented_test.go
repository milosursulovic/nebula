package scheduler

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/milosursulovic/nebula/internal/metrics"
	"github.com/milosursulovic/nebula/internal/node"
)

type fakeScheduler struct {
	node node.Node
	err  error
}

func (f fakeScheduler) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	return f.node, f.err
}

func TestInstrumentedRecordsSuccess(t *testing.T) {
	s := newInstrumented("test_strategy_success", fakeScheduler{node: node.Node{ID: "node-1"}})

	if _, err := s.Schedule(context.Background(), ResourceRequest{}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	got := testutil.ToFloat64(metrics.SchedulerDecisionsTotal.WithLabelValues("test_strategy_success", "success"))
	if got != 1 {
		t.Errorf("success counter = %v, want 1", got)
	}
}

func TestInstrumentedRecordsNoCapacity(t *testing.T) {
	s := newInstrumented("test_strategy_no_capacity", fakeScheduler{err: ErrNoCapacity})

	if _, err := s.Schedule(context.Background(), ResourceRequest{}); err == nil {
		t.Fatal("expected an error")
	}

	got := testutil.ToFloat64(metrics.SchedulerDecisionsTotal.WithLabelValues("test_strategy_no_capacity", "no_capacity"))
	if got != 1 {
		t.Errorf("no_capacity counter = %v, want 1", got)
	}
}
