# NEBULA — project instructions

Miniature cloud infrastructure platform in Go (mini AWS EC2 / OpenStack /
K8s control-plane / Nomad / Proxmox). Full spec: `docs/nebula.pdf`.

**Read `docs/nebula.txt` instead of the PDF.** It's a `pdftotext -layout`
extraction of the same spec, plain text, ~48KB, line-number-greppable.
Reading the PDF costs one image per page (78 images) and floods context;
`grep`/`sed -n 'A,Bp'` on the .txt file costs almost nothing. Only fall
back to the PDF for a diagram/ASCII-art figure that didn't extract
cleanly. The `nebula-spec-reader` agent (below) already does this — prefer
it over reading the file directly when you just need an answer, not the
full section.

## Spec section index (line numbers in `docs/nebula.txt`)

```
38    1. Core Architecture              1301  25. Dead Letter Queue
125   2. Technology Stack               1323  26. Saga / Compensation
167   3. Repository Structure           1405  27. Nebula Agent
302   4. Domain Model                   1445  28. Agent Communication
347   5. PostgreSQL Database            1511  29. KVM / libvirt
385   6. Tenant System                  1562  30. Libvirt Abstraction
416   7. Authentication                 1591  31. Networking
461   8. RBAC                           1634  32. IPAM
516   9. Compute Nodes                  1666  33. Linux Networking
563   10. Node Registration             1704  34. Storage
594   11. Heartbeat                     1749  35. Monitoring
655   12. Go Concurrency                1794  36. Distributed Tracing
691   13. Instance Lifecycle            1837  37. Rate Limiting
748   14. Instance API                  1864  38. Audit Logging
795   15. Idempotency                   1921  39. API Error Model
846   16. Resource Reservation          1946  40. Graceful Shutdown
891   17. Database Concurrency          1997  41. Configuration
934   18. Scheduler                     2038  42. Docker Compose
997   19. Scheduler Concurrency Test    2067  43. Testing Strategy
1026  20. Job System                   2091  44. Integration Tests
1089  21. Worker Pool                  2130  45. Concurrency Tests
1138  22. Kafka                        2151  46. Failure Testing
1191  23. Transactional Outbox         2183  47. Benchmarking
1270  24. Retry System                 2223  48. Go Performance Work

2268  49. CLI                          2765  67. Phase 13 — Storage
2332  50. Security Requirements        2785  68. Phase 14 — Observability
2365  51. Agent Security               2817  69. Phase 15 — CLI
2403  52. API Versioning               2825  70. Phase 16 — Failure Recovery
2423  53. Documentation                2844  71. Phase 17 — Performance
2470  54. Architecture Decision Records 2875 72. Phase 18 — Production Hardening
2495  55. Development Phases           2903  73. Future Microservice Extraction
2499  Phase 1 — Project Foundation     2973  74. Kubernetes — Final Stage
2529  56. Phase 2 — Authentication     3004  75. Final Demo Scenario
2556  57. Phase 3 — Compute Nodes      3179  76. Definition of Done
2572  58. Phase 4 — Instances          3222  77. What This Project Should Teach You
2592  59. Phase 5 — Scheduler          3309  78. The Most Important Rule
2614  60. Phase 6 — Jobs
2634  61. Phase 7 — Kafka
2654  62. Phase 8 — Provisioning Saga
2675  63. Phase 9 — Nebula Agent
2699  64. Phase 10 — gRPC
2716  65. Phase 11 — KVM/libvirt
2742  66. Phase 12 — Networking
```

## Progress (update this after every phase's commit)

