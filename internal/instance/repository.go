package instance

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/milosursulovic/nebula/internal/outbox"
)

// These mirror internal/job's Type/Status/DefaultMaxAttempts constants.
// Duplicated as literals rather than imported so internal/instance doesn't
// depend on internal/job, matching the project's pattern of domain
// packages not depending on each other — spec section 20 fixes these
// exact string values, so drift risk is low.
const (
	createInstanceJobType = "CREATE_INSTANCE"
	jobStatusQueued       = "QUEUED"
	jobDefaultMaxAttempts = 5
)

// transitionEvent maps a target Status to the outbox event spec section 22
// names for it. Statuses with no entry (STOPPING, STOPPED, DELETING) emit
// no event — spec doesn't name one for them.
var transitionEvent = map[Status]string{
	StatusProvisioning: outbox.EventInstanceProvisioningStarted,
	StatusRunning:      outbox.EventInstanceProvisioned,
	StatusError:        outbox.EventInstanceProvisioningFailed,
	StatusDeleted:      outbox.EventInstanceDeleted,
}

// Repository is the persistence boundary the instance Service depends on.
type Repository interface {
	// CreateWithJob inserts the instance, an InstanceCreated outbox event,
	// and its CREATE_INSTANCE job row in one transaction (spec section 23:
	// a DB write and its follow-on effects must commit atomically or not
	// at all — this also closes the instance-created/job-enqueued gap
	// Phase 6 left as two separate, non-atomic writes).
	CreateWithJob(ctx context.Context, in Instance) (Instance, error)

	List(ctx context.Context, tenantID string) ([]Instance, error)
	Get(ctx context.Context, tenantID, id string) (Instance, error)

	// TransitionState applies a compare-and-swap status update plus (when
	// spec names one for the target status) an outbox event, atomically.
	// Returns errNoRows if the instance doesn't exist, belongs to another
	// tenant, or is no longer in the `from` status (a concurrent change
	// raced it).
	TransitionState(ctx context.Context, tenantID, id string, from, to Status) (Instance, error)

	// SetNodeID records which node the saga reserved for this instance.
	// Not a state transition (no outbox event of its own — the Transition
	// calls around it already fire the real ones); guarded to only apply
	// while PROVISIONING. Returns errNoRows if that guard fails.
	SetNodeID(ctx context.Context, tenantID, id, nodeID string) (Instance, error)
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

const selectColumns = `id, tenant_id, name, status, cpu, memory_mb, disk_gb, image, node_id, created_at, updated_at`

func scanInstance(row pgx.Row) (Instance, error) {
	var i Instance
	err := row.Scan(
		&i.ID, &i.TenantID, &i.Name, &i.Status, &i.CPU, &i.MemoryMB, &i.DiskGB,
		&i.Image, &i.NodeID, &i.CreatedAt, &i.UpdatedAt,
	)
	return i, err
}

func (r *pgxRepository) CreateWithJob(ctx context.Context, in Instance) (Instance, error) {
	var created Instance

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO instances (tenant_id, name, status, cpu, memory_mb, disk_gb, image)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING `+selectColumns,
			in.TenantID, in.Name, in.Status, in.CPU, in.MemoryMB, in.DiskGB, in.Image,
		)
		var err error
		created, err = scanInstance(row)
		if err != nil {
			return err
		}

		if _, err := outbox.InsertTx(ctx, tx, outbox.EventInstanceCreated, outbox.AggregateInstance, created.ID, &created.TenantID); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO jobs (type, status, tenant_id, instance_id, max_attempts)
			VALUES ($1, $2, $3, $4, $5)`,
			createInstanceJobType, jobStatusQueued, created.TenantID, created.ID, jobDefaultMaxAttempts,
		)
		return err
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Instance{}, ErrNameTaken
		}
		return Instance{}, err
	}

	return created, nil
}

func (r *pgxRepository) List(ctx context.Context, tenantID string) ([]Instance, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+selectColumns+` FROM instances WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, i)
	}
	return instances, rows.Err()
}

func (r *pgxRepository) Get(ctx context.Context, tenantID, id string) (Instance, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM instances WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	i, err := scanInstance(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Instance{}, errNoRows
	}
	return i, err
}

func (r *pgxRepository) TransitionState(ctx context.Context, tenantID, id string, from, to Status) (Instance, error) {
	var updated Instance

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE instances SET status = $4, updated_at = now()
			WHERE id = $1 AND tenant_id = $2 AND status = $3
			RETURNING `+selectColumns,
			id, tenantID, from, to,
		)
		var err error
		updated, err = scanInstance(row)
		if err != nil {
			return err
		}

		if eventType, ok := transitionEvent[to]; ok {
			if _, err := outbox.InsertTx(ctx, tx, eventType, outbox.AggregateInstance, updated.ID, &updated.TenantID); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Instance{}, errNoRows
	}
	if err != nil {
		return Instance{}, err
	}
	return updated, nil
}

func (r *pgxRepository) SetNodeID(ctx context.Context, tenantID, id, nodeID string) (Instance, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE instances SET node_id = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2 AND status = 'PROVISIONING'
		RETURNING `+selectColumns,
		id, tenantID, nodeID,
	)
	i, err := scanInstance(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Instance{}, errNoRows
	}
	return i, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
