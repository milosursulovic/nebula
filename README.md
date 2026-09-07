# NEBULA

A miniature cloud infrastructure platform in Go — conceptually a simplified
combination of AWS EC2, OpenStack, Kubernetes' control plane, HashiCorp
Nomad, and Proxmox. Full design spec: `docs/nebula.pdf`.

## What's here

**HTTP API** (`cmd/nebula-api`)
- `/health` (liveness) and `/ready` (readiness, checks PostgreSQL).
- Env-based configuration with fail-fast validation
  (`internal/common/config.go`).
- Graceful shutdown on SIGINT/SIGTERM.

**Authentication & RBAC** (`internal/auth/`)
- JWT access tokens + rotating opaque refresh tokens; bcrypt password
  hashing; roles `SUPER_ADMIN` / `TENANT_ADMIN` / `USER`.
- `POST /api/v1/auth/register` — self-serve signup: creates a tenant, a
  user, and a `TENANT_ADMIN` membership in one transaction, returns tokens.
- `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh` (rotates the
  refresh token), `POST /api/v1/auth/logout` (revokes it).
- `GET /api/v1/me` — authenticated identity.

```
curl -X POST localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a-real-password","tenant_name":"my-co"}'

curl localhost:8080/api/v1/me -H "Authorization: Bearer <access_token>"
```

`register` only ever creates a `TENANT_ADMIN` — there's no endpoint yet to
mint a `SUPER_ADMIN`. For local testing, promote one by hand:
```sql
UPDATE tenant_members SET role = 'SUPER_ADMIN' WHERE user_id = '<id>';
```

**Compute nodes** (`internal/node/`)
- Nodes are shared platform infrastructure, not tenant-scoped.
- `POST /api/v1/nodes/register`, `GET /api/v1/nodes`, `GET
  /api/v1/nodes/{id}` — gated by `RequireRole(SUPER_ADMIN)`. Registration
  returns a one-time opaque `node_token`.
- `POST /api/v1/nodes/{id}/heartbeat` — authenticated with that node token
  (not a user JWT, since the agent runs unattended).
- A background **node monitor** checks every 5s and demotes a node to
  `DEGRADED` (10-30s since last heartbeat) or `OFFLINE` (30s+).

**Instances** (`internal/instance/`)
- Instances are tenant-scoped (unlike nodes) — every query is scoped to the
  caller's `tenant_id` from their JWT, never a client-supplied value.
- `POST /api/v1/instances`, `GET /api/v1/instances`, `GET
  /api/v1/instances/{id}`, `DELETE /api/v1/instances/{id}` — any
  authenticated tenant member. Create returns immediately with
  `status: "PENDING"`; provisioning is not yet wired up (no scheduler/worker
  exists), so instances stay virtual records for now — no KVM.
- An explicit state machine (`PENDING → PROVISIONING → RUNNING → STOPPING →
  STOPPED`, plus `ERROR`/`DELETING`/`DELETED`) rejects invalid transitions;
  delete is a soft two-hop `→ DELETING → DELETED`.
- Cross-tenant access returns `404`, not `403` (no existence leak).

**Scheduler & resource reservation** (`internal/scheduler/`, `internal/node/`)
- `Scheduler` interface with four strategies — FirstFit, BestFit
  (tightest normalized leftover capacity), LeastLoaded, and Weighted
  (`cpu*0.30 + memory*0.30 + disk*0.15 + load*0.15 + instances*0.10`) — only
  considering `ONLINE` nodes with enough capacity. Not wired into instance
  creation yet; that arrives with the job worker.
- `node.Service.Reserve`/`Release` do transactional capacity accounting on
  `compute_nodes` via optimistic concurrency (a `version` column, retried
  with jittered backoff on conflict) — never a `SELECT ... FOR UPDATE`
  lock, matching the worked example in the spec.
- The mandatory 100-concurrent-request test
  (`internal/node/reserve_concurrency_test.go`) proves this against a real
  Postgres (gated on `NEBULA_DATABASE_URL`, skipped otherwise): available
  capacity never goes negative and every request resolves to success or a
  legitimate rejection.

**Persistence & infra**
- PostgreSQL via pgx (`internal/common/postgres.go`), SQL migrations via
  `golang-migrate` (`migrations/`).
- Docker Compose for local development (`deployments/compose/`).

## Running locally

Requires Go 1.25+, Docker, and the `golang-migrate` CLI:

```
go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Start Postgres and the API:

```
make compose-up
make migrate-up
curl localhost:8080/health
curl localhost:8080/ready
```

Or run the API against your own Postgres:

```
cp configs/.env.example .env   # edit as needed
export $(cat .env | xargs)
make migrate-up
make run
```

## Testing

```
make test
make vet
```
