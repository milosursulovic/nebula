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

// NewScheduler builds a Scheduler for the named strategy.
func NewScheduler(strategy string, nodes NodeLister) (Scheduler, error) {
	switch strategy {
	case StrategyFirstFit:
		return NewFirstFit(nodes), nil
	case StrategyBestFit:
		return NewBestFit(nodes), nil
	case StrategyLeastLoaded:
		return NewLeastLoaded(nodes), nil
	case StrategyWeighted:
		return NewWeighted(nodes), nil
	default:
		return nil, fmt.Errorf("unknown scheduler strategy %q", strategy)
	}
}
