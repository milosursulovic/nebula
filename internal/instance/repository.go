package instance

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the instance Service depends on.
type Repository interface {
	Create(ctx context.Context, in Instance) (Instance, error)
	List(ctx context.Context, tenantID string) ([]Instance, error)
	Get(ctx context.Context, tenantID, id string) (Instance, error)

	// TransitionState applies a compare-and-swap status update: it only
	// succeeds if the instance's current status is still `from`. Returns
	// errNoRows if the instance doesn't exist, belongs to another tenant,
	// or is no longer in the `from` status (a concurrent change raced it).
	TransitionState(ctx context.Context, tenantID, id string, from, to Status) (Instance, error)
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

func (r *pgxRepository) Create(ctx context.Context, in Instance) (Instance, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO instances (tenant_id, name, status, cpu, memory_mb, disk_gb, image)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+selectColumns,
		in.TenantID, in.Name, in.Status, in.CPU, in.MemoryMB, in.DiskGB, in.Image,
	)

	created, err := scanInstance(row)
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
	row := r.pool.QueryRow(ctx, `
		UPDATE instances SET status = $4, updated_at = now()
		WHERE id = $1 AND tenant_id = $2 AND status = $3
		RETURNING `+selectColumns,
		id, tenantID, from, to,
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
