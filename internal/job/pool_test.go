package job

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitForStatus polls repo for id to reach one of the wanted statuses,
// failing the test if it doesn't happen within timeout.
func waitForStatus(t *testing.T, repo Repository, id string, timeout time.Duration, want ...Status) Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		for _, w := range want {
			if j.Status == w {
				return j
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %v within %v", id, want, timeout)
	return Job{}
}

func runPool(ctx context.Context, p *Pool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	return done
}

func TestPoolSuccessPath(t *testing.T) {
	repo := newFakeRepository()
	pool := NewPool(repo, testLogger(), 1)
	pool.RegisterHandler(TypeCreateInstance, func(ctx context.Context, j Job) error {
		return nil
	})

	j, err := repo.Create(context.Background(), Job{Type: TypeCreateInstance, Status: StatusQueued, MaxAttempts: DefaultMaxAttempts})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := runPool(ctx, pool)

	final := waitForStatus(t, repo, j.ID, time.Second, StatusSuccess)
	if final.Error != nil {
		t.Errorf("Error = %v, want nil", *final.Error)
	}

	cancel()
	<-done // Run must actually return once ctx is cancelled
}

func TestPoolRetriesThenSucceeds(t *testing.T) {
	repo := newFakeRepository()
	pool := NewPool(repo, testLogger(), 1)

	var calls int64
	pool.RegisterHandler(TypeCreateInstance, func(ctx context.Context, j Job) error {
		if atomic.AddInt64(&calls, 1) == 1 {
			return errors.New("simulated transient failure")
		}
		return nil
	})

	j, err := repo.Create(context.Background(), Job{Type: TypeCreateInstance, Status: StatusQueued, MaxAttempts: DefaultMaxAttempts})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := runPool(ctx, pool)

	final := waitForStatus(t, repo, j.ID, 3*time.Second, StatusSuccess)
	if final.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (one recorded failure before success)", final.Attempts)
	}
	if atomic.LoadInt64(&calls) != 2 {
		t.Errorf("handler called %d times, want 2", calls)
	}

	cancel()
	<-done
}

func TestPoolExhaustsRetriesIntoDLQ(t *testing.T) {
	repo := newFakeRepository()
	pool := NewPool(repo, testLogger(), 1)
	pool.RegisterHandler(TypeCreateInstance, func(ctx context.Context, j Job) error {
		return errors.New("always fails")
	})

	// A small MaxAttempts keeps this test fast: one retry cycle (~1-1.5s
	// backoff) instead of spec's full 5-attempt table.
	j, err := repo.Create(context.Background(), Job{Type: TypeCreateInstance, Status: StatusQueued, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := runPool(ctx, pool)

	final := waitForStatus(t, repo, j.ID, 3*time.Second, StatusFailed)
	if final.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", final.Attempts)
	}
	if final.Error == nil {
		t.Error("Error = nil, want a recorded failure reason")
	}

	cancel()
	<-done
}

func TestPoolNoHandlerRegisteredFailsImmediately(t *testing.T) {
	repo := newFakeRepository()
	pool := NewPool(repo, testLogger(), 1) // no handlers registered at all

	j, err := repo.Create(context.Background(), Job{Type: TypeDeleteInstance, Status: StatusQueued, MaxAttempts: DefaultMaxAttempts})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := runPool(ctx, pool)

	final := waitForStatus(t, repo, j.ID, time.Second, StatusFailed)
	if final.Error == nil {
		t.Error("Error = nil, want a 'no handler registered' message")
	}

	cancel()
	<-done
}

func TestPoolRequeuesOrphanedRunningOnStartup(t *testing.T) {
	repo := newFakeRepository()
	ctx := context.Background()

	j, err := repo.Create(ctx, Job{Type: TypeCreateInstance, Status: StatusQueued, MaxAttempts: DefaultMaxAttempts})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate a crash mid-execution: claim it (-> RUNNING) but never finish it.
	if _, ok, err := repo.ClaimNextDue(ctx); err != nil || !ok {
		t.Fatalf("ClaimNextDue: ok=%v err=%v", ok, err)
	}
	if orphaned, err := repo.Get(ctx, j.ID); err != nil || orphaned.Status != StatusRunning {
		t.Fatalf("expected job to be RUNNING before pool startup, got %+v (err=%v)", orphaned, err)
	}

	pool := NewPool(repo, testLogger(), 1)
	pool.RegisterHandler(TypeCreateInstance, func(ctx context.Context, j Job) error {
		return nil
	})

	runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := runPool(runCtx, pool)

	waitForStatus(t, repo, j.ID, time.Second, StatusSuccess)

	cancel()
	<-done
}
