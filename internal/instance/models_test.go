package instance

import "testing"

func TestCanTransitionValidChain(t *testing.T) {
	chain := []Status{StatusPending, StatusProvisioning, StatusRunning, StatusStopping, StatusStopped}
	for i := 0; i < len(chain)-1; i++ {
		if !CanTransition(chain[i], chain[i+1]) {
			t.Errorf("CanTransition(%s, %s) = false, want true", chain[i], chain[i+1])
		}
	}
}

func TestCanTransitionRejectsInvalidExample(t *testing.T) {
	// Spec section 13's explicit invalid example.
	if CanTransition(StatusDeleted, StatusRunning) {
		t.Error("CanTransition(DELETED, RUNNING) = true, want false")
	}
}

func TestCanTransitionDeletedIsTerminal(t *testing.T) {
	for _, to := range []Status{StatusPending, StatusProvisioning, StatusRunning, StatusStopping, StatusStopped, StatusError, StatusDeleting, StatusDeleted} {
		if CanTransition(StatusDeleted, to) {
			t.Errorf("CanTransition(DELETED, %s) = true, want false", to)
		}
	}
}

func TestCanTransitionDeletingReachableFromLiveStates(t *testing.T) {
	for _, from := range []Status{StatusPending, StatusRunning, StatusStopped, StatusError} {
		if !CanTransition(from, StatusDeleting) {
			t.Errorf("CanTransition(%s, DELETING) = false, want true", from)
		}
	}
}

func TestCanTransitionRejectsArbitraryJumps(t *testing.T) {
	tests := []struct{ from, to Status }{
		{StatusPending, StatusRunning},
		{StatusPending, StatusStopped},
		{StatusRunning, StatusPending},
		{StatusStopped, StatusProvisioning},
		{StatusDeleting, StatusRunning},
	}
	for _, tt := range tests {
		if CanTransition(tt.from, tt.to) {
			t.Errorf("CanTransition(%s, %s) = true, want false", tt.from, tt.to)
		}
	}
}
