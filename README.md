# NEBULA

Distributed cloud infrastructure platform in Go. Full spec: `docs/nebula.pdf`.

This repository is built in phases (see spec section 55 onward). Currently
implemented: **Phase 1 — Project Foundation**, **Phase 2 — Authentication**,
and **Phase 3 — Compute Nodes**.

## Phase 1: what's here

- HTTP server (`cmd/nebula-api`) with `/health` (liveness) and `/ready`
  (readiness, checks PostgreSQL) endpoints.
- Environment-based configuration (`internal/common/config.go`).
- PostgreSQL connection pool via pgx (`internal/common/postgres.go`).
- SQL migrations via `golang-migrate` (`migrations/`).
- Docker Compose for local development (`deployments/compose/`).

## Phase 2: what's here

- `users` / `tenants` / `tenant_members` / `refresh_tokens` tables
  (`migrations/000002_auth.*.sql`).
- JWT access tokens + rotating opaque refresh tokens, RBAC roles
  (`SUPER_ADMIN` / `TENANT_ADMIN` / `USER`), bcrypt password hashing
  (`internal/auth/`).
- `POST /api/v1/auth/register` — self-serve signup: creates a tenant, a
  user, and a `TENANT_ADMIN` membership in one transaction, returns tokens.
- `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh` (rotates the
  refresh token), `POST /api/v1/auth/logout` (revokes it).
- `GET /api/v1/me` — authenticated identity, gated by the `Authenticate`
  middleware; the pattern later phases reuse with `RequireRole`.

```
curl -X POST localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a-real-password","tenant_name":"my-co"}'

curl localhost:8080/api/v1/me -H "Authorization: Bearer <access_token>"
```

## Phase 3: what's here

- `compute_nodes` table (`migrations/000003_nodes.*.sql`). Nodes are shared
  platform infrastructure, not tenant-scoped (spec section 6).
- `POST /api/v1/nodes/register`, `GET /api/v1/nodes`, `GET
  /api/v1/nodes/{id}` — gated by `RequireRole(SUPER_ADMIN)` (`internal/node/`).
  Registration returns a one-time opaque `node_token`.
- `POST /api/v1/nodes/{id}/heartbeat` — authenticates with that node token
  (not a user JWT, since the agent runs unattended). Updates load/instance
  count and marks the node `ONLINE`.
- A background **node monitor** (`internal/node/monitor.go`), started from
  `cmd/nebula-api/main.go`, checks every 5s and demotes a node to
  `DEGRADED` (10-30s since last heartbeat) or `OFFLINE` (30s+), per spec
  section 11's thresholds.

`register` only ever creates a `TENANT_ADMIN` (Phase 2's design), so there's
no endpoint yet to mint a `SUPER_ADMIN` — for local testing, promote one by
hand:
```sql
UPDATE tenant_members SET role = 'SUPER_ADMIN' WHERE user_id = '<id>';
```

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
