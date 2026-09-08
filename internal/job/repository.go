package job

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary for jobs. Retry policy (backoff,
// max-attempts decisions) lives in the Pool, not here — this is pure
// persistence, mirroring node.Repository/node.Service's split.
type Repository interface {
	Create(ctx context.Context, j Job) (Job, error)
	List(ctx context.Context, status *Status) ([]Job, error)
	Get(ctx context.Context, id string) (Job, error)

	// ClaimNextDue atomically claims one QUEUED job whose next_attempt_at
	// has passed, marking it RUNNING. ok=false means nothing is due.
	ClaimNextDue(ctx context.Context) (Job, bool, error)

	MarkSuccess(ctx context.Context, id string) (Job, error)
	MarkQueuedForRetry(ctx context.Context, id string, attempts int, errMsg string, nextAttemptAt time.Time) (Job, error)
	MarkFailed(ctx context.Context, id string, attempts int, errMsg string) (Job, error)

	// RequeueOrphanedRunning resets any RUNNING job back to QUEUED,
	// returning how many were reset. Called once at Pool startup: with a
	// single worker-pool process, a RUNNING job found at startup can only
	// be orphaned from a crashed prior run (spec section 46).
	RequeueOrphanedRunning(ctx context.Context) (int64, error)

	// ResetForRetry moves a FAILED job back to QUEUED with a fresh
	// attempt budget. ok=false means the job wasn't FAILED (or didn't exist).
	ResetForRetry(ctx context.Context, id string) (Job, bool, error)
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

const selectColumns = `id, type, status, tenant_id, instance_id, node_id,
	attempts, max_attempts, next_attempt_at, error, created_at, started_at, finished_at, trace_context`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(
		&j.ID, &j.Type, &j.Status, &j.TenantID, &j.InstanceID, &j.NodeID,
		&j.Attempts, &j.MaxAttempts, &j.NextAttemptAt, &j.Error,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.TraceContext,
	)
	return j, err
}

func (r *pgxRepository) Create(ctx context.Context, j Job) (Job, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO jobs (type, status, tenant_id, instance_id, node_id, max_attempts, trace_context)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+selectColumns,
		j.Type, j.Status, j.TenantID, j.InstanceID, j.NodeID, j.MaxAttempts, j.TraceContext,
	)
	return scanJob(row)
}

func (r *pgxRepository) List(ctx context.Context, status *Status) ([]Job, error) {
	query := `SELECT ` + selectColumns + ` FROM jobs`
	args := []any{}
	if status != nil {
		query += ` WHERE status = $1`
		args = append(args, *status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *pgxRepository) Get(ctx context.Context, id string) (Job, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM jobs WHERE id = $1`, id)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, errNoRows
	}
	return j, err
}

func (r *pgxRepository) ClaimNextDue(ctx context.Context) (Job, bool, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'RUNNING', started_at = now()
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'QUEUED' AND next_attempt_at <= now()
			ORDER BY next_attempt_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+selectColumns,
	)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

func (r *pgxRepository) MarkSuccess(ctx context.Context, id string) (Job, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'SUCCESS', finished_at = now(), error = NULL
		WHERE id = $1
		RETURNING `+selectColumns,
		id,
	)
	return scanJob(row)
}

func (r *pgxRepository) MarkQueuedForRetry(ctx context.Context, id string, attempts int, errMsg string, nextAttemptAt time.Time) (Job, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'QUEUED', attempts = $2, error = $3, next_attempt_at = $4
		WHERE id = $1
		RETURNING `+selectColumns,
		id, attempts, errMsg, nextAttemptAt,
	)
	return scanJob(row)
}

func (r *pgxRepository) MarkFailed(ctx context.Context, id string, attempts int, errMsg string) (Job, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'FAILED', attempts = $2, error = $3, finished_at = now()
		WHERE id = $1
		RETURNING `+selectColumns,
		id, attempts, errMsg,
	)
	return scanJob(row)
}

func (r *pgxRepository) RequeueOrphanedRunning(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE jobs SET status = 'QUEUED' WHERE status = 'RUNNING'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *pgxRepository) ResetForRetry(ctx context.Context, id string) (Job, bool, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE jobs
		SET status = 'QUEUED', attempts = 0, error = NULL, next_attempt_at = now(),
		    started_at = NULL, finished_at = NULL
		WHERE id = $1 AND status = 'FAILED'
		RETURNING `+selectColumns,
		id,
	)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}
