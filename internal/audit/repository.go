package audit

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the Consumer depends on.
type Repository interface {
	// InsertIdempotent stores a record unless one with the same EventID
	// already exists (spec section 22: "Consumers must be idempotent").
	// Returns inserted=false, nil error on a duplicate — not an error
	// condition, since at-least-once Kafka delivery makes duplicates
	// routine.
	InsertIdempotent(ctx context.Context, r Record) (inserted bool, err error)
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

func (r *pgxRepository) InsertIdempotent(ctx context.Context, rec Record) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO audit_logs (event_id, tenant_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id) DO NOTHING`,
		rec.EventID, rec.TenantID, rec.Action, rec.ResourceType, rec.ResourceID, rec.Metadata,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