| Phase | What | Commit |
|---|---|---|
| 1 | Foundation (HTTP, config, Postgres, migrations, health) | `c528693` |
| 2 | Auth/RBAC (JWT, refresh tokens, register/login) | `1c41418` |
| 3 | Compute nodes (register, heartbeat, monitor) | `5b82c72` |
| 4 | Instances (CRUD, state machine, tenant isolation) | `b1769d1` |
| 5 | Scheduler + resource reservation (4 strategies, optimistic concurrency) | `70e2e3d` |
| 6 | Jobs (worker pool, retry/backoff, DLQ) | `4ccf7f5` |
| 7 | Kafka (transactional outbox, audit consumer) | `37860f4` |
| 8 | Provisioning saga (real scheduling/reservation, compensation) | `5f662c7` |
| 9 | Nebula Agent (registration, heartbeat, real metrics, mock VM ops over real HTTP) | `0040289` |
| 10 | gRPC + TLS (protobuf via buf, control plane -> agent over gRPC, server-authenticated TLS) | `988674f` |
| 11 | KVM/libvirt (Hypervisor interface, MockHypervisor, real LibvirtHypervisor gated behind -tags libvirt) | `2be9a7e` |
| 12 | Networking (network/subnet/IPAM, real saga integration, instance ip_address) | `af37aca` |
| 13 | Storage (real sparse-file disks, agent gRPC disk RPCs, saga integration, attach/detach/resize API) | `d45209a` |
| 14 | Observability (Prometheus, OpenTelemetry/Jaeger, Grafana dashboards) | `4a2c443` |

**Next: Phase 15 — CLI** (spec section 69/line 2817). Linux bridge/veth/
network-namespace device management (spec section 33) and real libvirt
`<disk>`/`<interface>` device attachment both stay deferred — see the
Phase 12/13 plans' own scope-boundary notes for why (no `CAP_NET_ADMIN`
in this sandbox for the former; no bootable OS/image pipeline yet to make
either meaningfully testable).

**This sandbox has real libvirtd/qemu-kvm** (`libvirt-dev` installed
Phase 11) — `LibvirtHypervisor` isn't theoretical, it's proven against
real KVM domains via `make test-libvirt`. Compose still defaults to
`MockHypervisor` (`NEBULA_AGENT_HYPERVISOR=mock`) — see Phase 11's own
scope-boundary note in its commit message for why (no `/dev/kvm`
passthrough into the container this phase).

