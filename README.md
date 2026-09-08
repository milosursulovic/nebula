<p align="center">
  <img src="docs/assets/logo.png" alt="NEBULA logo" width="120" height="120">
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
- `GET /api/v1/tenants` — every tenant, `RequireRole(SUPER_ADMIN)` (spec
  section 8: "SUPER_ADMIN -> manage tenants"), same gate as nodes/jobs.

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
  A hostname whose existing row is `OFFLINE` is reclaimed in place (same
  node ID, fresh token, back to `ONLINE`) rather than rejected — a
  restarted agent registers under the same hostname it always has, so
  without this a node that ever went offline could never come back
  (rejected forever, its agent process exhausting its registration retry
  budget and exiting for good). A hostname still held by a live
  (`ONLINE`/`DEGRADED`/`DRAINING`) node is still rejected as before.
- `GET /api/v1/nodes`, `GET /api/v1/nodes/{id}` — gated by
  `RequireRole(SUPER_ADMIN)`.
- `POST /api/v1/nodes/{id}/heartbeat` — authenticated with that node token
  (not a user JWT, since the agent runs unattended).
- A background **node monitor** checks every 5s and demotes a node to
  `DEGRADED` (10-30s since last heartbeat) or `OFFLINE` (30s+).
- `POST /api/v1/nodes/{id}/drain` (`SUPER_ADMIN`) — marks a node
  `DRAINING`, taking it out of scheduling rotation ahead of maintenance;
  the scheduler already only considers `ONLINE` nodes and the monitor
  already leaves `DRAINING` nodes alone, so this is what actually sets the
  status the rest of the system already respects. A drained node's own
  agent keeps heartbeating, though — heartbeat handling deliberately does
  *not* clobber an existing `DRAINING` status back to `ONLINE`, or drain
  would silently undo itself on the node's next heartbeat (default 5s).

**Instances** (`internal/instance/`)
- Instances are tenant-scoped (unlike nodes) — every query is scoped to the
  caller's `tenant_id` from their JWT, never a client-supplied value.
- `POST /api/v1/instances`, `GET /api/v1/instances`, `GET
  /api/v1/instances/{id}`, `DELETE /api/v1/instances/{id}` — any
  authenticated tenant member. Create returns immediately with
  `status: "PENDING"` and enqueues a `CREATE_INSTANCE` job; a worker then
  runs the provisioning saga (see below), landing the instance on
  `RUNNING` with a real `node_id` and `ip_address` — or `ERROR` if
  provisioning failed. Delete tears down the VM on the node's agent and
  releases the instance's reserved node capacity and allocated IP (all
  best-effort) if it had gotten that far.
- An explicit state machine (`PENDING → PROVISIONING → RUNNING → STOPPING →
  STOPPED`, plus `ERROR`/`DELETING`/`DELETED`) rejects invalid transitions;
  `ERROR → PROVISIONING` lets a job retry re-enter the saga; delete is a
  soft two-hop `→ DELETING → DELETED`.
- `POST /api/v1/instances/{id}/stop`, `POST /api/v1/instances/{id}/start`
  — synchronous (same precedent as delete: transition DB state, then call
  the agent gRPC directly in the same request, not a queued job). Stop:
  `RUNNING → STOPPING`, call `StopVM`, then `→ STOPPED` on success or
  `→ ERROR` on agent failure (both legal transitions). Start: call
  `StartVM` *first* while still `STOPPED`, only transitioning
  `STOPPED → RUNNING` if that succeeds — the state machine has no
  `STOPPED → ERROR` path, so a failed start leaves the instance untouched
  rather than stuck.
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
  set `ip_address` → create VM → start VM → transition to `RUNNING`.
  Every step is real as of Phase 13 — resource reservation, a `ROOT`
  disk (real sparse file), an IP allocation, and VM create/start are all
  genuine calls, not simulated work. VM/disk ops are real gRPC calls
  (TLS-secured) to the target node's `nebula-agent` (see below) — the
  agent's own VM hypervisor backend can still be a mock or real
  libvirt/KVM depending on build (Phase 11), but the network hop and the
  disk file are real either way.
- Any step's failure compensates in reverse order — exactly the spec's
  worked example: create VM fails → delete network → delete disk →
  release reservation — then transitions the instance to `ERROR`. A job
  retry re-invokes the saga from scratch, re-scheduling fresh (possibly
  onto a different node) rather than assuming the prior choice still
  holds.
