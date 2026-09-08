package job

import (
	"math/rand"
	"time"
)

// Type is an infrastructure operation represented as a job (spec section 20).
type Type string

const (
	TypeCreateInstance Type = "CREATE_INSTANCE"
	TypeDeleteInstance Type = "DELETE_INSTANCE"
	TypeStartInstance  Type = "START_INSTANCE"
	TypeStopInstance   Type = "STOP_INSTANCE"
	TypeCreateNetwork  Type = "CREATE_NETWORK"
	TypeDeleteNetwork  Type = "DELETE_NETWORK"
	TypeCreateDisk     Type = "CREATE_DISK"
	TypeDeleteDisk     Type = "DELETE_DISK"
)

// Status is a job's execution state (spec section 20).
type Status string

const (
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusSuccess   Status = "SUCCESS"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

// DefaultMaxAttempts matches spec section 24's 5-entry backoff table
// (1s/2s/4s/8s/16s).
const DefaultMaxAttempts = 5

// Job is a queued infrastructure operation (spec section 20).
type Job struct {
	ID            string
	Type          Type
	Status        Status
	TenantID      *string
	InstanceID    *string
	NodeID        *string
	Attempts      int
	MaxAttempts   int
	NextAttemptAt time.Time
	Error         *string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time

	// TraceContext is the W3C traceparent captured when this job was
	// enqueued (spec section 36) — the worker extracts it back into the
	// handler's context so the saga's spans land as children of the
	// original HTTP request's trace, not a disconnected new one.
	TraceContext *string
}

// computeBackoff returns the delay before retrying the given (1-based)
// attempt number, per spec section 24: 2^(attempt-1) seconds, plus jitter
// ("add jitter to avoid synchronized retries") of up to half the base
// delay.
func computeBackoff(attempt int) time.Duration {
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	return base + jitter
}
