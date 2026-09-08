# Benchmarks

Phase 17 (spec section 71): benchmark the six targets spec section 47
names — API, scheduler, resource reservation, IPAM, Kafka, workers — and
use `pprof` to investigate whatever the numbers point at. Per section
48's own framing, the goal here isn't "Go is fast" — it's showing where
NEBULA actually spends its time, with real measurements, and fixing what
that turns up when a fix is safe and clearly justified.

**These numbers are from one run on a shared, non-isolated development
sandbox (a laptop, other things running) — not a stable, reproducible
production benchmarking environment.** Treat them as illustrative and
relative (which strategy costs more than another, what a fix changed),
not as absolute numbers to design capacity plans around. Nothing here is
wired into CI as a regression baseline.

## Running them

```
go test -bench=. -benchmem ./...
```

Scheduler and API benchmarks need nothing else — pure in-memory, same as
their unit tests. Resource reservation, IPAM, and workers need real
Postgres (`NEBULA_DATABASE_URL`); Kafka's two need a real broker
(`NEBULA_KAFKA_BROKERS`, e.g. `localhost:9092`). Every gated benchmark
`b.Skip`s cleanly with a clear message when its env var isn't set — a
bare `go test -bench=. ./...` with no Docker running still completes.

```
make compose-up && make migrate-up
NEBULA_DATABASE_URL="postgres://nebula:nebula@localhost:5432/nebula?sslmode=disable" \
NEBULA_KAFKA_BROKERS="localhost:9092" \
go test -bench=. -benchmem ./...
```

The two Kafka benchmarks specifically need to run from inside the
compose network (Kafka's advertised listener is `kafka:9092`, which only
resolves inside Docker) — from the host, run them via a throwaway
container attached to it instead:

```
docker run --rm --network compose_default -v "$(pwd)":/src -w /src \
  -e NEBULA_KAFKA_BROKERS=kafka:9092 golang:1.26 \
  go test -bench=. -benchmem ./internal/outbox/... ./internal/audit/...
```

## Results (this run)

### Scheduler (`internal/scheduler`) — 1000 synthetic nodes, varied capacity

| Strategy | ns/op | before fix | allocs/op | before fix |
|---|---|---|---|---|
| FirstFit | 73,023 | 124,780 | 1 | 11 |
| BestFit | 113,094 | 115,120 | 1 | 11 |
| LeastLoaded | 117,581 | 106,486 | 1 | 11 |
| Weighted | 108,874 | 115,587 | 1 | 11 |

**Finding + fix.** A CPU profile (`go tool pprof -top`) on the
4-strategy run showed `eligibleNodes` (`internal/scheduler/models.go`,
shared by all four strategies) accounting for **100% of allocated
memory** (309GB cumulative across a 200,000-iteration run) and over a
third of CPU time — not from its own filtering logic, but from the GC
pressure that volume of allocation causes downstream (`scanObjectsSmall`,
`memclrNoHeapPointers`, `memmove`, `scanObject` filled out the rest of
the top-15). Cause: `var eligible []node.Node` starting nil and growing
via `append` forces repeated reallocate-and-copy cycles as it doubles,
each copying an increasing number of (fairly large) `node.Node` values.
Fix: pre-size it to the worst case, `make([]node.Node, 0, len(all))` —
same idiom this codebase already uses in `pkg/api`'s list handlers. Net
effect: allocations per call dropped from 11 to 1 (~49% less memory),
and FirstFit (which benefits most, since it returns after the first
match rather than doing further per-node scoring work) got ~41% faster.
This is the one concrete, safely-justified fix this phase made — see
`internal/scheduler/models.go`'s comment for the full reasoning.

### API (`pkg/api`) — httptest against fakes, no DB

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| ListInstances | 31,456 | 23,228 | 136 |
| CreateInstance | 25,041 | 15,631 | 144 |

Full HTTP round trip (chi router, middleware, JSON encode/decode)
against a fake service — isolates the API layer's own overhead from
network/DB latency. Allocation count here is dominated by
`net/http/httptest`'s request/response machinery and JSON
marshal/unmarshal, not application logic — nothing surprising, no fix
warranted.

### Resource reservation (`internal/node`) — real Postgres

| Benchmark | ns/op | allocs/op |
|---|---|---|
| ReserveRelease (sequential) | 1,558,674 | 133 |
| ReserveReleaseParallel (16-way, same node) | 11,035,473 | 1186 |

