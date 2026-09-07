package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newEvent builds the Event (including its would-be payload) for
// eventType/aggregateType/aggregateID/tenantID. Split out from InsertTx so
// the payload shape (spec section 22's example: event_id, event_type,
// tenant_id, <aggregate>_id, timestamp) is unit-testable without a
// database. tenantID may be nil (node events aren't tenant-scoped).
func newEvent(eventType, aggregateType, aggregateID string, tenantID *string) (Event, error) {
	id := uuid.NewString()
	now := time.Now().UTC()

	payload, err := json.Marshal(map[string]any{
		"event_id":            id,
		"event_type":          eventType,
		"tenant_id":           tenantID,
		aggregateType + "_id": aggregateID,
		"timestamp":           now.Format(time.RFC3339),
	})
	if err != nil {
		return Event{}, err
	}

	return Event{
		ID: id, EventType: eventType, AggregateType: aggregateType,
		AggregateID: aggregateID, Payload: payload, CreatedAt: now,
	}, nil
}

// InsertTx records an event as part of the CALLER's own transaction (spec
// section 23: the DB write and the outbox row must commit atomically, or
// not at all). It is a free function, not a Repository method, precisely
// because it has no state of its own — it only ever runs inside a tx
// another repository already opened (e.g. instance.Repository.Create).
func InsertTx(ctx context.Context, tx pgx.Tx, eventType, aggregateType, aggregateID string, tenantID *string) (Event, error) {
	e, err := newEvent(eventType, aggregateType, aggregateID, tenantID)
	if err != nil {
		return Event{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, event_type, aggregate_type, aggregate_id, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID, e.EventType, e.AggregateType, e.AggregateID, e.Payload, e.CreatedAt,
	); err != nil {
		return Event{}, err
	}

	return e, nil
}

// Repository is the persistence boundary the Publisher depends on.
type Repository interface {
	ListUnpublished(ctx context.Context, limit int) ([]Event, error)
	MarkPublished(ctx context.Context, id string) error
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

func (r *pgxRepository) ListUnpublished(ctx context.Context, limit int) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_type, aggregate_type, aggregate_id, payload, created_at, published_at
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventType, &e.AggregateType, &e.AggregateID, &e.Payload, &e.CreatedAt, &e.PublishedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r *pgxRepository) MarkPublished(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE id = $1`, id)
	return err
}
