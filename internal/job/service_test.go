package job

import (
	"context"
	"errors"
	"testing"
)

func newTestService() Service {
	return NewService(newFakeRepository())
}

func strPtr(s string) *string { return &s }

func TestEnqueueDefaults(t *testing.T) {
	svc := newTestService()

	j, err := svc.Enqueue(context.Background(), TypeCreateInstance, strPtr("tenant-1"), strPtr("instance-1"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if j.Status != StatusQueued {
		t.Errorf("Status = %q, want %q", j.Status, StatusQueued)
	}
	if j.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("MaxAttempts = %d, want %d", j.MaxAttempts, DefaultMaxAttempts)
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestRetryOnlyFromFailed(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	j, err := svc.Enqueue(ctx, TypeCreateInstance, nil, strPtr("instance-1"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Still QUEUED, not FAILED.
	_, err = svc.Retry(ctx, j.ID)
	if !errors.Is(err, ErrNotFailed) {
		t.Fatalf("Retry on QUEUED job = %v, want ErrNotFailed", err)
	}
}

func TestRetryResetsFailedJob(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)
	ctx := context.Background()

	j, err := svc.Enqueue(ctx, TypeCreateInstance, nil, strPtr("instance-1"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := repo.MarkFailed(ctx, j.ID, DefaultMaxAttempts, "simulated exhaustion"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	retried, err := svc.Retry(ctx, j.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if retried.Status != StatusQueued {
		t.Errorf("Status = %q, want %q", retried.Status, StatusQueued)
	}
	if retried.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", retried.Attempts)
	}
	if retried.Error != nil {
		t.Errorf("Error = %v, want nil", *retried.Error)
	}
}

func TestListFiltersByStatus(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)
	ctx := context.Background()

	a, err := svc.Enqueue(ctx, TypeCreateInstance, nil, strPtr("instance-a"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := svc.Enqueue(ctx, TypeCreateInstance, nil, strPtr("instance-b")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := repo.MarkFailed(ctx, a.ID, DefaultMaxAttempts, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	failed := StatusFailed
	list, err := svc.List(ctx, &failed)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != a.ID {
		t.Errorf("List(FAILED) = %+v, want just job %s", list, a.ID)
	}
}
