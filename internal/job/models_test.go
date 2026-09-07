package job

import "testing"

func TestComputeBackoffBaseMatchesSpecTable(t *testing.T) {
	// spec section 24: attempt1->1s, attempt2->2s, attempt3->4s,
	// attempt4->8s, attempt5->16s, before jitter. Jitter only adds time
	// (up to base/2), so the backoff must be within [base, base*1.5].
	tests := []struct {
		attempt  int
		baseSecs float64
	}{
		{1, 1}, {2, 2}, {3, 4}, {4, 8}, {5, 16},
	}

	for _, tt := range tests {
		got := computeBackoff(tt.attempt)
		min := tt.baseSecs
		max := tt.baseSecs * 1.5
		if got.Seconds() < min || got.Seconds() > max {
			t.Errorf("computeBackoff(%d) = %v, want between %vs and %vs", tt.attempt, got, min, max)
		}
	}
}

func TestComputeBackoffAddsJitterVariation(t *testing.T) {
	// Not a strict guarantee (jitter is random), but with enough samples
	// at least one should differ if jitter is actually applied.
	first := computeBackoff(3)
	differed := false
	for i := 0; i < 20; i++ {
		if computeBackoff(3) != first {
			differed = true
			break
		}
	}
	if !differed {
		t.Error("computeBackoff appears to never vary across calls; expected jitter")
	}
}
