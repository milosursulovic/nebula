package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the network Service depends on.
type Repository interface {
	// CreateNetworkWithSubnet creates a network, its one subnet, and the
	// subnet's full IP pool (one row per usable host address) in a single
	// transaction — matches spec section 31's single worked example
	// bundling network+subnet+gateway together.
	CreateNetworkWithSubnet(ctx context.Context, name, cidr, gateway string, usableIPs []string) (Network, Subnet, error)

	List(ctx context.Context) ([]Network, error)
	Get(ctx context.Context, id string) (Network, error)
	SubnetsByNetwork(ctx context.Context, networkID string) ([]Subnet, error)

	// AllocateForInstance is idempotent by instanceID: an existing
	// ALLOCATED row for this instance is returned unchanged; otherwise
	// one AVAILABLE row is claimed via SELECT ... FOR UPDATE SKIP LOCKED
	// (same row-locking queue-claim pattern as job dispatch).
	AllocateForInstance(ctx context.Context, instanceID string) (string, error)

	// ReleaseForInstance flips instanceID's allocated row back to
	// AVAILABLE; a no-op if it has none (idempotent, same shape as
	// node.Service.Release/agent.Store.Delete).
	ReleaseForInstance(ctx context.Context, instanceID string) error

	// ReserveIP flips one exact AVAILABLE row to RESERVED.
	ReserveIP(ctx context.Context, subnetID, ip string) error
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

const networkColumns = `id, name, created_at`
const subnetColumns = `id, network_id, cidr, gateway, created_at`

func scanNetwork(row pgx.Row) (Network, error) {
	var n Network
	err := row.Scan(&n.ID, &n.Name, &n.CreatedAt)
	return n, err
}

func scanSubnet(row pgx.Row) (Subnet, error) {
	var s Subnet
	err := row.Scan(&s.ID, &s.NetworkID, &s.CIDR, &s.Gateway, &s.CreatedAt)
	return s, err
}

func (r *pgxRepository) CreateNetworkWithSubnet(ctx context.Context, name, cidr, gateway string, usableIPs []string) (Network, Subnet, error) {
	var network Network
	var subnet Subnet

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO networks (name) VALUES ($1)
			RETURNING `+networkColumns,
			name,
		)
		var err error
		network, err = scanNetwork(row)
		if err != nil {
			return err
		}

		row = tx.QueryRow(ctx, `
			INSERT INTO subnets (network_id, cidr, gateway) VALUES ($1, $2, $3)
			RETURNING `+subnetColumns,
			network.ID, cidr, gateway,
		)
		subnet, err = scanSubnet(row)
		if err != nil {
			return err
		}

		if len(usableIPs) == 0 {
			return nil
		}
		rows := make([][]any, len(usableIPs))
		for i, ip := range usableIPs {
			// CopyFrom always uses binary format, which needs a
			// netip.Addr (pgx's native inet-encodable type) — a plain
			// string has no binary-inet encode plan.
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				return fmt.Errorf("parse ip %q: %w", ip, err)
			}
			rows[i] = []any{subnet.ID, addr, string(IPStatusAvailable)}
		}
		_, err = tx.CopyFrom(ctx,
			pgx.Identifier{"ip_addresses"},
			[]string{"subnet_id", "ip_address", "status"},
			pgx.CopyFromRows(rows),
		)
		return err
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Network{}, Subnet{}, ErrNameTaken
		}
		return Network{}, Subnet{}, err
	}

	return network, subnet, nil
}

func (r *pgxRepository) List(ctx context.Context) ([]Network, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+networkColumns+` FROM networks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var networks []Network
	for rows.Next() {
		n, err := scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

func (r *pgxRepository) Get(ctx context.Context, id string) (Network, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+networkColumns+` FROM networks WHERE id = $1`, id)
	n, err := scanNetwork(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Network{}, errNoRows
	}
	return n, err
}

func (r *pgxRepository) SubnetsByNetwork(ctx context.Context, networkID string) ([]Subnet, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+subnetColumns+` FROM subnets WHERE network_id = $1 ORDER BY created_at`, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subnets []Subnet
	for rows.Next() {
		s, err := scanSubnet(rows)
		if err != nil {
			return nil, err
		}
		subnets = append(subnets, s)
	}
	return subnets, rows.Err()
}

func (r *pgxRepository) AllocateForInstance(ctx context.Context, instanceID string) (string, error) {
	// Idempotent: a job retry re-running Provision() from scratch after a
	// crash between a successful allocation and the next saga step must
	// not leak a second address for the same instance.
	row := r.pool.QueryRow(ctx, `
		SELECT host(ip_address) FROM ip_addresses
		WHERE instance_id = $1 AND status = 'ALLOCATED'`,
		instanceID,
	)
	var existing string
	if err := row.Scan(&existing); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	row = r.pool.QueryRow(ctx, `
		UPDATE ip_addresses SET status = 'ALLOCATED', instance_id = $1
		WHERE id = (
			SELECT id FROM ip_addresses
			WHERE status = 'AVAILABLE'
			ORDER BY subnet_id, ip_address
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING host(ip_address)`,
		instanceID,
	)
	var ip string
	err := row.Scan(&ip)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoCapacity
	}
	return ip, err
}

func (r *pgxRepository) ReleaseForInstance(ctx context.Context, instanceID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ip_addresses SET status = 'AVAILABLE', instance_id = NULL
		WHERE instance_id = $1 AND status = 'ALLOCATED'`,
		instanceID,
	)
	return err
}

func (r *pgxRepository) ReserveIP(ctx context.Context, subnetID, ip string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ip_addresses SET status = 'RESERVED'
		WHERE subnet_id = $1 AND host(ip_address) = $2 AND status = 'AVAILABLE'`,
		subnetID, ip,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrIPNotAvailable
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
