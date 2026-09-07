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

**Next: Phase 9 — Nebula Agent** (spec section 63/line 2675).

**Known issue (found during Phase 8 verification, not Phase 8's own bug):**
the audit Kafka consumer (`internal/audit`, reader wired in
`cmd/nebula-api/main.go`) can hit `"Unable to establish connection to
consumer group coordinator... Group Coordinator Not Available"` on a fresh
`compose up` and then consume **zero** messages indefinitely — unlike the
job pool/outbox publisher/node monitor, it doesn't appear to self-retry
the coordinator connection. Reproduced once (~8 min stall, unstuck only
after an external consumer forced a rebalance); not yet root-caused or
fixed. Worth a clean re-test (`compose up` -> `migrate up` -> wait a
minute or two with zero manual Kafka CLI interference) before deciding
whether it's a slow-retry or a fully wedged reader.

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
- Concurrency: optimistic (version column + jittered retry) for resource
  reservation (`node.Reserve`/`Release`); row-locking
  (`SELECT ... FOR UPDATE SKIP LOCKED`) for queue-claiming (job dispatch).
  Different tool for a different concurrency problem — don't default to
  one pattern everywhere; the mandatory concurrency test (Phase 5) is what
  caught the first version of the retry budget being wrong, so don't skip
  writing one when adding a new contended operation.
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