**Known issue, now reproduced across Phases 8/9/10 (still not root-caused,
not any one phase's own bug):** the audit Kafka consumer (`internal/
audit`, reader wired in `cmd/nebula-api/main.go`) can hit `"Unable to
establish connection to consumer group coordinator... Group Coordinator
Not Available"` on a fresh `compose up` and then consume **zero** messages
for several minutes — unlike the job pool/outbox publisher/node monitor,
it doesn't appear to self-retry the coordinator connection promptly. The
Phase 10 verification run reproduced a ~5 minute stall with *zero* manual
Kafka CLI interference (ruling out the "manual probing caused it" theory
from the Phase 9 notes) — outbox events were published fine the whole
time, `audit_logs` just stayed empty until the consumer eventually caught
up. This looks like a real, if slow-to-manifest, bug in `internal/audit`'s
consumer setup (likely something in its `kafka.ReaderConfig` around
retry/backoff on the initial coordinator lookup) and deserves a dedicated
fix pass — next time a phase touches `internal/audit`/`internal/outbox`,
or as a standalone fix, don't just re-verify around it again.

## Workflow for a new phase

1. Look up the phase's spec section (index above) — use the
   `nebula-spec-reader` agent, or `sed -n 'START,ENDp' docs/nebula.txt`
   directly for a quick check.
2. `EnterPlanMode`. The plan's Context section must explain *why*, not
   just *what* — cite the prior-phase commits this builds on, and any
   scope boundary being drawn (what's deliberately deferred, and to which
   later phase/mechanism). This project has a strong pattern of exactly
   that: idempotency deferred past Phase 4, Testcontainers skipped in
   favor of an env-gated real-DB test in Phase 5, scheduler left unwired
   in Phase 6 pending Phase 8's compensation logic. Keep doing this —
   don't quietly build something a later phase will have to tear out.
3. Use `AskUserQuestion` only for a genuine fork with real tradeoffs (e.g.
   "SUPER_ADMIN-gated node registration vs. open bootstrap", "reuse
   compose Postgres for the concurrency test vs. add Testcontainers").
   Don't ask when spec + established precedent already gives one clearly
   correct reading.
4. Migration(s): next sequential `NNNNNN_name.up.sql` / `.down.sql` in
   `migrations/`.
5. Implement following the package pattern below. `gofmt -w .`, `go build
   ./...`, `go vet ./...`, `go test -race ./...` (standalone, no DB/Kafka
   needed — see Testing below).
6. Verify against the real stack — use the `nebula-verifier` agent to keep
   the compose/curl/log output out of the main conversation's context,
   unless the walkthrough is trivial enough to just run inline.
7. Update `README.md` — **group by feature, never by phase number**. The
   user explicitly asked for this; phase framing is an internal
   build-order detail, not something a reader of the repo needs.
8. Update the Progress table above with the new commit hash.
9. `git add` specific paths (never a blind `-A` — check `git status` for
   anything unexpected first), commit explaining *why*, push directly to
   `main` (no PR flow used on this project so far).

## Architecture conventions (established — don't re-litigate these)

- Module `github.com/milosursulovic/nebula`, Go 1.25+.
- Monorepo layout follows spec section 3's tree: `cmd/nebula-api/`,
  `internal/<domain>/`, `pkg/api/`, `migrations/`, `deployments/`.
- Every `internal/<domain>` package: `models.go`, `errors.go`,
  `repository.go` (`Repository` interface + `pgxRepository`), `service.go`
  (`Service` interface + impl), `fake_repository_test.go` (in-memory,
  mutex-guarded fake), plus `*_test.go` per concern. Look at
  `internal/node/` or `internal/instance/` as the reference shape before
  starting a new package.
- Domain packages don't import each other by default. The two exceptions
  so far are both deliberate and justified in their own commit:
  `internal/instance` imports `internal/outbox` (an instance state change
  *is* an outbox event, in the same transaction), `internal/audit` imports
  `internal/outbox` (shared event-type constants). Reach for
  `pkg/api` coordinating multiple `Service`s from the HTTP layer first;
  only add a direct domain-to-domain import when the relationship is this
  tight.
- `pkg/api` owns HTTP transport: chi router, `X_handlers.go` per domain,
  `fake_X_service_test.go` doubles, shared `errors.go` (`writeError`
  matching spec section 39's `{timestamp, status, code, message,
  trace_id}` shape).
- Tenant isolation: `tenant_id` always comes from the JWT identity
  (`auth.IdentityFromContext`), never a client-supplied value. Cross-tenant
  access returns `404`, not `403` (no existence leak).
- RBAC: `SUPER_ADMIN` gates platform infrastructure (nodes, jobs).
  Tenant-owned resources (instances) just require authentication, no role
  gate — both `TENANT_ADMIN` and `USER` can act on their own tenant's
  resources per spec's RBAC table (section 8/line 461).
- Machine-to-machine bootstrap (an unattended process with no user
  credentials, e.g. `nebula-agent` self-registering): a pre-shared secret
  (`NEBULA_NODE_BOOTSTRAP_SECRET`, constant-time compared) as an
  alternative auth path alongside the existing human/JWT one, not a
  replacement — see `pkg/api/node_handlers.go`'s
  `requireNodeBootstrapOrSuperAdmin` (Phase 9). The spec deliberately
  leaves this unspecified before mTLS (Phase 10) lands; reach for this
  same pattern rather than re-deciding it if another unattended process
  needs to call into `nebula-api` before Phase 10.
- Concurrency: optimistic (version column + jittered retry) for resource
  reservation (`node.Reserve`/`Release`); row-locking
  (`SELECT ... FOR UPDATE SKIP LOCKED`) for queue-claiming — job dispatch,
  and (Phase 12) IPAM's `AllocateForInstance` claiming one `AVAILABLE`
  `ip_addresses` row the same way. Different tool for a different
  concurrency problem — don't default to one pattern everywhere; when a
  new contended operation is really "claim any one of many interchangeable
  rows," reach for the row-locking pattern rather than inventing a third
  one. The mandatory concurrency test (Phase 5, and now Phase 12's
  `internal/network/ip_allocation_concurrency_test.go`) is what proves it
  — don't skip writing one when adding a new contended operation.
- `internal/common/config.go`: only add an env var once something in that
  same phase actually reads it. Don't add config ahead of its consumer.
- `deployments/compose/docker-compose.yml` grows incrementally — one file,
  never replaced, each phase adds what it needs (Kafka arrived Phase 7).

## Testing conventions

- Unit tests run against `fakeRepository` (in-memory, same package) by
  default — this is sufficient for almost everything.
- Only add a real-Postgres-backed test when the fake genuinely can't prove
  the property under test (e.g. actual SQL-level CAS/locking behavior —
  see `internal/node/reserve_concurrency_test.go`). Gate it on
  `NEBULA_DATABASE_URL` with a clean `t.Skip` when unset, so `go test
  ./...` always passes standalone without Docker running.
- `go test -race ./...` is mandatory practice (spec sections 19/45), not
  optional — run it before considering a phase done.
- `gofmt -l .` / `gofmt -w .` before every build check; CI-equivalent
  local loop is `gofmt -w . && go build ./... && go vet ./... && go test
  -race ./...`.

## Docker Compose gotchas already solved (don't re-debug these)

- Single-node KRaft Kafka needs `KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1`
  (and the transaction-log equivalents) explicitly — the Kafka default of
  3 can never be satisfied with one broker and wedges internal topic
  creation forever.
- A fresh `kafka-go` consumer group needs `StartOffset: kafka.FirstOffset`
  explicitly, or it silently starts at the latest offset and misses every
  message published before it finished joining the group.
- `bitnami/kafka` image tags are gone (moved behind a paywall as of this
  writing) — use `apache/kafka:3.7.0+` (KRaft-native, no Zookeeper needed).
- Running `make compose-up` before `make migrate-up` produces a few
  seconds of expected transient "relation does not exist" errors in logs
  from the background goroutines (node monitor, outbox publisher) that
  started before the migration landed. Self-heals the moment the migration
  applies — not a bug, don't chase it.
- Since Phase 12, creating an instance on a fresh stack with **no network
  created yet** doesn't fail synchronously — `POST /instances` returns
  `201 PENDING` fine, but the saga then fails asynchronously at its
  `create network` step (`"allocate ip: no available ip address"`),
  retries the full backoff schedule, and lands the instance in `ERROR`.
  Always `POST /api/v1/networks` (as `SUPER_ADMIN`) before creating
  instances in a fresh walkthrough — not a bug, just easy to trip over
  since the symptom shows up several seconds later than the request that
  "caused" it.
- No `protoc`/`sudo apt-get` in this sandbox (no passwordless sudo) — use
  `buf` (`go install github.com/bufbuild/buf/cmd/buf@latest`, pure Go,
  ships its own compiler) plus `protoc-gen-go`/`protoc-gen-go-grpc`
  (also `go install`-able) for any `.proto` regeneration. `make proto-gen`
  wraps `buf generate`; generated `internal/agentpb/` is committed, not
  regenerated at build/CI time.
- `deployments/certs/nebula-agent.crt`/`.key` (Phase 10's dev-only TLS
  cert) must exist before `docker compose build` — both Dockerfiles
  `COPY` them in. If you ever need to regenerate it, both
  `nebula-agent`'s server and `nebula-api`'s client trust config
  (`NEBULA_AGENT_TLS_CA_FILE`) point at the same file — see
  `deployments/certs/README.md`.
- `LibvirtHypervisor` (`internal/agent/libvirt_hypervisor.go`, `//go:build
  libvirt`) needs `libvirt-dev` (CGO headers) — not part of the default
  toolchain, `sudo apt-get install libvirt-dev` needed once. `go mod tidy`
  (no tags) will strip `libvirt.org/go/libvirt` from `go.mod` since only a
  tagged file imports it — regenerate with `GOFLAGS=-tags=libvirt go mod
  tidy` instead (`go mod tidy` itself has no `-tags` flag). Docker images
  never build with this tag — the shipped `nebula-agent` always runs
  `MockHypervisor`.
- A libvirt domain's custom `<metadata>` element MUST be marshaled with
  its element name itself namespace-prefixed (`<nebula:info
  xmlns:nebula="...">`), not just a bare element with an `xmlns:` attribute
  sitting on it (`<info xmlns:nebula="...">` looks namespaced but isn't —
  libvirt silently drops/ignores it, `GetMetadata` returns nothing back).
  `internal/agent/libvirt_hypervisor.go`'s `nebulaInfo` struct uses the
  `xml:"nebula:info"` tag trick on both the `XMLName` field and the parent
  struct's field tag (they must match exactly or `encoding/xml` refuses
  to marshal) to get this right — caught by the real-libvirt gated test,
  not by any mock.
- Reading a Postgres `inet` column: `pgx` v5's inet codec only scans into
  `netip.Addr`/`netip.Prefix`, never `*string`, so `SELECT
  some_inet_column` into a Go `string` needs an explicit SQL-side
  conversion — but use `host(col)`, not `col::text`. The bare cast
  renders a single address as `"10.20.0.2/32"` (netmask included); `host()`
  strips it to `"10.20.0.2"`. Same asymmetry applies writing an `inet`
  column via `CopyFrom` (needs an actual `netip.Addr` value, since
  `CopyFrom` is binary-only and `string` has no inet binary-encode plan)
  vs. a plain parameterized `INSERT`/`UPDATE` (a Go `string` parameter
  with an explicit `$N::inet` cast works fine there). Verified/caught
  this exact `/32` bug via the real `nebula-verifier` walkthrough in
  Phase 12 — the fake-repository unit tests never touch real Postgres so
  they can't catch inet-rendering quirks like this.
- `jaegertracing/all-in-one` has no bare `:1.62`-style minor tag on Docker
  Hub — only fully-qualified patch tags (`1.62.0`, `1.63.0`, ...) plus
  `latest`. Compose pins `1.62.0`. If bumping, check the actual published
  tag list first, not just the minor version scheme other images use.
- Grafana's host port `3000` isn't reserved by anything in this repo —
  on this particular dev machine it collided with an unrelated
  `pingvin-share-x` container already bound to `3000` (Phase 14
  verification hit this: the `grafana` container stayed in `Created`,
  never started, while a `curl localhost:3000` silently answered from
  the *other* container instead — caught only by noticing the response
  headers didn't say Grafana). Not a bug in this repo; if it recurs, free
  the port or remap Grafana's host-side port in
  `deployments/compose/docker-compose.yml`.

## Verification checklist per phase

(This is exactly what the `nebula-verifier` agent automates.)

1. `go build ./...`, `go vet ./...`, `go test -race ./...` — standalone.
2. `make compose-up` (rebuilds the image), `make migrate-up`.
3. A curl walkthrough proving the phase's new behavior end-to-end — not
   just unit tests. Include the negative cases (wrong role → 403, wrong
   tenant → 404, invalid state transition → 409, etc.) alongside the happy
   path.
4. Check container logs for unexpected errors (`docker compose logs
   nebula-api --no-log-prefix`).
5. Graceful shutdown: `docker compose stop nebula-api`, confirm a clean
   `"nebula-api stopped cleanly"` log line with no hang, then `docker
   compose down`.
