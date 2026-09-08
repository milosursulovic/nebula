package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the storage Service depends on.
type Repository interface {
	Create(ctx context.Context, d Disk) (Disk, error)
	Get(ctx context.Context, id string) (Disk, error)
	ListByTenant(ctx context.Context, tenantID string) ([]Disk, error)
	ListByInstance(ctx context.Context, instanceID string) ([]Disk, error)

	// GetRootByInstance finds an instance's ROOT disk, if any. Returns
	// errNoRows if there isn't one (already deleted, or never created).
	GetRootByInstance(ctx context.Context, instanceID string) (Disk, error)

	// Attach sets instance_id — only if the row is currently detached
	// (guarded in the WHERE clause, not a separate check-then-set).
	Attach(ctx context.Context, id, instanceID string) (Disk, error)

	// Detach clears instance_id — only if currently attached.
	Detach(ctx context.Context, id string) (Disk, error)

	// Resize updates size_gb — only if newSizeGB is not smaller than the
	// current size (guarded in the WHERE clause).
	Resize(ctx context.Context, id string, newSizeGB int) (Disk, error)

	// Delete removes the row — only if currently detached.
	Delete(ctx context.Context, id string) error
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

const selectColumns = `id, tenant_id, instance_id, node_id, type, size_gb, file_path, created_at, updated_at`

func scanDisk(row pgx.Row) (Disk, error) {
	var d Disk
	err := row.Scan(&d.ID, &d.TenantID, &d.InstanceID, &d.NodeID, &d.Type, &d.SizeGB, &d.FilePath, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// Create inserts d with a caller-supplied ID (rather than a DB-generated
// one) — the disk's file on the agent is named by this same ID, created
// before the row exists, so the ID has to be chosen up front.
func (r *pgxRepository) Create(ctx context.Context, d Disk) (Disk, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO disks (id, tenant_id, instance_id, node_id, type, size_gb, file_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+selectColumns,
		d.ID, d.TenantID, d.InstanceID, d.NodeID, d.Type, d.SizeGB, d.FilePath,
	)
	return scanDisk(row)
}

func (r *pgxRepository) Get(ctx context.Context, id string) (Disk, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM disks WHERE id = $1`, id)
	d, err := scanDisk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Disk{}, errNoRows
	}
	return d, err
}

func (r *pgxRepository) ListByTenant(ctx context.Context, tenantID string) ([]Disk, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM disks WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var disks []Disk
	for rows.Next() {
		d, err := scanDisk(rows)
		if err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, rows.Err()
}

func (r *pgxRepository) ListByInstance(ctx context.Context, instanceID string) ([]Disk, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM disks WHERE instance_id = $1 ORDER BY created_at`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var disks []Disk
	for rows.Next() {
		d, err := scanDisk(rows)
		if err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, rows.Err()
}

func (r *pgxRepository) GetRootByInstance(ctx context.Context, instanceID string) (Disk, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM disks WHERE instance_id = $1 AND type = 'ROOT'`, instanceID)
	d, err := scanDisk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Disk{}, errNoRows
	}
	return d, err
}

func (r *pgxRepository) Attach(ctx context.Context, id, instanceID string) (Disk, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE disks SET instance_id = $2, updated_at = now()
		WHERE id = $1 AND instance_id IS NULL
		RETURNING `+selectColumns,
		id, instanceID,
	)
	d, err := scanDisk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Disk{}, errNoRows
	}
	return d, err
}

func (r *pgxRepository) Detach(ctx context.Context, id string) (Disk, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE disks SET instance_id = NULL, updated_at = now()
		WHERE id = $1 AND instance_id IS NOT NULL
		RETURNING `+selectColumns,
		id,
	)
	d, err := scanDisk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Disk{}, errNoRows
	}
	return d, err
}

func (r *pgxRepository) Resize(ctx context.Context, id string, newSizeGB int) (Disk, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE disks SET size_gb = $2, updated_at = now()
		WHERE id = $1 AND size_gb <= $2
		RETURNING `+selectColumns,
		id, newSizeGB,
	)
	d, err := scanDisk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Disk{}, errNoRows
	}
	return d, err
}

func (r *pgxRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM disks WHERE id = $1 AND instance_id IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNoRows
	}
	return nil
}
