package job

import "errors"

var (
	// ErrNotFound is returned when a job ID does not match any job.
	ErrNotFound = errors.New("job not found")

	// ErrNotFailed is returned when retrying a job that isn't currently
	// FAILED (spec section 25: retry applies to jobs that exhausted their
	// attempts and landed in the dead letter queue).
	ErrNotFailed = errors.New("job is not in FAILED status")

	// errNoRows is an internal repository-layer sentinel.
	errNoRows = errors.New("no rows")
)