- **Crash recovery**: this system has no separate worker binary — job
  dispatch runs in-process inside `nebula-api`, so a hard kill of
  `nebula-api` mid-saga (not a graceful shutdown) can leave an instance
  sitting at `PROVISIONING` with whatever it reserved before dying never
  released (the crash skips the normal compensation code entirely).
  `job.Pool`'s existing orphaned-job requeue (Phase 6) hands the
  interrupted job back to a worker on restart — `Saga.Provision` detects
  `PROVISIONING` at its own entry as the reliable "a prior attempt for
  this instance crashed" signal (unreachable any other way: one job is
  claimed by one worker at a time, and a normal completed attempt always
  ends at `RUNNING` or `ERROR` first), best-effort releases whatever that
  attempt left behind, then falls through to the same `ERROR →
  PROVISIONING` retry path a normal failure already uses — self-healing
  back to `RUNNING`, or cleanly to `ERROR` if the retry itself fails,
  rather than stuck forever.

**Networking & IPAM** (`internal/network/`)
- `Network`/`Subnet` follow AWS VPC/Subnet's 1:many shape (spec section
  31's example bundles a network with one subnet + gateway).
  `POST /api/v1/networks` (`SUPER_ADMIN`-gated, like nodes) creates both
  in one call — `{name, cidr, gateway}` — and pre-populates the subnet's
  full IP pool as individual rows (one per usable host address, excluding
  network/broadcast/gateway). A safety cap rejects subnets larger than
  `/16`. `GET /api/v1/networks`, `GET /api/v1/networks/{id}`.
- IPAM (spec section 32): allocate/release/reserve an IP, preventing
  duplicate allocation under concurrency via the same row-locking
  queue-claim pattern job dispatch already uses (`SELECT ... FOR UPDATE
  SKIP LOCKED` against `AVAILABLE` rows) — not a third concurrency
  pattern, the existing one reapplied to a new resource. Allocation is
  idempotent by instance ID, closing the "saga crashes after allocating,
  job retry re-runs from scratch" leak a naive always-allocate would
  have. The mandatory 100-concurrent-goroutine test
  (`internal/network/ip_allocation_concurrency_test.go`, gated on
  `NEBULA_DATABASE_URL` like the node/scheduler ones) proves every
  concurrent allocation gets a unique address.
