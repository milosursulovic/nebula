package node

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the node Service depends on.
type Repository interface {
	Create(ctx context.Context, n Node) (Node, error)
	List(ctx context.Context) ([]Node, error)
	Get(ctx context.Context, id string) (Node, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (Node, error)

	// UpdateHeartbeat persists a fresh heartbeat and sets status.
	UpdateHeartbeat(ctx context.Context, id string, loadAverage float64, runningInstances int, status Status) error

	// UpdateStatus persists only a status transition (used by the monitor).
	UpdateStatus(ctx context.Context, id string, status Status) error

	// TryReserve attempts a single compare-and-swap capacity decrement,
	// guarded by expectedVersion (spec section 17's optimistic concurrency
	// example). ok=false with a nil error means the version no longer
	// matched (a concurrent writer got there first) — the caller re-reads
	// and retries; it does not check capacity itself.
	TryReserve(ctx context.Context, id string, expectedVersion int64, cpu, memoryMB, diskGB int) (Node, bool, error)

	// TryRelease is the symmetric compare-and-swap capacity increment.
	TryRelease(ctx context.Context, id string, expectedVersion int64, cpu, memoryMB, diskGB int) (Node, bool, error)
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

const selectColumns = `id, hostname, ip, status, total_cpu, available_cpu,
	total_memory_mb, available_memory_mb, total_disk_gb, available_disk_gb,
	load_average, running_instances, last_heartbeat_at, token_hash, version, created_at, updated_at`

func scanNode(row pgx.Row) (Node, error) {
	var n Node
	err := row.Scan(
		&n.ID, &n.Hostname, &n.IP, &n.Status, &n.TotalCPU, &n.AvailableCPU,
		&n.TotalMemoryMB, &n.AvailableMemoryMB, &n.TotalDiskGB, &n.AvailableDiskGB,
		&n.LoadAverage, &n.RunningInstances, &n.LastHeartbeatAt, &n.TokenHash, &n.Version,
		&n.CreatedAt, &n.UpdatedAt,
	)
	return n, err
}

func (r *pgxRepository) Create(ctx context.Context, n Node) (Node, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO compute_nodes (
			hostname, ip, status, total_cpu, available_cpu,
			total_memory_mb, available_memory_mb, total_disk_gb, available_disk_gb,
			load_average, running_instances, last_heartbeat_at, token_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+selectColumns,
		n.Hostname, n.IP, n.Status, n.TotalCPU, n.AvailableCPU,
		n.TotalMemoryMB, n.AvailableMemoryMB, n.TotalDiskGB, n.AvailableDiskGB,
		n.LoadAverage, n.RunningInstances, n.LastHeartbeatAt, n.TokenHash,
	)

	created, err := scanNode(row)
	if err != nil {
		if isUniqueViolation(err) {
			return Node{}, ErrHostnameTaken
		}
		return Node{}, err
	}
	return created, nil
}

func (r *pgxRepository) List(ctx context.Context) ([]Node, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM compute_nodes ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (r *pgxRepository) Get(ctx context.Context, id string) (Node, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM compute_nodes WHERE id = $1`, id)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, errNoRows
	}
	return n, err
}

func (r *pgxRepository) FindByTokenHash(ctx context.Context, tokenHash string) (Node, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM compute_nodes WHERE token_hash = $1`, tokenHash)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, errNoRows
	}
	return n, err
}

func (r *pgxRepository) UpdateHeartbeat(ctx context.Context, id string, loadAverage float64, runningInstances int, status Status) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE compute_nodes
		SET load_average = $2, running_instances = $3, status = $4,
		    last_heartbeat_at = now(), updated_at = now()
		WHERE id = $1`,
		id, loadAverage, runningInstances, status,
	)
	return err
}

func (r *pgxRepository) UpdateStatus(ctx context.Context, id string, status Status) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE compute_nodes SET status = $2, updated_at = now() WHERE id = $1`,
		id, status,
	)
	return err
}

func (r *pgxRepository) TryReserve(ctx context.Context, id string, expectedVersion int64, cpu, memoryMB, diskGB int) (Node, bool, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE compute_nodes
		SET available_cpu = available_cpu - $3,
		    available_memory_mb = available_memory_mb - $4,
		    available_disk_gb = available_disk_gb - $5,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1 AND version = $2
		RETURNING `+selectColumns,
		id, expectedVersion, cpu, memoryMB, diskGB,
	)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, err
	}
	return n, true, nil
}

func (r *pgxRepository) TryRelease(ctx context.Context, id string, expectedVersion int64, cpu, memoryMB, diskGB int) (Node, bool, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE compute_nodes
		SET available_cpu = available_cpu + $3,
		    available_memory_mb = available_memory_mb + $4,
		    available_disk_gb = available_disk_gb + $5,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1 AND version = $2
		RETURNING `+selectColumns,
		id, expectedVersion, cpu, memoryMB, diskGB,
	)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, err
	}
	return n, true, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
