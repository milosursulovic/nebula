package provisioning

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := newCircuitBreakers()
	failure := errors.New("agent unreachable")

	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		if !cb.allow("node-1") {
			t.Fatalf("allow() = false before threshold reached (attempt %d)", i+1)
		}
		cb.recordResult("node-1", failure)
	}

	if cb.allow("node-1") {
		t.Fatal("allow() = true after threshold consecutive failures, want false (open)")
	}
}

func TestCircuitBreakerIndependentPerNode(t *testing.T) {
	cb := newCircuitBreakers()
	failure := errors.New("agent unreachable")

	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		cb.recordResult("node-1", failure)
	}

	if cb.allow("node-1") {
		t.Error("node-1 should be open")
	}
	if !cb.allow("node-2") {
		t.Error("node-2 should be unaffected by node-1's failures")
	}
}

func TestCircuitBreakerHalfOpenAfterCooldownThenCloses(t *testing.T) {
	cb := newCircuitBreakers()
	failure := errors.New("agent unreachable")

	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		cb.recordResult("node-1", failure)
	}
	if cb.allow("node-1") {
		t.Fatal("expected open immediately after threshold")
	}

	// Simulate cooldown elapsed by backdating openedAt directly (avoids a
	// real 30s sleep in the test).
	b := cb.breaker("node-1")
	b.mu.Lock()
	b.openedAt = time.Now().Add(-circuitBreakerCooldown - time.Second)
	b.mu.Unlock()

	if !cb.allow("node-1") {
		t.Fatal("expected one half-open trial call to be allowed after cooldown")
	}

	// A second call while still half-open (trial in flight) should also
	// be let through by allow() itself — recordResult is what decides
	// the outcome, not a second allow() call — but for this test we
	// simulate the trial succeeding:
	cb.recordResult("node-1", nil)

	if !cb.allow("node-1") {
		t.Fatal("expected closed (allowed) after a successful half-open trial")
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	cb := newCircuitBreakers()
	failure := errors.New("agent unreachable")

	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		cb.recordResult("node-1", failure)
	}

	b := cb.breaker("node-1")
	b.mu.Lock()
	b.openedAt = time.Now().Add(-circuitBreakerCooldown - time.Second)
	b.mu.Unlock()

	if !cb.allow("node-1") {
		t.Fatal("expected half-open trial to be allowed")
	}
	cb.recordResult("node-1", failure) // trial fails

	if cb.allow("node-1") {
		t.Fatal("expected re-opened (not allowed) immediately after a failed half-open trial")
	}
}

func TestCircuitBreakerRecordResultIgnoresCircuitOpenError(t *testing.T) {
	cb := newCircuitBreakers()
	failure := errors.New("agent unreachable")

	for i := 0; i < circuitBreakerFailureThreshold; i++ {
		cb.recordResult("node-1", failure)
	}
	b := cb.breaker("node-1")
	b.mu.Lock()
	openedAt := b.openedAt
	b.mu.Unlock()

	// Recording ErrCircuitOpen itself (what a caller that never got past
	// allow() would pass) must not reset openedAt — otherwise a retry
	// storm against an open breaker would extend its cooldown forever.
	time.Sleep(10 * time.Millisecond)
	cb.recordResult("node-1", ErrCircuitOpen)

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.openedAt.Equal(openedAt) {
		t.Errorf("openedAt changed after recording ErrCircuitOpen: was %v, now %v", openedAt, b.openedAt)
	}
}