- The provisioning saga's `create network` step (above) calls this real
  IPAM instead of a mock, setting the instance's `ip_address`; delete
  releases it. No Linux bridge/veth/network-namespace device is created
  anywhere yet — spec section 33 frames that as "Eventually," and there's
  neither a safe way to test it in this environment (no `CAP_NET_ADMIN`)
  nor, yet, a real network interface on a libvirt domain to attach it to
  (Phase 11's domains are still deviceless). Deferred until both exist.

**Storage** (`internal/storage/`)
- `Disk{Type (ROOT/DATA/BACKUP), SizeGB, InstanceID, NodeID, FilePath}`
  (spec section 34) — a real sparse file (`os.Create` + `Truncate`, no
  disk space consumed until written) on the compute node's own
  filesystem, created/deleted/resized via three new agent gRPC RPCs
  (`CreateDisk`/`DeleteDisk`/`ResizeDisk`). A disk's `NodeID` is fixed at
  creation — attaching it to an instance only succeeds if that instance
  is scheduled on the *same* node (a local file can't jump hosts).
- Every instance gets a `ROOT` disk automatically, created by the
  provisioning saga right after node reservation, sized to the
  instance's requested `disk_gb` — this is Phase 8's `createDisk`/
  `deleteDisk` mock finally made real, the last of the saga's five steps
  to get one.
- Tenant-scoped (like instances, not `SUPER_ADMIN` like nodes/networks):
  `POST /api/v1/instances/{id}/disks` (`{type, size_gb}`, `DATA`/`BACKUP`
  only — `ROOT` is saga-automatic), `GET /api/v1/instances/{id}/disks`,
  `GET /api/v1/disks/{id}`, `POST /api/v1/disks/{id}/attach`
  (`{instance_id}`), `POST /api/v1/disks/{id}/detach` (rejected for
  `ROOT`), `POST /api/v1/disks/{id}/resize` (`{size_gb}`, grow-only),
  `DELETE /api/v1/disks/{id}` (only once detached).
- No real libvirt `<disk>` device is attached to the VM domain — this
  phase is the real file + real DB tracking + real saga integration, not
  device passthrough into the VM. Deferred for the same reason Phase 12
  deferred real Linux network devices: nothing yet boots from a disk, so
  there's no way to meaningfully test a real attachment.

**Nebula Agent** (`cmd/nebula-agent`, `internal/agent/`, `internal/agentpb/`)
- A separate binary that runs on each compute node (spec section 27) — the
  abstraction layer between the control plane and the machine, so
  `nebula-api` never executes anything directly on a node.
- On startup: registers itself with `nebula-api` over REST, retried with
  backoff (including through a `409` — since Phase 16, that's not
  necessarily permanent: the control plane reclaims a hostname once its
  existing node row goes `OFFLINE`, so a restarted agent racing that
  30s window just needs to keep trying, same as any other failure; the
  existing 1s→2s→4s→8s→16s backoff schedule already clears 30s by its
  6th attempt) and starts a heartbeat loop, posting real host metrics
  (CPU% from `/proc/stat` deltas, memory from
  `/proc/meminfo`, disk from `statfs`, load average from `/proc/loadavg`)
  every `NEBULA_AGENT_HEARTBEAT_INTERVAL` (default `5s`). This direction
  (agent → control plane) is unchanged since Phase 9.
- Runs a TLS-secured gRPC server (`NEBULA_AGENT_PORT`, default `7071`) —
  `internal/agentpb/` is generated (via `buf generate`, see `proto/
  nebula_agent.proto`) from spec section 28's `NebulaAgent` service:
  `GetNodeInfo`, `CreateVM`, `DeleteVM`, `StartVM`, `StopVM`,
  `GetVMStatus`, plus (Phase 13) `CreateDisk`/`DeleteDisk`/`ResizeDisk`
  for real sparse-file disk management (see Storage below). This is the
  control-plane → agent direction; Phase 9's
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

**Observability** (`internal/metrics/`, `internal/tracing/`)
- `GET /metrics` on `nebula-api` (unauthenticated, like `/health`/`/ready`
  — matches how Prometheus scraping normally works) exposes: request rate/
  latency (`nebula_api_requests_total`, `nebula_api_request_duration_seconds`,
  labeled by chi's matched route pattern, not the raw path, to avoid label
  cardinality blowing up on `{id}`-shaped URLs), scheduler decisions
  (`nebula_scheduler_decisions_total{strategy,outcome}`), job outcomes
  (`nebula_jobs_total`, `nebula_jobs_failed_total`), node resource usage
  (`nebula_node_cpu_usage`, `nebula_node_memory_usage`, set live from each
  heartbeat), instance counts by status (`nebula_instances_total`, a
  `prometheus.Collector` querying Postgres at scrape time, not incrementally
  bookkept), and the audit consumer's Kafka lag
  (`nebula_kafka_consumer_lag`).
- Distributed tracing via OpenTelemetry, exported over OTLP-HTTP straight
  to Jaeger (`internal/tracing.NewProvider`, called by both `nebula-api`
  and `nebula-agent` with their own service name) — one trace follows
  `POST /instances` end to end: the HTTP handler span, DB spans
  (`otelpgx`), the async job-worker/provisioning-saga spans, the gRPC
  call to `nebula-agent` (`otelgrpc`), and the agent's own
  hypervisor/disk-store spans. The hard part is the gap in the middle:
  job dispatch is Postgres-poll-based, not a direct call, so there's a
  real async gap between "HTTP request enqueues a job" and "a worker
  picks it up later, possibly seconds away." Bridged by capturing the
  W3C `traceparent` on the `jobs.trace_context` column at enqueue time
  (same transaction as the instance+job insert) and re-extracting it into
  the worker's `context.Context` when it claims the job — so the saga's
  spans land as children of the *original* request's trace, not a
  disconnected new one. The outbox → Kafka → audit-log pipeline gets its
  own separate two-hop trace (publish → consume, `traceparent` carried as
  a Kafka header) rather than chaining back to the request that caused
  the underlying domain event.
- Grafana dashboards (`deployments/grafana/`), provisioned (not clicked
  together by hand) via datasource + dashboard-provider YAML: API, nodes,
  instances, scheduler, jobs, Kafka, workers.

**CLI** (`cmd/nebula-cli`, binary name `nebula`)
- A thin client over `nebula-api`'s HTTP surface — no direct DB/Kafka/gRPC
  access, same boundary any external caller would have. Built by `make
  build` into `bin/nebula` alongside the other two binaries.
- `nebula login <email> <password>` persists an access/refresh token pair
  to `~/.nebula/credentials.json` (mode `0600`). Every other command loads
  it, sends `Authorization: Bearer <access_token>`, and on a `401`
  (access tokens live 15 minutes) transparently refreshes once via the
  stored refresh token, persists the new pair, and retries — a CLI
  session outlives one access token without a re-`login`.
- Commands (spec section 49's exact list): `tenant list`; `node
  list`/`get`/`drain`; `instance create --name --cpu --memory --disk
  --image`/`list`/`get`/`start`/`stop`/`delete`; `network
  list`/`create <name> --cidr --gateway`; `job list [--status]`/`retry`.
  `list`/`get` output as tables/key-value pairs (stdlib `text/tabwriter`,
  no new dependency); API errors print as `CODE: message` with a non-zero
  exit rather than a raw status code or a stack trace.
- Base URL from `NEBULA_API_URL` (default `http://localhost:8080`) — no
  other config surface, no TLS concern here (that's the control-plane→
  agent hop, unchanged).

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

Also brought up by `make compose-up`: Prometheus (`localhost:9090`),
Jaeger UI (`localhost:16686`), Grafana (`localhost:3000`, `admin`/`admin`,
7 dashboards auto-provisioned under the "NEBULA" folder).

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