A CPU profile on the sequential benchmark confirmed this is exactly what
it should be — I/O-bound, not CPU-bound: `Syscall6` (network I/O) and
context-cancellation machinery around each `pgx` `Query`/`QueryRow` call
account for the large majority of the profile, not application code.
Time here is genuinely "waiting on Postgres," which is the honest,
expected answer for a real compare-and-swap round trip.

The parallel variant — 16 goroutines continuously racing the *same*
node's version column for the whole benchmark run (a harsher, sustained
version of the mandatory 100-goroutine burst in
`reserve_concurrency_test.go`) — measured **`0.15
exhausted-cas-budget/op`**: under this level of sustained single-row
contention, `Reserve`/`Release`'s own internal 50-attempt CAS retry
budget (`internal/node/service.go`'s `maxReservationRetries`) got
exhausted and had to be retried at the benchmark's own outer level in
about 15% of operations. This is a legitimate outcome (`ErrReservationConflict`
is a real, already-handled error, not a crash) under a synthetic,
continuous full-throttle load pattern real traffic is very unlikely to
sustain — noted here as an honest finding, not changed, since there's no
evidence a real workload creates this level of sustained single-row
contention (see section 48: measure, don't optimize blindly).

### IPAM (`internal/network`) — real Postgres

| Benchmark | ns/op | allocs/op |
|---|---|---|
| AllocateReleaseForInstance | 1,410,439 | 43 |

Same shape and cost profile as resource reservation (real
`SELECT ... FOR UPDATE SKIP LOCKED` round trip) — consistent with both
being real-Postgres row-claim operations of similar complexity.

### Workers (`internal/job`) — real Postgres

| Benchmark | ns/op | allocs/op |
|---|---|---|
| JobCycle (create + claim + mark success) | 1,965,690 | 122 |

Three sequential real-Postgres round trips per iteration (insert, the
`SELECT ... FOR UPDATE SKIP LOCKED` claim, an update) — the ~2ms total
tracks with three independent DB round trips at this reservation/IPAM
benchmark's own per-round-trip cost.

### Kafka (`internal/outbox`, `internal/audit`) — real broker

| Benchmark | ns/op | before fix |
|---|---|---|
| Publish (one message per `WriteMessages` call) | 10,417,391 | ~1,001,317,034 |

**Finding + fix.** `kafka.Writer`'s `BatchTimeout` defaults to **1
second** when unset — confirmed directly in `segmentio/kafka-go`'s
source (`writer.go`'s `batchTimeout()`). `outbox.Publisher` (spec
section 23) calls `WriteMessages` once per event, one at a time, in a
loop — exactly the shape that pays the full default timeout on every
call when there's nothing else to batch with. `cmd/nebula-api/main.go`'s
production `kafkaWriter` had never overridden this. Since the publisher
already does its *own* batching one level up (ticks once a second,
gathers up to `PublisherBatchSize` unpublished events, then writes them),
`kafka.Writer`'s redundant 1-second timer was pure added latency with no
benefit. Fix: set `BatchTimeout: 10 * time.Millisecond` explicitly on
the production writer. Effect: **~1.0s/op → ~10.4ms/op, roughly a 96x
improvement** — a genuinely large, concrete, low-risk win this
benchmarking pass found and fixed, not a blind guess.

**Consume**: not usefully benchmarked as a steady-state per-message
number in this run. Every invocation creates a brand-new topic and
consumer group (to avoid touching production's real ones), which lands
squarely in the **cold-start** scenario `CLAUDE.md` already documents as
slow and still not root-caused (Phase 16's chaos testing found *restarts*
of an already-initialized broker recover in ~20-35s, but first-ever
topic/group creation is a different, slower path). This run measured
**~9-12 seconds** just to create the topic, join the consumer group, and
read the very first message — consistent with, and adding independent
confirmation to, that open issue. A forced fixed-iteration run
(`-benchtime=50x`, warmup read moved outside the timer) got a steady-
state number of **~180ms/op** for subsequent reads once already joined —
plausible real Fetch-request round-trip cost in this containerized
sandbox for a naive one-message-at-a-time read pattern, not an obvious
misconfiguration the way the Writer's default was, so left as-measured
rather than tuned further.

## What this didn't touch

No CI benchmark-regression tracking (no `benchstat`, no stored historical
baselines) — spec asks for benchmarks and profiling that explain current
behavior, not a perf-regression pipeline; that's speculative
infrastructure beyond what was asked for. No further Kafka Reader tuning
beyond documenting the honest number, and no attempt to root-cause the
cold-start topic/group creation slowness in this pass — that's real,
open, pre-existing work (`CLAUDE.md`'s Kafka gotcha), not something to
half-fix as a side effect of a benchmarking phase.
