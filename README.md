# NEBULA

Distributed cloud infrastructure platform in Go. Full spec: `docs/nebula.pdf`.

This repository is built in phases (see spec section 55 onward). Currently
implemented: **Phase 1 — Project Foundation** and **Phase 2 — Authentication**.

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
