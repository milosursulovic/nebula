package scheduler

import "fmt"

// Strategy names accepted by NewScheduler — spec section 18: "make the
// strategy configurable."
const (
	StrategyFirstFit    = "first_fit"
	StrategyBestFit     = "best_fit"
	StrategyLeastLoaded = "least_loaded"
	StrategyWeighted    = "weighted"
)

// NewScheduler builds a Scheduler for the named strategy, wrapped to
// record nebula_scheduler_decisions_total (spec section 35).
func NewScheduler(strategy string, nodes NodeLister) (Scheduler, error) {
	var s Scheduler
	switch strategy {
	case StrategyFirstFit:
		s = NewFirstFit(nodes)
	case StrategyBestFit:
		s = NewBestFit(nodes)
	case StrategyLeastLoaded:
		s = NewLeastLoaded(nodes)
	case StrategyWeighted:
		s = NewWeighted(nodes)
	default:
		return nil, fmt.Errorf("unknown scheduler strategy %q", strategy)
	}
	return newInstrumented(strategy, s), nil
}
