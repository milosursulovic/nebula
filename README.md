# NEBULA

Distributed cloud infrastructure platform in Go. Full spec: `docs/nebula.pdf`.

This repository is built in phases (see spec section 55 onward). Currently
implemented: **Phase 1 — Project Foundation**.

## Phase 1: what's here

- HTTP server (`cmd/nebula-api`) with `/health` (liveness) and `/ready`
  (readiness, checks PostgreSQL) endpoints.
- Environment-based configuration (`internal/common/config.go`).
- PostgreSQL connection pool via pgx (`internal/common/postgres.go`).
- SQL migrations via `golang-migrate` (`migrations/`).
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
