package scheduler

import "testing"

func TestNewSchedulerKnownStrategies(t *testing.T) {
	lister := fakeNodeLister{}

	for _, strategy := range []string{StrategyFirstFit, StrategyBestFit, StrategyLeastLoaded, StrategyWeighted} {
		s, err := NewScheduler(strategy, lister)
		if err != nil {
			t.Errorf("NewScheduler(%q): %v", strategy, err)
		}
		if s == nil {
			t.Errorf("NewScheduler(%q) returned nil Scheduler", strategy)
		}
	}
}

func TestNewSchedulerUnknownStrategy(t *testing.T) {
	if _, err := NewScheduler("does-not-exist", fakeNodeLister{}); err == nil {
		t.Fatal("expected an error for an unknown strategy")
	}
}
