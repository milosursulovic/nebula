package job

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/milosursulovic/nebula/internal/metrics"
)

// Handler executes one job's actual work. Pool knows nothing about what a
// given job type does — handlers are registered by the caller (see
// cmd/nebula-api/main.go), keeping this package decoupled from domain
// packages like internal/instance.
type Handler func(ctx context.Context, j Job) error

// PollInterval is how often the dispatcher checks for due jobs when the
// queue is empty (spec section 20/21: jobs should be picked up promptly).
const PollInterval = time.Second

// Pool is the worker pool described in spec section 21: a dispatcher feeds
// an unbuffered channel that workerCount goroutines drain.
type Pool struct {
	repo        Repository
	logger      *slog.Logger
	workerCount int
	handlers    map[Type]Handler
}

func NewPool(repo Repository, logger *slog.Logger, workerCount int) *Pool {
	return &Pool{
		repo:        repo,
		logger:      logger,
		workerCount: workerCount,
		handlers:    make(map[Type]Handler),
	}
}

// RegisterHandler wires a Handler for a job Type. Call before Run.
func (p *Pool) RegisterHandler(t Type, h Handler) {
	p.handlers[t] = h
}

// Run blocks, dispatching and executing jobs until ctx is cancelled.
func (p *Pool) Run(ctx context.Context) {
	if n, err := p.repo.RequeueOrphanedRunning(ctx); err != nil {
		p.logger.Error("job pool: requeue orphaned running jobs failed", "error", err)
	} else if n > 0 {
		p.logger.Info("job pool: requeued orphaned running jobs", "count", n)
	}

	jobs := make(chan Job)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.dispatch(ctx, jobs)
	}()

	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				p.execute(ctx, j)
			}
		}()
	}

	wg.Wait()
}

func (p *Pool) dispatch(ctx context.Context, jobs chan<- Job) {
	defer close(jobs)

	for {
		j, ok, err := p.repo.ClaimNextDue(ctx)
		if err != nil {
			p.logger.Error("job pool: claim next due job failed", "error", err)
			ok = false
		}

		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-time.After(PollInterval):
				continue
			}
		}

		select {
		case jobs <- j:
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pool) execute(ctx context.Context, j Job) {
	// Continue the trace that enqueued this job (spec section 36) — the
	// original HTTP request's span, captured at enqueue time since the
	// worker's call is a genuinely separate, later, async invocation.
	ctx = withTraceContext(ctx, j.TraceContext)

	handler, ok := p.handlers[j.Type]
	if !ok {
		if _, err := p.repo.MarkFailed(ctx, j.ID, j.Attempts, fmt.Sprintf("no handler registered for job type %s", j.Type)); err != nil {
			p.logger.Error("job pool: mark failed (no handler) failed", "job_id", j.ID, "error", err)
		}
		metrics.JobsTotal.WithLabelValues(string(j.Type), "failed").Inc()
		metrics.JobsFailedTotal.WithLabelValues(string(j.Type)).Inc()
		return
	}

	if err := handler(ctx, j); err != nil {
		p.handleFailure(ctx, j, err)
		return
	}

	if _, err := p.repo.MarkSuccess(ctx, j.ID); err != nil {
		p.logger.Error("job pool: mark success failed", "job_id", j.ID, "error", err)
		return
	}
	metrics.JobsTotal.WithLabelValues(string(j.Type), "success").Inc()
	p.logger.Info("job succeeded", "job_id", j.ID, "type", j.Type)
}

func withTraceContext(ctx context.Context, traceparent *string) context.Context {
	if traceparent == nil || *traceparent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": *traceparent}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func (p *Pool) handleFailure(ctx context.Context, j Job, execErr error) {
	newAttempts := j.Attempts + 1

	if newAttempts >= j.MaxAttempts {
		if _, err := p.repo.MarkFailed(ctx, j.ID, newAttempts, execErr.Error()); err != nil {
			p.logger.Error("job pool: mark failed failed", "job_id", j.ID, "error", err)
			return
		}
		metrics.JobsTotal.WithLabelValues(string(j.Type), "failed").Inc()
		metrics.JobsFailedTotal.WithLabelValues(string(j.Type)).Inc()
		p.logger.Error("job failed permanently (entering DLQ)", "job_id", j.ID, "type", j.Type, "attempts", newAttempts, "error", execErr)
		return
	}

	backoff := computeBackoff(newAttempts)
	if _, err := p.repo.MarkQueuedForRetry(ctx, j.ID, newAttempts, execErr.Error(), time.Now().Add(backoff)); err != nil {
		p.logger.Error("job pool: mark queued for retry failed", "job_id", j.ID, "error", err)
		return
	}
	metrics.JobsTotal.WithLabelValues(string(j.Type), "retry").Inc()
	p.logger.Warn("job failed, will retry", "job_id", j.ID, "type", j.Type, "attempt", newAttempts, "backoff", backoff, "error", execErr)
}
