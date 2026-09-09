package provisioning

import (
	"errors"
	"sync"
	"time"
)

// circuitBreakerFailureThreshold consecutive failures against one node
// before its breaker opens; circuitBreakerCooldown is how long it then
// fails fast before allowing one trial call again (half-open).
const (
	circuitBreakerFailureThreshold = 5
	circuitBreakerCooldown         = 30 * time.Second
)

// ErrCircuitOpen means a node has failed circuitBreakerFailureThreshold
// times in a row recently — this call fails immediately without even
// attempting the network call, rather than paying the full gRPC
// callTimeout again on a node that's very likely still down.
var ErrCircuitOpen = errors.New("circuit breaker open: node has failed repeatedly, cooling down")

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// nodeBreaker is one node's closed/open/half-open state.
type nodeBreaker struct {
	mu               sync.Mutex
	state            breakerState
	consecutiveFails int
	openedAt         time.Time
}

// circuitBreakers is a small, self-contained per-node circuit breaker —
// scoped here rather than a top-level package because AgentSteps is its
// only real consumer; a generic pluggable framework would be speculative
// for one call site (spec section 72: "circuit breakers").
type circuitBreakers struct {
	mu       sync.Mutex
	byNodeID map[string]*nodeBreaker
}

func newCircuitBreakers() *circuitBreakers {
	return &circuitBreakers{byNodeID: make(map[string]*nodeBreaker)}
}

func (c *circuitBreakers) breaker(nodeID string) *nodeBreaker {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.byNodeID[nodeID]
	if !ok {
		b = &nodeBreaker{}
		c.byNodeID[nodeID] = b
	}
	return b
}

// allow reports whether a call against nodeID should proceed right now.
// An open breaker past its cooldown transitions to half-open and allows
// exactly one trial call through.
func (c *circuitBreakers) allow(nodeID string) bool {
	b := c.breaker(nodeID)
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerOpen:
		if time.Since(b.openedAt) < circuitBreakerCooldown {
			return false
		}
		b.state = breakerHalfOpen
		return true
	default:
		return true
	}
}

// recordResult updates nodeID's breaker after a call attempt allow()
// approved. A caller that never got past allow() (err is ErrCircuitOpen)
// has nothing real to record — recording it anyway would keep resetting
// openedAt on every retry while already open, extending the cooldown
// forever instead of ever letting it elapse.
func (c *circuitBreakers) recordResult(nodeID string, err error) {
	if errors.Is(err, ErrCircuitOpen) {
		return
	}

	b := c.breaker(nodeID)
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.state = breakerClosed
		b.consecutiveFails = 0
		return
	}

	if b.state == breakerHalfOpen {
		// The one trial call failed — back to open for another cooldown.
		b.state = breakerOpen
		b.openedAt = time.Now()
		return
	}

	b.consecutiveFails++
	if b.consecutiveFails >= circuitBreakerFailureThreshold {
		b.state = breakerOpen
		b.openedAt = time.Now()
	}
}
