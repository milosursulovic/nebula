<p align="center">
  <img src="docs/assets/logo.svg" alt="NEBULA logo" width="120" height="120">
</p>

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
- `POST /api/v1/nodes/register` — accepts either a pre-shared
  `NEBULA_NODE_BOOTSTRAP_SECRET` bearer token (how an unattended
  `nebula-agent` registers itself, spec section 10) or a `SUPER_ADMIN` JWT
  (manual/human registration). Returns a one-time opaque `node_token`.
- `GET /api/v1/nodes`, `GET /api/v1/nodes/{id}` — gated by
  `RequireRole(SUPER_ADMIN)`.
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
  `status: "PENDING"` and enqueues a `CREATE_INSTANCE` job; a worker then
  runs the provisioning saga (see below), landing the instance on
  `RUNNING` with a real `node_id` — or `ERROR` if provisioning failed.
  Delete tears down the VM on the node's agent and releases the instance's
  reserved node capacity (both best-effort) if it had been scheduled onto
  one.
- An explicit state machine (`PENDING → PROVISIONING → RUNNING → STOPPING →
  STOPPED`, plus `ERROR`/`DELETING`/`DELETED`) rejects invalid transitions;
  `ERROR → PROVISIONING` lets a job retry re-enter the saga; delete is a
  soft two-hop `→ DELETING → DELETED`.
- Cross-tenant access returns `404`, not `403` (no existence leak).

**Scheduler & resource reservation** (`internal/scheduler/`, `internal/node/`)
- `Scheduler` interface with four strategies — FirstFit, BestFit
  (tightest normalized leftover capacity), LeastLoaded, and Weighted
  (`cpu*0.30 + memory*0.30 + disk*0.15 + load*0.15 + instances*0.10`) — only
  considering `ONLINE` nodes with enough capacity. Selected via
  `NEBULA_SCHEDULER_STRATEGY` (default `weighted`) and driven by the
  provisioning saga on every instance create.
- `node.Service.Reserve`/`Release` do transactional capacity accounting on
  `compute_nodes` via optimistic concurrency (a `version` column, retried
  with jittered backoff on conflict) — never a `SELECT ... FOR UPDATE`
  lock, matching the worked example in the spec.
- The mandatory 100-concurrent-request test
  (`internal/node/reserve_concurrency_test.go`) proves this against a real
  Postgres (gated on `NEBULA_DATABASE_URL`, skipped otherwise): available
  capacity never goes negative and every request resolves to success or a
  legitimate rejection.

**Provisioning saga** (`internal/provisioning/`)
- Drives each `CREATE_INSTANCE` job attempt through: schedule a node →
  reserve its capacity → set `node_id` → create disk → create network →
  create VM → start VM → transition to `RUNNING`. Disk/network steps are
  local mocks (simulated work, always succeed) — real implementations are
  Phase 12 (Networking) and Phase 13 (Storage)'s job. Resource reservation
  is real, and VM steps are real gRPC calls (TLS-secured) to the target
  node's `nebula-agent` (see below) — the agent's own VM backend is still
  mocked (Phase 11/KVM's job), but a real, authenticated network hop
  happens now, not an in-process function call.
- Any step's failure compensates in reverse order — exactly the spec's
  worked example: create VM fails → delete network → delete disk →
  release reservation — then transitions the instance to `ERROR`. A job
  retry re-invokes the saga from scratch, re-scheduling fresh (possibly
  onto a different node) rather than assuming the prior choice still
  holds.

**Nebula Agent** (`cmd/nebula-agent`, `internal/agent/`, `internal/agentpb/`)
- A separate binary that runs on each compute node (spec section 27) — the
  abstraction layer between the control plane and the machine, so
  `nebula-api` never executes anything directly on a node.
- On startup: registers itself with `nebula-api` over REST (retried with
  backoff — fails fast, no retries, on a permanent `409` like an
  already-registered hostname) and starts a heartbeat loop, posting real
  host metrics (CPU% from `/proc/stat` deltas, memory from
  `/proc/meminfo`, disk from `statfs`, load average from `/proc/loadavg`)
  every `NEBULA_AGENT_HEARTBEAT_INTERVAL` (default `5s`). This direction
  (agent → control plane) is unchanged since Phase 9.
- Runs a TLS-secured gRPC server (`NEBULA_AGENT_PORT`, default `7071`) —
  `internal/agentpb/` is generated (via `buf generate`, see `proto/
  nebula_agent.proto`) from spec section 28's `NebulaAgent` service:
  `GetNodeInfo`, `CreateVM`, `DeleteVM`, `StartVM`, `StopVM`,
  `GetVMStatus`. This is the control-plane → agent direction; Phase 9's
  REST version of this same surface is fully replaced, not kept
  alongside. Server reflection is enabled (`grpcurl` works without the
  `.proto` file). TLS is server-authenticated only — the agent presents a
  cert, `nebula-api` verifies it against a pinned dev cert
  (`NEBULA_AGENT_TLS_CA_FILE`); mTLS (the agent verifying *its* caller)
  is explicitly deferred (spec: "Later implement mTLS").
- VM operations run behind a `Hypervisor` interface (spec section 30,
  `internal/agent/hypervisor.go`) with two implementations, selected via
  `NEBULA_AGENT_HYPERVISOR` (default `mock`):
  - `MockHypervisor` — the same in-memory `Store` from Phase 9, wrapped
    to satisfy the interface. What Docker Compose runs.
  - `LibvirtHypervisor` (`NEBULA_AGENT_HYPERVISOR=libvirt`) — real
    libvirt/KVM via `libvirt.org/go/libvirt`, gated behind a `libvirt` Go
    build tag (needs CGO + `libvirt-dev`, not part of the default
    build/Docker image). Domains it creates are deliberately
    headless/diskless/netless (real disks and networking are Phase 13
    and Phase 12's job) — this phase's honest scope is the real
    create/start/stop/delete/status lifecycle against actual KVM, proven
    by a test gated on real libvirtd connectivity
    (`go test -tags libvirt ./internal/agent/...`, same
    skip-cleanly-when-unavailable pattern as the Postgres concurrency
    test). `qemu:///system` by default (`NEBULA_AGENT_LIBVIRT_URI`).
- The dev TLS cert (`deployments/certs/`) is a static, checked-in
  self-signed cert+key — same "dev-only, change-me" precedent as the
  plaintext `JWT_SECRET`/`NEBULA_NODE_BOOTSTRAP_SECRET` values already in
  `docker-compose.yml`. See `deployments/certs/README.md`.
- Registration is not idempotent across restarts — a restarted agent with
  the same hostname gets rejected and exits; recovering it today needs a
  hostname change or removing the stale `compute_nodes` row by hand
  (accepted Phase 9 limitation, same idempotency-deferred precedent as
  instance creation).

**Jobs** (`internal/job/`)
- A channel-based worker pool (dispatcher goroutine polls PostgreSQL for
  due `QUEUED` jobs, claims one via `SELECT ... FOR UPDATE SKIP LOCKED`,
  fans it out to `NEBULA_WORKER_COUNT` — default 3 — worker goroutines).
- Exponential backoff with jitter on failure (1s/2s/4s/8s/16s, spec's
  table) via a durable `next_attempt_at` column — no in-process sleep
  timers, survives a restart. After 5 attempts a job is `FAILED` — that
  status **is** the dead letter queue, no separate table.
- `GET /api/v1/jobs` (optional `?status=`), `GET /api/v1/jobs/{id}`,
  `POST /api/v1/jobs/{id}/retry` (`FAILED → QUEUED`, fresh attempt
  budget) — `RequireRole(SUPER_ADMIN)`, same as nodes.
- On startup, any job still `RUNNING` is requeued — with one worker-pool
  process, that can only mean an orphaned job from a crashed prior run.
- Only `CREATE_INSTANCE` has a registered handler — it drives the
  provisioning saga above; the other 7 job types spec names exist in the
  schema for when networking/storage/start-stop arrive.

**Kafka & the transactional outbox** (`internal/outbox/`, `internal/audit/`)
- Domain events (`InstanceCreated`, `InstanceProvisioningStarted`,
  `InstanceProvisioned`, `InstanceProvisioningFailed`, `InstanceDeleted`,
  `NodeOnline`, `NodeOffline`) are written to an `outbox_events` row in the
  **same transaction** as the state change that causes them — e.g.
  `instance.Repository.CreateWithJob` inserts the instance, its outbox
  event, and its `CREATE_INSTANCE` job row atomically, closing the
  non-atomic create-then-enqueue gap Phase 6 left open. A separate
  **outbox publisher** goroutine drains unpublished rows to Kafka and marks
  them published — the DB write can never succeed while the "event" it
  implies silently vanishes.
- **Consumer**: `internal/audit` reads the same events and writes
  `audit_logs` (`INSTANCE_CREATED`, `NODE_OFFLINE`, ...) via `INSERT ...
  ON CONFLICT (event_id) DO NOTHING` — idempotent against Kafka's
  at-least-once redelivery and safe to replay after a restart.
- Client: `github.com/segmentio/kafka-go` (pure Go, no CGO/librdkafka,
  matching the existing `CGO_ENABLED=0` build). Compose runs a single-node
  KRaft `apache/kafka` broker — no separate Zookeeper container.

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

Regenerating `internal/agentpb/` after editing `proto/nebula_agent.proto`
requires [`buf`](https://buf.build) plus the Go protoc plugins (all
`go install`-able, no `protoc`/`sudo` needed):

```
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
make proto-gen
```

## Testing

```
make test
make vet
```

The `LibvirtHypervisor` backend (`internal/agent/libvirt_hypervisor.go`)
is behind a `libvirt` build tag and needs CGO + `libvirt-dev` — the
default `make test`/`make vet` never touch it. To build/test it on a
Linux machine with libvirt installed:

```
sudo apt-get install libvirt-dev   # or your distro's equivalent
go build -tags libvirt ./...
go test -tags libvirt ./internal/agent/...
```

`libvirt.org/go/libvirt` is only imported by that tagged file, so a
plain `go mod tidy` will see it as unused and strip it from `go.mod` —
regenerate with `GOFLAGS=-tags=libvirt go mod tidy` instead.
