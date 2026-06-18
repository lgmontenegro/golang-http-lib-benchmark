# HTTP Dependency Inversion Benchmark

[English](#english) · [Português](#português)

---

## English

A proof of concept demonstrating dependency inversion in Go to swap between
five HTTP frameworks (stdlib, Gin, Fiber, Echo, chi) and compare their
performance under steady load with vegeta.

The front service can resolve the transaction aggregate through several
driven adapters selected by the `-repo` flag — each a **(transport,
serialization)** pair so the benchmark isolates protocol from wire format:

| `-repo`       | transport     | serialization | dataservice port |
|---------------|---------------|---------------|------------------|
| `mysql` *(def)* | in-process  | —             | —                |
| `rest`        | HTTP/1.1      | JSON          | `:9090`          |
| `resth2`      | HTTP/2 (h2c)  | JSON          | `:9092`          |
| `resth2-pb`   | HTTP/2 (h2c)  | protobuf      | `:9092`          |
| `resth2-avro` | HTTP/2 (h2c)  | Avro          | `:9092`          |
| `grpc`        | gRPC          | protobuf      | `:9091`          |
| `grpc-avro`   | gRPC          | Avro          | `:9091`          |
| `grpc-flat`   | gRPC          | FlatBuffers   | `:9091`          |

This makes the comparisons answerable cleanly, each holding everything else
fixed: **HTTP/1.1 vs HTTP/2** (`rest` vs `resth2`), **HTTP/2 vs gRPC** with
serialization held to protobuf (`resth2-pb` vs `grpc`), and **protobuf vs
Avro vs FlatBuffers** on gRPC (`grpc` vs `grpc-avro` vs `grpc-flat`).

There are two endpoints behind every backend: the single aggregate
`/v1/transaction/:id` (~311 B) and the list `/v1/transactions/:count`
returning N aggregates — the latter exists to drive the wire codecs under
larger payloads, since serialization is rounding error at 311 B and only
separates as N grows.

The dataservice owns the MySQL connection and exposes all three transports
from the same binary (REST/HTTP1 `:9090`, gRPC `:9091`, REST/h2c `:9092`),
so the front picks the pair at startup. The REST endpoints content-negotiate
JSON/protobuf/Avro via the `Accept` header; gRPC serves protobuf, Avro, and
FlatBuffers on the same listener via content-subtype. Same shape, same
`app.TransactionRepository` interface (`GetByID` + `GetBatch`) — only the
adapter differs.

### Table of contents

- [Requirements](#requirements)
- [Project structure](#project-structure)
- [Initialize the module](#initialize-the-module)
- [Bring up MySQL](#bring-up-mysql)
- [Run the server manually](#run-the-server-manually)
- [Run the test suite](#run-the-test-suite)
- [Run the benchmark](#run-the-benchmark)
- [Analyze results](#analyze-results)
- [Relevant metrics](#relevant-metrics)
- [Sample results](#sample-results)
- [Notes](#notes)

### Requirements

- Go 1.26+
- Docker + Docker Compose (for MySQL)
- curl (for the script's healthcheck poll)
- vegeta

```bash
go install github.com/tsenart/vegeta@latest
```

### Project structure

```
go-http-di/
├── main.go                       # Front service: composition root, -engine + -repo
├── cmd/
│   └── dataservice/              # Data service: owns MySQL, exposes REST/HTTP1 + REST/h2c + gRPC
│       ├── main.go
│       ├── handler.go            # REST handler (content-negotiated JSON/protobuf/Avro)
│       ├── handler_test.go
│       ├── grpc.go               # gRPC TransactionService impl (protobuf, GetById + GetBatch)
│       ├── grpc_avro.go          # gRPC TransactionServiceAvro impl (manual ServiceDesc)
│       └── grpc_flat.go          # gRPC TransactionServiceFlat impl (FlatBuffers, manual ServiceDesc)
├── proto/
│   ├── transactions.proto        # protobuf gRPC wire schema (GetById + GetBatch)
│   ├── transactionspb/           # generated protobuf stubs (committed)
│   ├── transactions.fbs          # FlatBuffers wire schema
│   └── transactionsfb/           # generated FlatBuffers code (committed)
├── serde/                        # Serialization codecs (JSON/protobuf/Avro/FlatBuffers)
│   ├── serde.go                  # Codec port (single + list) + ForAccept factory
│   ├── json.go / protobuf.go / avro.go
│   └── flatbuffers.go            # FlatBuffers builders/readers + grpc codec (zero-copy)
├── bench.sh                      # Engine comparison (5 engines × 2 modes, mysql repo only)
├── bench-full.sh                 # Full matrix (5 engines × 8 backends × 2 modes, GetById)
├── bench-payload.sh              # Payload matrix (fiber × 8 backends × 4 sizes, GetBatch)
├── docker-compose.yml            # MySQL 8.0 with bind mount
├── app/
│   ├── app.go                    # Core: App struct + handlers
│   └── transactions.go           # Driven port: Transaction + TransactionRepository + sentinel
├── server/
│   ├── server.go                 # Driving port: Server interface + Request/Response types
│   ├── stdlib/stdlib.go          # net/http adapter
│   ├── ginadapt/gin.go           # Gin adapter
│   ├── fiberadapt/fiber.go       # Fiber adapter
│   ├── echoadapt/echo.go         # Echo adapter
│   └── chiadapt/chi.go           # chi adapter
├── db/
│   ├── mysql/mysql.go            # MySQL (sqlx) adapter for TransactionRepository
│   ├── restclient/restclient.go  # REST client adapter (HTTP/1.1 or h2c; codec-pluggable)
│   ├── grpcclient/grpcclient.go  # gRPC client adapter (protobuf)
│   ├── grpcavroclient/grpcavroclient.go # gRPC client adapter (Avro codec)
│   └── grpcflatclient/grpcflatclient.go # gRPC client adapter (FlatBuffers codec)
├── mysql-init/
│   ├── 01-schema.sql             # DDL for the 3 tables; runs only on first boot
│   └── 02-seed.sql               # Bulk seed via recursive CTE (50k/150k/150k)
├── mysql-data/                   # MySQL bind volume (gitignored)
└── bench-results/                # Generated after the benchmark (gitignored)
    ├── <engine>-<mode>.bin / .txt / .html
    ├── targets-cycled.txt
    ├── summary.txt
    └── combined.html
```

### Initialize the module

```bash
go mod init example.com/httpdi
go mod tidy
```

### Bring up MySQL

```bash
docker compose up -d              # MySQL 8.0 on :3306, data in ./mysql-data
docker compose logs -f mysql      # follow first boot (~30-60s due to the seed)
docker compose down               # stop (preserves data)
rm -rf mysql-data                 # full wipe + re-apply init scripts on next up
```

Default credentials (change before any serious use): user `httpdi`, password
`httpdipass`, database `httpdi`. Initialization is split across two files that
run once when `mysql-data/` is empty:

- [`mysql-init/01-schema.sql`](mysql-init/01-schema.sql) — DDL for three tables:
  - `customer (id, nome, create_date)`.
  - `` `transaction` (id, value, customer_id FK → customer, create_date) `` —
    backticked because `TRANSACTION` is a MySQL reserved word.
  - `cart_snapshot (id, transaction_id FK → transaction UNIQUE, create_date)` —
    **1:1** with `transaction`.
- [`mysql-init/02-seed.sql`](mysql-init/02-seed.sql) — bulk seed via recursive
  CTE:
  - **50,000** customers (`Customer #1` .. `#50000`).
  - **150,000** transactions (3 per customer; random value 0–1000).
  - **150,000** cart_snapshots (1:1 with transactions).

Deterministic IDs: customer N is `11111111-…-LPAD(N,12,'0')`, transaction N is
`00000000-…-LPAD(N,12,'0')`, cart_snapshot N is `22222222-…-LPAD(N,12,'0')`.
Transaction N belongs to customer `FLOOR((N-1)/3)+1` — so transactions 1, 2,
and 3 all belong to customer 1. The ID `…001` in any of the three tables
points to the same aggregate (this is what the benchmark and the integration
test hit).

The endpoint `/v1/transaction/:id` runs `transaction ⋈ customer ⋈
cart_snapshot` (INNER JOIN of all three) and returns a denormalised JSON
aggregate.

**First boot ~30–60s** because of the seed (`bench.sh`'s `wait_for_mysql`
waits up to 5 min). Subsequent boots skip initialization (~5s). To re-apply
schema/seed: `docker compose down && rm -rf mysql-data && docker compose up
-d`.

### Run the server manually

**In-process MySQL** (default: front talks to MySQL directly):

```bash
# stdlib (default), connects to MySQL on the default DSN
go run main.go

# Gin / Fiber / Echo / chi
go run main.go -engine gin
go run main.go -engine fiber
go run main.go -engine echo
go run main.go -engine chi

# Custom port + custom DSN (also reads the DB_DSN env var)
go run main.go -engine gin -addr :3000 -dsn "user:pass@tcp(host:3306)/db?parseTime=true"
```

**Microservice topology — REST or gRPC** (two terminals):

```bash
# Terminal A: data tier (owns MySQL, REST/HTTP1 :9090, gRPC :9091, REST/h2c :9092)
go run ./cmd/dataservice -rest-addr :9090 -grpc-addr :9091 -rest-h2c-addr :9092

# Terminal B: front picks the (transport, serialization) pair (any engine)
go run main.go -engine fiber -repo rest        -repo-addr http://localhost:9090   # HTTP/1.1 + JSON
go run main.go -engine fiber -repo resth2      -repo-addr http://localhost:9092   # HTTP/2  + JSON
go run main.go -engine fiber -repo resth2-pb   -repo-addr http://localhost:9092   # HTTP/2  + protobuf
go run main.go -engine fiber -repo resth2-avro -repo-addr http://localhost:9092   # HTTP/2  + Avro
go run main.go -engine fiber -repo grpc        -repo-addr localhost:9091          # gRPC    + protobuf
go run main.go -engine fiber -repo grpc-avro   -repo-addr localhost:9091          # gRPC    + Avro
go run main.go -engine fiber -repo grpc-flat   -repo-addr localhost:9091          # gRPC    + FlatBuffers
```

Regenerate the proto stubs **only when [proto/transactions.proto](proto/transactions.proto)
changes** (needs `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` — see
the Notes at the bottom):

```bash
PATH="$(go env GOPATH)/bin:$PATH" protoc \
  --go_out=. --go_opt=module=example.com/httpdi \
  --go-grpc_out=. --go-grpc_opt=module=example.com/httpdi \
  proto/transactions.proto
```

Smoke test (same regardless of `-repo`):

```bash
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo
curl http://localhost:8080/v1/transaction/00000000-0000-0000-0000-000000000001
```

### Run the test suite

```bash
go test ./...                          # unit + HTTP integration (~2.5s, no MySQL)
go test -race ./...                    # with race detector
go test -tags integration ./db/mysql   # MySQL integration (requires docker compose up)
go test -v ./server/stdlib             # verbose, one package
```

Coverage:

- **Unit tests** — `app` validates `Health`, `Hello` (including the `"world"`
  default), and `GetTransaction` (200/404/500 via `fakeTxRepo`);
  `server/stdlib` validates the `:name` → `{name}` translation.
- **HTTP integration** — a shared suite in `server/internal/servertest`
  registers routes, starts each adapter on dedicated ports
  (stdlib `:18081`, gin `:18082`, fiber `:18083`, echo `:18084`, chi
  `:18085`), makes real HTTP calls, and verifies `/health`,
  `/hello/:name`, and 404. Each adapter has a minimal `_test.go` that
  invokes the suite.
- **MySQL integration** — `db/mysql/mysql_test.go` is gated by the
  `integration` build tag (needs the container UP). Excluded from the default
  `go test ./...`.

### Run the benchmark

Three scripts answer three questions:

- **[`bench.sh`](bench.sh)** — *Q1: which framework is fastest?* Holds
  `-repo mysql` (in-process). Varies engine × mode (5 × 2 = 10 cells).
  ~5 min. Use this for quick iteration.
- **[`bench-full.sh`](bench-full.sh)** — *Q2: how does each framework
  behave under each (transport, serialization)?* Full matrix engine ×
  backend × mode on the single aggregate (5 × 8 × 2 = 80 cells). ~46 min.
  The 8 backends are `mysql`, `rest` (h1+json), `resth2`/`resth2-pb`/
  `resth2-avro` (h2c + json/pb/avro), `grpc` (pb), `grpc-avro`, `grpc-flat`.
  Output grouped per engine — 5 sub-tables of 16 rows — so you can read
  HTTP/1.1 vs HTTP/2 (`rest` vs `resth2`), HTTP/2 vs gRPC (`resth2-pb` vs
  `grpc`) and protobuf vs Avro vs FlatBuffers (`grpc` vs `grpc-avro` vs
  `grpc-flat`) directly. Writes to `bench-results-full/`. Trim the
  `BACKENDS`/`ENGINES` arrays if 80 cells is too long.
- **[`bench-payload.sh`](bench-payload.sh)** — *Q3: how do the wire formats
  behave as the payload grows?* The list endpoint `/v1/transactions/:count`,
  fiber only, backend × size: 8 backends × N{10,100,1000,5000} = 32 cells.
  ~8 min (default 500 req/s × 10s — lower because payloads reach ~1.5 MB).
  Writes to `bench-results-payload/`. This is where serialization is meant to
  separate; at ~311 B (bench-full) it's noise.

```bash
chmod +x bench.sh bench-full.sh

# Engine comparison (default: 5000 req/s for 30s per engine)
./bench.sh
./bench.sh 10000 60s            # custom rate + duration

# Full matrix (engine × backend × mode)
./bench-full.sh
./bench-full.sh 10000 60s
```

Both scripts run **two attack modes** (per engine in bench.sh, per
engine×backend in bench-full.sh), back-to-back without restarting the
server between modes:

- **`single`** — every request hits the same row (`…000000000001`). InnoDB
  pins it in the buffer pool → measures the hot path: framework + sqlx +
  driver + MySQL with zero cache variance.
- **`cycled`** — vegeta cycles through the 150,000 seeded IDs (file
  `bench-results/targets-cycled.txt`, generated at startup). Exercises the PK
  index + JOIN across the entire dataset. The working set fits the default
  128 MB buffer pool, so it remains mostly cache hits; the gap from `single`
  is mainly index-walk cost and JOIN planner work.

Each mode does its own 500 req/s × 3s warmup using the *same source* as the
attack, so the Go runtime and any per-target setup costs are paid before the
measurement.

Full flow:

1. Verifies `vegeta` and `docker` are on `$PATH`.
2. `docker compose up -d` and waits for MySQL to be `healthy` (up to 5 min on
   first boot because of the seed).
3. Builds the binary once (`go build`).
4. Generates the cycled targets file (150k lines, ~12 MB).
5. For each of the five engines: starts the server, waits for the
   healthcheck, runs both modes (single, cycled), stops the server.
6. Produces a text report + HTML plot per (engine, mode) — 10 reports
   total (5 × 2).
7. Prints the 10-row comparative table and writes `summary.txt` +
   `combined.html` (auto-fit grid of all plots).
8. `trap` on `EXIT` runs `docker compose down` even on error / Ctrl+C.

### Analyze results

The `.bin` files are vegeta's native format and allow re-analysis without
re-running the test:

```bash
# Re-generate text report
vegeta report < bench-results/stdlib-single.bin

# Latency histogram
vegeta report -type=hist < bench-results/gin-cycled.bin

# Export to JSON (jq, scripts, etc.)
vegeta report -type=json < bench-results/fiber-single.bin

# Generate a new plot
vegeta plot < bench-results/stdlib-cycled.bin > new-plot.html
```

The combined report (`bench-results/combined.html`) shows all ten plots
in the browser (5 engines × 2 modes), auto-fit grid.

### Relevant metrics

| Metric      | What it means                                                          |
|-------------|------------------------------------------------------------------------|
| Throughput  | Requests processed per second (should match or approach the target)    |
| Latency p50 | Median — half the requests came in below this value                    |
| Latency p95 | Tail — 5% of requests came in above this value                         |
| Latency p99 | Outliers — the slowest 1% of requests                                  |
| Success     | Percentage of responses with status 2xx/3xx                            |

If throughput stays below the target rate, the server is saturating at that
rate. In that case, compare the saturation point across the three engines.

### Sample results

#### Current run — `/v1/transaction/:id` with MySQL JOIN

macOS arm64, 5000 req/s × 30s, two modes per engine (run back-to-back):

| Engine  | Mode    | Throughput | p50         | p95         | p99           | Success |
|---------|---------|------------|-------------|-------------|---------------|---------|
| stdlib  | single  | 5000.0/s   | 421.4µs     | 513.5µs     | 723.3µs       | 100.00% |
| stdlib  | cycled  | 5000.0/s   | 420.4µs     | 516.4µs     | 704.2µs       | 100.00% |
| gin     | single  | 5000.0/s   | 423.1µs     | 514.8µs     | 701.8µs       | 100.00% |
| gin     | cycled  | 5000.0/s   | 409.6µs     | 498.8µs     | 691.5µs       | 100.00% |
| fiber   | single  | 5000.0/s   | **353.2µs** | **426.2µs** | **584.6µs**   | 100.00% |
| fiber   | cycled  | 5000.0/s   | 354.6µs     | 427.5µs     | 596.9µs       | 100.00% |
| echo    | single  | 5000.0/s   | 392.6µs     | 475.3µs     | 666.8µs       | 100.00% |
| echo    | cycled  | 5000.0/s   | 395.5µs     | 477.9µs     | 640.5µs       | 100.00% |
| chi     | single  | 5000.0/s   | 394.3µs     | 490.8µs     | **2.98ms** ⚠  | 100.00% |
| chi     | cycled  | 5000.0/s   | 397.7µs     | 479.8µs     | 665.2µs       | 100.00% |

Reading:

- **Throughput** — all ten cells sustain 5000 req/s at 100% success. None is
  saturated here; push the rate higher (`./bench.sh 20000 30s`) to find each
  engine's ceiling.
- **Three p50 tiers emerge**:
  1. **Fiber (~354 µs)** — clear winner, ~10% better than the next group.
  2. **Echo / chi (~393–398 µs)** — middle tier, essentially tied.
  3. **stdlib / gin (~420 µs)** — slowest, also tied within noise.
- **`single` vs `cycled` is essentially identical** across all five engines —
  confirms the working set (~15 MB) fits InnoDB's default 128 MB buffer pool.
  Index-walk cost is negligible relative to the JOIN planner + round-trip.
- **chi `single` p99 outlier (2.98 ms)** — ~8× the median, isolated against
  the other 9 cells. Last run had the same signature on Gin (`cycled`,
  4.58 ms) that **did not repeat** this run — so likely a GC pause or brief
  contention, not engine-specific. Worth one more pass to confirm.
- **Compared to the static `/health` baseline** (below), JOIN + sqlx +
  MySQL adds ~310–390 µs of constant p50 — DB+driver account for ~90%
  of today's latency, framework ≤10%.

#### Full matrix — transport × serialization

macOS arm64, 5000 req/s × 30s, 7 backends × 2 modes per engine (70 cells,
~40 min). Every cell sustained 5000 req/s at 100% success — nobody saturated,
so latency is the story. **p50, `single` mode** below (full per-cell data —
p95/p99 and `cycled` — in `bench-results-full/summary.txt`).

> The `grpc-flat` (FlatBuffers) backend was added after this run; at ~311 B it
> tracks `grpc`/`grpc-avro` (serialization is noise at this size — see the
> payload matrix below). Re-run `./bench-full.sh` to fill its column.

| Engine | mysql | rest (h1/json) | resth2 (h2/json) | resth2-pb (h2/pb) | resth2-avro (h2/avro) | grpc (pb) | grpc-avro |
|--------|------:|------:|------:|------:|------:|------:|------:|
| stdlib | 431µs | 525µs | 543µs | 538µs | 542µs | 558µs | 558µs |
| gin    | 398µs | 520µs | 544µs | 543µs | 543µs | 562µs | 560µs |
| fiber  | **359µs** | **513µs** | **535µs** | **530µs** | **533µs** | **548µs** | **546µs** |
| echo   | 397µs | 521µs | 544µs | 543µs | 537µs | 559µs | 556µs |
| chi    | 400µs | 523µs | 543µs | 541µs | 540µs | 560µs | 558µs |

Backend cost (p50 averaged across the 10 cells per backend, single+cycled):

| Backend       | transport    | serialization | avg p50 | Overhead vs mysql |
|---------------|--------------|---------------|---------|-------------------|
| mysql         | in-process   | —             | 399 µs  | —                 |
| rest          | HTTP/1.1     | JSON          | 522 µs  | +123 µs (+31%)    |
| resth2-avro   | HTTP/2 (h2c) | Avro          | 542 µs  | +143 µs (+36%)    |
| resth2-pb     | HTTP/2 (h2c) | protobuf      | 544 µs  | +145 µs (+36%)    |
| resth2        | HTTP/2 (h2c) | JSON          | 547 µs  | +148 µs (+37%)    |
| grpc-avro     | gRPC         | Avro          | 557 µs  | +158 µs (+40%)    |
| grpc          | gRPC         | protobuf      | 559 µs  | +160 µs (+40%)    |

Readings — each axis isolated by holding the other fixed:

- **protobuf vs Avro is a tie.** `resth2-pb` (544) ≈ `resth2-avro` (542) on
  HTTP/2; `grpc` (559) ≈ `grpc-avro` (557) on gRPC. The gaps (~2 µs) are
  inside the noise. For a ~250 B aggregate the serialization cost is
  negligible against the JOIN + round-trip — **serialization is not the
  bottleneck**, so the binary-vs-binary choice buys nothing here.
- **JSON doesn't lose to binary either.** `resth2` (json, 547) ≈ `resth2-pb`
  (544) ≈ `resth2-avro` (542). Same reason: encode/decode is rounding error
  next to the DB hop. Swapping JSON for protobuf/Avro at this payload size is
  not worth the schema machinery on latency grounds alone.
- **gRPC is ~15 µs *slower* than plain HTTP/2 with the same protobuf**
  (`grpc` 559 vs `resth2-pb` 544, every engine). gRPC's framing costs a bit
  more than h2c for a simple unary call; it earns its keep with streaming,
  multiplexing under load, or cross-language contracts — none exercised here.
- **HTTP/2 is ~25 µs *slower* than HTTP/1.1** for JSON (`resth2` 547 vs
  `rest` 522). On a single sequential loopback connection, HTTP/2's
  multiplexing brings no benefit while HPACK/framing add overhead. HTTP/2
  pays off with many concurrent streams, which this attack pattern lacks.
- **The dominant axis is the framework, not the protocol.** Fiber (359 µs
  in-process) leads the pack by ~40–70 µs — a wider spread than any
  transport/serialization difference on the network backends.
- **`single` vs `cycled` is essentially identical across all 70 cells** —
  the 150k working set fits the default 128 MB InnoDB buffer pool, so we
  measure index walk + JOIN planner, not disk I/O.
- **One tail outlier:** gin/resth2-pb/cycled p99 = 2.31 ms vs ~850 µs
  elsewhere; its p95 is well-behaved, so it's a transient pause (likely GC),
  not a regression.

Practical takeaway:

- **The network hop dominates everything else.** In-process (~399 µs) →
  any network backend (~520–560 µs) is a flat ~120–160 µs tax, roughly the
  same regardless of protocol or wire format. Stay in-process when you can.
- **At small payloads, pick the protocol for ergonomics, not latency.**
  REST/JSON is the simplest and the *fastest* network option here; HTTP/2
  and gRPC and binary serialization all cost a little more, not less.
  protobuf/Avro/gRPC earn their keep with large payloads, streaming, or
  cross-language type safety — re-run with a bigger aggregate to see the
  serialization axis actually separate.
- **Framework choice matters most when the framework is the bottleneck.**
  In-process, fiber's ~10% edge is real. Behind a network hop the five
  engines collapse into a ~15 µs band — pick by ergonomics, not by µs.

#### Payload matrix — where serialization finally separates

`bench-payload.sh`, macOS arm64, fiber only, 500 req/s × 10s, the list endpoint
`/v1/transactions/:count`. p50 below; **N is the transaction count** (~311 B
each, so N=1000 ≈ 300 KB, N=5000 ≈ 1.5 MB of JSON).

| Backend                | N=10   | N=100  | N=1000 | N=5000 *(saturated)* |
|------------------------|-------:|-------:|-------:|---------------------:|
| mysql (in-process)     | 1.20ms | 1.49ms | 2.47ms | 3.36s / 100% ok      |
| rest (h1 / JSON)       | 1.60ms | 2.19ms | 5.49ms | 4.19s / **60% ok**   |
| resth2 (h2 / JSON)     | 1.64ms | 2.20ms | 5.92ms | 3.98s / **62% ok**   |
| resth2-pb (h2 / pb)    | 1.55ms | 1.94ms | 3.73ms | 3.24s / **69% ok**   |
| resth2-avro (h2 / avro)| 1.54ms | 1.89ms | 3.26ms | 3.15s / **70% ok**   |
| grpc (pb)              | 1.53ms | 1.93ms | 3.32ms | 3.88s / 100% ok      |
| grpc-avro              | 1.48ms | 1.86ms | 3.02ms | 3.44s / 100% ok      |
| grpc-flat (FlatBuffers)| 1.49ms | 1.89ms | 3.04ms | 3.38s / 100% ok      |

Readings:

- **≤ N=100 (~30 KB): still a tie.** All formats sit within ~0.3 ms — the DB
  hop dominates, serialization is noise. Same conclusion as the single
  aggregate, just confirmed across a 100× payload range.
- **N=1000 (~300 KB): the formats split.** JSON (`rest` 5.49 / `resth2` 5.92)
  is **~60–95% slower** than the binary formats (`resth2-pb` 3.73, `resth2-avro`
  3.26, `grpc-avro` 3.02, `grpc-flat` 3.04). This is the payback the small-payload
  runs never showed — at scale, JSON's encode/parse + larger bytes cost real
  milliseconds.
- **Avro ≈ protobuf ≈ FlatBuffers** at every size. FlatBuffers' zero-copy does
  **not** pull ahead here — because the front still decodes the upstream
  response into `app.Transaction` and **re-encodes it as JSON for the client**,
  so there's no zero-copy passthrough to exploit. The compact binary formats win
  over JSON on the *hop*, but among themselves it's a wash for this access pattern.
- **N=5000 (~1.5 MB): everyone saturates at 500 req/s** (even `mysql` sustains
  only 271 req/s). The tell is **success rate**: the JSON REST backends start
  **dropping requests (60–62%)**, the binary REST backends hold a bit better
  (69–70%), and the gRPC backends keep 100% but pay it back as multi-second
  queue latency (p99 12–16 s). JSON fails first under bandwidth pressure.
- **HTTP/1.1 vs HTTP/2 for JSON stays a slight loss for h2** (`rest` ≤ `resth2`
  at every size) — consistent with the single-aggregate matrix; multiplexing
  brings nothing to this one-request-at-a-time pattern.

Caveat worth stating: the front **always re-serializes to JSON for the vegeta
client**, so the client-facing leg is identical across backends — the table
isolates the front↔dataservice hop, not an end-to-end binary pipeline. A true
zero-copy story would need FlatBuffers all the way to the client.

#### Previous baseline — static `/health` (reference)

macOS arm64, 5000 req/s × 30s on `/health` (hardcoded JSON, no MySQL in the
chain):

| Engine | Throughput | p50      | p95      | p99       | Success |
|--------|------------|----------|----------|-----------|---------|
| stdlib | 5000.03/s  | 39.7µs   | 71.4µs   | 134.1µs   | 100.00% |
| gin    | 5000.02/s  | 39.4µs   | 66.9µs   | 124.7µs   | 100.00% |
| fiber  | 5000.04/s  | 32.0µs   | 57.7µs   | 93.7µs    | 100.00% |

The baseline isolates pure framework cost (no DB in the chain). It only
covers three engines because Echo and chi were added later. For the three
shared engines, the ordering holds — Fiber fastest, stdlib/gin tied second.
The DB-bound run grows p50 ~10× (~40 µs → ~400 µs), confirming ~90% of
today's latency is DB + driver and ~10% framework.

### Notes

- Fiber uses fasthttp under the hood, not net/http. The adapter `copy()`s the
  request body because fasthttp reuses the buffer across requests.
- Routes are declared in `:name` form (Gin/Fiber convention). The stdlib
  adapter translates to `{name}` before registering on `http.ServeMux`, since
  Go 1.22+ pattern routing uses that syntax.
- `bench.sh` parses vegeta's current compact format — a single
  `Latencies [mean, 50, 95, 99, max]` line instead of per-percentile lines.
  Older vegeta versions that still emit `50th`/`95th`/`99th` will leave the
  comparative table empty (attacks and individual reports still work).
- The MySQL healthcheck uses `mysqladmin -h 127.0.0.1 ping` (TCP, not the
  Unix socket) so it only marks the container healthy once the real mysqld is
  listening on 3306. The Unix-socket variant would pass during the temporary
  init server phase and cause client connects to fail with `unexpected EOF`.
- `mysql-init/*.sql` only runs when MySQL initializes an empty data dir. To
  re-apply (after schema/seed changes): `docker compose down && rm -rf
  mysql-data && docker compose up -d`.
- The MySQL adapter uses `parseTime=true` in the DSN so `time.Time` scans the
  `DATETIME` column. Without it, `created_at` comes back zero and the
  integration test catches the omission.
- The warmup before each measurement prevents Go's cold-start (GC, initial
  goroutine allocation) from polluting results.
- For consistent results, close CPU/network-heavy apps and run several
  iterations.
- Generated proto stubs (`proto/transactionspb/`) are committed, so plain
  `go build` works without `protoc`. To regenerate (only after editing
  `transactions.proto`):
  ```bash
  brew install protobuf
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ```
  Then run the `protoc` command in the Microservice topology section.

---

## Português

Prova de conceito que demonstra inversão de dependência em Go para trocar
entre cinco frameworks HTTP (stdlib, Gin, Fiber, Echo, chi) e comparar as
suas performances sob carga constante com vegeta.

O serviço front pode resolver o agregado por vários adapters, selecionados
pela flag `-repo` — cada um um par **(transporte, serialização)** para o
benchmark isolar protocolo de formato de fio:

| `-repo`       | transporte    | serialização | porta dataservice |
|---------------|---------------|--------------|-------------------|
| `mysql` *(def)* | in-process  | —            | —                 |
| `rest`        | HTTP/1.1      | JSON         | `:9090`           |
| `resth2`      | HTTP/2 (h2c)  | JSON         | `:9092`           |
| `resth2-pb`   | HTTP/2 (h2c)  | protobuf     | `:9092`           |
| `resth2-avro` | HTTP/2 (h2c)  | Avro         | `:9092`           |
| `grpc`        | gRPC          | protobuf     | `:9091`           |
| `grpc-avro`   | gRPC          | Avro         | `:9091`           |

Isso torna duas perguntas respondíveis de forma limpa, cada uma fixando o
resto: **HTTP/1.1 vs HTTP/2** (`rest` vs `resth2`), **HTTP/2 vs gRPC** com a
serialização presa em protobuf (`resth2-pb` vs `grpc`) e **protobuf vs Avro**
em cada transporte (`resth2-pb` vs `resth2-avro`; `grpc` vs `grpc-avro`).

O dataservice é o dono da ligação ao MySQL e expõe os três transportes no
mesmo binário (REST/HTTP1 `:9090`, gRPC `:9091`, REST/h2c `:9092`), portanto
o front escolhe o par no arranque. O endpoint REST faz content-negotiation de
JSON/protobuf/Avro pelo header `Accept`; o gRPC serve protobuf e Avro no mesmo
listener via content-subtype. Mesma shape, mesma interface
`app.TransactionRepository` — só muda o adapter. O benchmark cobre o caminho
completo: framework + (sqlx ou hop REST/gRPC) + MySQL.

### Índice

- [Requisitos](#requisitos)
- [Estrutura do projecto](#estrutura-do-projecto)
- [Inicializar o módulo](#inicializar-o-módulo)
- [Subir o MySQL](#subir-o-mysql)
- [Executar o servidor manualmente](#executar-o-servidor-manualmente)
- [Executar a suite de testes](#executar-a-suite-de-testes)
- [Executar o benchmark](#executar-o-benchmark)
- [Analisar resultados](#analisar-resultados)
- [Métricas relevantes](#métricas-relevantes)
- [Resultado exemplo](#resultado-exemplo)
- [Notas](#notas)

### Requisitos

- Go 1.26+
- Docker + Docker Compose (para o MySQL)
- curl (para o healthcheck do script)
- vegeta

```bash
go install github.com/tsenart/vegeta@latest
```

### Estrutura do projecto

```
go-http-di/
├── main.go                       # Serviço front: composition root, -engine + -repo
├── cmd/
│   └── dataservice/              # Serviço de dados: dono do MySQL, expõe REST/HTTP1 + REST/h2c + gRPC
│       ├── main.go
│       ├── handler.go            # Handler REST (content-negotiation JSON/protobuf/Avro)
│       ├── handler_test.go
│       ├── grpc.go               # TransactionService em gRPC (protobuf)
│       └── grpc_avro.go          # TransactionServiceAvro em gRPC (ServiceDesc manual)
├── proto/
│   ├── transactions.proto        # Schema gRPC protobuf
│   └── transactionspb/           # Stubs Go gerados (commitados)
├── serde/                        # Codecs de serialização (JSON/protobuf/Avro); schema Avro vive aqui
│   ├── serde.go                  # Porta Codec + fábrica ForAccept
│   └── json.go / protobuf.go / avro.go
├── bench.sh                      # Comparação de engines (5 × 2 modos, só -repo mysql)
├── bench-full.sh                 # Matriz cheia (5 engines × 7 backends × 2 modos)
├── docker-compose.yml            # MySQL 8.0 com bind mount
├── app/
│   ├── app.go                    # Core: App struct + handlers
│   └── transactions.go           # Driven port: Transaction + TransactionRepository + sentinel
├── server/
│   ├── server.go                 # Driving port: interface Server + tipos Request/Response
│   ├── stdlib/stdlib.go          # Adapter net/http
│   ├── ginadapt/gin.go           # Adapter Gin
│   ├── fiberadapt/fiber.go       # Adapter Fiber
│   ├── echoadapt/echo.go         # Adapter Echo
│   └── chiadapt/chi.go           # Adapter chi
├── db/
│   ├── mysql/mysql.go            # Adapter MySQL (sqlx) para TransactionRepository
│   ├── restclient/restclient.go  # Adapter REST client (HTTP/1.1 ou h2c; codec-plugável)
│   ├── grpcclient/grpcclient.go  # Adapter gRPC client (protobuf)
│   └── grpcavroclient/grpcavroclient.go # Adapter gRPC client (codec Avro)
├── mysql-init/
│   ├── 01-schema.sql             # DDL das 3 tabelas; corre só no 1º boot
│   └── 02-seed.sql               # Bulk seed via CTE recursiva (50k/150k/150k)
├── mysql-data/                   # Volume bind do MySQL (gitignored)
└── bench-results/                # Gerado após benchmark (gitignored)
    ├── <engine>-<mode>.bin / .txt / .html
    ├── targets-cycled.txt
    ├── summary.txt
    └── combined.html
```

### Inicializar o módulo

```bash
go mod init example.com/httpdi
go mod tidy
```

### Subir o MySQL

```bash
docker compose up -d              # MySQL 8.0 em :3306, data em ./mysql-data
docker compose logs -f mysql      # acompanhar primeiro boot (~30-60s por causa do seed)
docker compose down               # parar (mantém dados)
rm -rf mysql-data                 # wipe total + re-aplicar init scripts no próximo up
```

Credenciais default (alterar antes de qualquer uso sério): user `httpdi`,
password `httpdipass`, database `httpdi`. A inicialização está dividida em
dois ficheiros que correm uma única vez quando o `mysql-data/` está vazio:

- [`mysql-init/01-schema.sql`](mysql-init/01-schema.sql) — DDL das três
  tabelas:
  - `customer (id, nome, create_date)`.
  - `` `transaction` (id, value, customer_id FK → customer, create_date) `` —
    backtickada porque `TRANSACTION` é palavra reservada no MySQL.
  - `cart_snapshot (id, transaction_id FK → transaction UNIQUE, create_date)`
    — **1:1** com `transaction`.
- [`mysql-init/02-seed.sql`](mysql-init/02-seed.sql) — bulk seed via CTE
  recursiva:
  - **50 000** customers (`Customer #1` .. `#50000`).
  - **150 000** transactions (3 por customer; value aleatório 0–1000).
  - **150 000** cart_snapshots (1:1 com transactions).

IDs determinísticos: customer N é `11111111-…-LPAD(N,12,'0')`, transaction N
é `00000000-…-LPAD(N,12,'0')`, cart_snapshot N é `22222222-…-LPAD(N,12,'0')`.
A transaction N pertence ao customer `FLOOR((N-1)/3)+1` — portanto as
transactions 1, 2 e 3 estão todas no customer 1. O ID `…001` em qualquer das
três tabelas leva ao mesmo agregado (é onde o benchmark e o teste de
integração batem).

O endpoint `/v1/transaction/:id` faz `transaction ⋈ customer ⋈ cart_snapshot`
(INNER JOIN dos três) e devolve um agregado denormalizado em JSON.

**Primeiro boot ~30–60s** por causa do seed (o `wait_for_mysql` do bench.sh
espera até 5 min). Boots seguintes ignoram a inicialização (~5s). Para
reaplicar schema/seed: `docker compose down && rm -rf mysql-data && docker
compose up -d`.

### Executar o servidor manualmente

**MySQL in-process** (default: front fala directamente com o MySQL):

```bash
# stdlib (default), conecta a MySQL no DSN default
go run main.go

# Gin / Fiber / Echo / chi
go run main.go -engine gin
go run main.go -engine fiber
go run main.go -engine echo
go run main.go -engine chi

# Porta custom + DSN custom (também aceita env DB_DSN)
go run main.go -engine gin -addr :3000 -dsn "user:pass@tcp(host:3306)/db?parseTime=true"
```

**Microserviço — REST ou gRPC** (dois terminais):

```bash
# Terminal A: serviço de dados (dono do MySQL, REST/HTTP1 :9090, gRPC :9091, REST/h2c :9092)
go run ./cmd/dataservice -rest-addr :9090 -grpc-addr :9091 -rest-h2c-addr :9092

# Terminal B: front escolhe o par (transporte, serialização) (qualquer engine)
go run main.go -engine fiber -repo rest        -repo-addr http://localhost:9090   # HTTP/1.1 + JSON
go run main.go -engine fiber -repo resth2      -repo-addr http://localhost:9092   # HTTP/2  + JSON
go run main.go -engine fiber -repo resth2-pb   -repo-addr http://localhost:9092   # HTTP/2  + protobuf
go run main.go -engine fiber -repo resth2-avro -repo-addr http://localhost:9092   # HTTP/2  + Avro
go run main.go -engine fiber -repo grpc        -repo-addr localhost:9091          # gRPC    + protobuf
go run main.go -engine fiber -repo grpc-avro   -repo-addr localhost:9091          # gRPC    + Avro
go run main.go -engine fiber -repo grpc-flat   -repo-addr localhost:9091          # gRPC    + FlatBuffers
```

Regenerar os stubs proto **só quando o [proto/transactions.proto](proto/transactions.proto)
mudar** (precisa de `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` —
ver Notas no fim):

```bash
PATH="$(go env GOPATH)/bin:$PATH" protoc \
  --go_out=. --go_opt=module=example.com/httpdi \
  --go-grpc_out=. --go-grpc_opt=module=example.com/httpdi \
  proto/transactions.proto
```

Testar (smoke, igual independentemente do `-repo`):

```bash
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo
curl http://localhost:8080/v1/transaction/00000000-0000-0000-0000-000000000001
```

### Executar a suite de testes

```bash
go test ./...                          # unitários + integração HTTP (~2.5s, sem MySQL)
go test -race ./...                    # com detector de races
go test -tags integration ./db/mysql   # integração MySQL (requer docker compose up)
go test -v ./server/stdlib             # verbose, um pacote
```

Cobertura:

- **Unitários** — `app` valida `Health`, `Hello` (incluindo o default `"world"`)
  e `GetTransaction` (200/404/500 com `fakeTxRepo`); `server/stdlib` valida a
  tradução de `:name` → `{name}`.
- **Integração HTTP** — uma suite partilhada em `server/internal/servertest`
  regista rotas, sobe cada adapter em portos dedicados (stdlib `:18081`,
  gin `:18082`, fiber `:18083`, echo `:18084`, chi `:18085`), faz HTTP real
  e verifica `/health`, `/hello/:name` e 404. Cada adapter tem um `_test.go`
  mínimo que invoca a suite.
- **Integração MySQL** — `db/mysql/mysql_test.go` está atrás da build tag
  `integration` (precisa do container UP). Excluído de `go test ./...` por
  default.

### Executar o benchmark

Três scripts respondem a três perguntas:

- **[`bench.sh`](bench.sh)** — *Q1: qual framework é mais rápido?*
  Mantém `-repo mysql` (in-process). Varia engine × modo (5 × 2 = 10
  cells). ~5 min. Bom para iteração rápida.
- **[`bench-full.sh`](bench-full.sh)** — *Q2: como é que cada framework se
  comporta sob cada (transporte, serialização)?* Matriz cheia engine ×
  backend × modo no agregado único (5 × 8 × 2 = 80 cells). ~46 min. Os 8
  backends são `mysql`, `rest` (h1+json), `resth2`/`resth2-pb`/`resth2-avro`
  (h2c + json/pb/avro), `grpc` (pb), `grpc-avro`, `grpc-flat`. Output
  agrupado por engine — 5 sub-tabelas de 16 linhas — para ler directamente
  HTTP/1.1 vs HTTP/2 (`rest` vs `resth2`), HTTP/2 vs gRPC (`resth2-pb` vs
  `grpc`) e protobuf vs Avro vs FlatBuffers (`grpc` vs `grpc-avro` vs
  `grpc-flat`). Escreve para `bench-results-full/`. Corte os arrays
  `BACKENDS`/`ENGINES` se 80 cells for demasiado.
- **[`bench-payload.sh`](bench-payload.sh)** — *Q3: como os formatos de fio
  se comportam à medida que o payload cresce?* O endpoint de lista
  `/v1/transactions/:count`, só fiber, backend × tamanho: 8 backends ×
  N{10,100,1000,5000} = 32 cells. ~8 min (default 500 req/s × 10s — mais
  baixo porque os payloads chegam a ~1.5 MB). Escreve para
  `bench-results-payload/`. É aqui que a serialização deve separar; a ~311 B
  (bench-full) é ruído.

```bash
chmod +x bench.sh bench-full.sh

# Comparação de engines (default: 5000 req/s durante 30s por engine)
./bench.sh
./bench.sh 10000 60s            # rate + duração custom

# Matriz cheia (engine × backend × modo)
./bench-full.sh
./bench-full.sh 10000 60s
```

Ambos correm **dois modos** (por engine no bench.sh, por engine×backend
no bench-full.sh), back-to-back sem reiniciar o servidor entre modos:

- **`single`** — todos os pedidos batem na mesma row
  (`…000000000001`). InnoDB mantém-a no buffer pool → mede o caminho
  quente framework + sqlx + driver + MySQL com zero variância de cache.
- **`cycled`** — vegeta cicla pelos 150 000 IDs seedados (ficheiro
  `bench-results/targets-cycled.txt`, gerado no arranque). Exercita o
  índice PK + JOIN em todo o dataset. O working set cabe nos 128 MB de
  buffer pool default, portanto continua maioritariamente em cache; a
  diferença para o `single` é sobretudo custo de index walk e
  planner-work do JOIN.

Cada modo tem o seu warmup de 500 req/s × 3s usando a *mesma fonte* que o
ataque, para o runtime Go e qualquer setup per-target estabilizar antes
da medição.

Fluxo completo:

1. Verifica `vegeta` e `docker` no PATH.
2. `docker compose up -d` e espera o MySQL ficar `healthy` (até 5 min em
   primeiro boot por causa do seed).
3. Compila o binário uma vez (`go build`).
4. Gera o ficheiro de targets ciclados (150k linhas, ~12 MB).
5. Para cada um dos cinco engines: inicia o servidor, espera o
   healthcheck, corre os dois modos (single, cycled), pára o servidor.
6. Gera relatório texto + plot HTML por (engine, mode) — 10 relatórios
   no total (5 × 2).
7. Imprime a tabela comparativa de 10 linhas e regista `summary.txt` +
   `combined.html` (grid auto-fit com todos os plots).
8. `trap` no `EXIT` faz `docker compose down` mesmo em erro / Ctrl+C.

### Analisar resultados

Os ficheiros `.bin` são o formato nativo do vegeta e permitem re-análise sem
repetir o teste:

```bash
# Re-gerar relatório texto
vegeta report < bench-results/stdlib-single.bin

# Histograma de latência
vegeta report -type=hist < bench-results/gin-cycled.bin

# Exportar para JSON (integração com jq, scripts, etc.)
vegeta report -type=json < bench-results/fiber-single.bin

# Gerar novo plot
vegeta plot < bench-results/stdlib-cycled.bin > novo-plot.html
```

O relatório combinado (`bench-results/combined.html`) mostra os dez plots
no browser (5 engines × 2 modes), em grid auto-fit.

### Métricas relevantes

| Métrica     | O que indica                                                                |
|-------------|-----------------------------------------------------------------------------|
| Throughput  | Requisições processadas por segundo (deve igualar ou aproximar o rate alvo) |
| Latency p50 | Latência mediana — metade dos pedidos respondeu abaixo deste valor          |
| Latency p95 | Tail latency — 5% dos pedidos ficaram acima deste valor                     |
| Latency p99 | Outliers — 1% dos pedidos mais lentos                                       |
| Success     | Percentagem de respostas com status 2xx/3xx                                 |

Se o throughput ficar abaixo do rate alvo, o servidor está a saturar nessa
taxa. Nesse caso, compara o ponto de saturação entre os três engines.

### Resultado exemplo

#### Corrida actual — `/v1/transaction/:id` com JOIN MySQL

macOS arm64, 5000 req/s × 30s, dois modos por engine (executados back-to-back):

| Engine  | Mode    | Throughput | p50         | p95         | p99           | Success |
|---------|---------|------------|-------------|-------------|---------------|---------|
| stdlib  | single  | 5000.0/s   | 421.4µs     | 513.5µs     | 723.3µs       | 100.00% |
| stdlib  | cycled  | 5000.0/s   | 420.4µs     | 516.4µs     | 704.2µs       | 100.00% |
| gin     | single  | 5000.0/s   | 423.1µs     | 514.8µs     | 701.8µs       | 100.00% |
| gin     | cycled  | 5000.0/s   | 409.6µs     | 498.8µs     | 691.5µs       | 100.00% |
| fiber   | single  | 5000.0/s   | **353.2µs** | **426.2µs** | **584.6µs**   | 100.00% |
| fiber   | cycled  | 5000.0/s   | 354.6µs     | 427.5µs     | 596.9µs       | 100.00% |
| echo    | single  | 5000.0/s   | 392.6µs     | 475.3µs     | 666.8µs       | 100.00% |
| echo    | cycled  | 5000.0/s   | 395.5µs     | 477.9µs     | 640.5µs       | 100.00% |
| chi     | single  | 5000.0/s   | 394.3µs     | 490.8µs     | **2.98ms** ⚠  | 100.00% |
| chi     | cycled  | 5000.0/s   | 397.7µs     | 479.8µs     | 665.2µs       | 100.00% |

Leituras:

- **Throughput** — todas as dez células aguentam 5000 req/s com 100% sucesso.
  Ninguém satura aqui; sobe a taxa (`./bench.sh 20000 30s`) para encontrar o
  tecto de cada um.
- **Três grupos de p50**:
  1. **Fiber (~354 µs)** — vencedor claro, ~10% melhor que o grupo seguinte.
  2. **Echo / chi (~393–398 µs)** — meio do pelotão, praticamente empatados.
  3. **stdlib / gin (~420 µs)** — os mais lentos, também empatados dentro
     do ruído.
- **`single` vs `cycled` é praticamente idêntico** nos cinco engines —
  confirma que o working set (~15 MB) cabe no buffer pool default do InnoDB
  (128 MB). Custo de index walk é negligível face ao planner do JOIN +
  round-trip.
- **Outlier do chi em `single` p99 (2.98 ms)** — ~8× a mediana, isolado dos
  outros 9 cells. A corrida anterior tinha a mesma assinatura no Gin
  (`cycled`, 4.58 ms) e **não se repetiu** desta vez — portanto é
  provavelmente uma pausa de GC ou contention pontual, não específico do
  engine. Vale uma nova execução para confirmar.
- **Comparado à baseline estática do `/health`** (mais abaixo), JOIN +
  sqlx + MySQL adiciona ~310–390 µs de p50 constantes — DB+driver
  representam ~90% da latência hoje, framework ≤10%.

#### Matriz cheia — transporte × serialização

macOS arm64, 5000 req/s × 30s, 7 backends × 2 modos por engine (70 cells,
~40 min). Todas as cells sustentaram 5000 req/s a 100% de sucesso — ninguém
saturou, logo a história é a latência. **p50, modo `single`** abaixo (dados
completos por cell — p95/p99 e `cycled` — em `bench-results-full/summary.txt`).

> O backend `grpc-flat` (FlatBuffers) foi adicionado depois desta corrida; a
> ~311 B acompanha `grpc`/`grpc-avro` (serialização é ruído neste tamanho —
> ver a matriz de payload abaixo). Re-execute `./bench-full.sh` para preencher
> a coluna dele.

| Engine | mysql | rest (h1/json) | resth2 (h2/json) | resth2-pb (h2/pb) | resth2-avro (h2/avro) | grpc (pb) | grpc-avro |
|--------|------:|------:|------:|------:|------:|------:|------:|
| stdlib | 431µs | 525µs | 543µs | 538µs | 542µs | 558µs | 558µs |
| gin    | 398µs | 520µs | 544µs | 543µs | 543µs | 562µs | 560µs |
| fiber  | **359µs** | **513µs** | **535µs** | **530µs** | **533µs** | **548µs** | **546µs** |
| echo   | 397µs | 521µs | 544µs | 543µs | 537µs | 559µs | 556µs |
| chi    | 400µs | 523µs | 543µs | 541µs | 540µs | 560µs | 558µs |

Custo por backend (p50 médio das 10 cells de cada backend, single+cycled):

| Backend       | transporte   | serialização | p50 médio | Overhead vs mysql |
|---------------|--------------|--------------|-----------|-------------------|
| mysql         | in-process   | —            | 399 µs    | —                 |
| rest          | HTTP/1.1     | JSON         | 522 µs    | +123 µs (+31%)    |
| resth2-avro   | HTTP/2 (h2c) | Avro         | 542 µs    | +143 µs (+36%)    |
| resth2-pb     | HTTP/2 (h2c) | protobuf     | 544 µs    | +145 µs (+36%)    |
| resth2        | HTTP/2 (h2c) | JSON         | 547 µs    | +148 µs (+37%)    |
| grpc-avro     | gRPC         | Avro         | 557 µs    | +158 µs (+40%)    |
| grpc          | gRPC         | protobuf     | 559 µs    | +160 µs (+40%)    |

Leituras — cada eixo isolado fixando o outro:

- **protobuf vs Avro é empate.** `resth2-pb` (544) ≈ `resth2-avro` (542) em
  HTTP/2; `grpc` (559) ≈ `grpc-avro` (557) em gRPC. As diferenças (~2 µs)
  estão dentro do ruído. Para um agregado de ~250 B o custo de serialização
  é desprezível face ao JOIN + round-trip — **serialização não é o gargalo**,
  logo a escolha binário-vs-binário não compra nada aqui.
- **JSON também não perde para binário.** `resth2` (json, 547) ≈ `resth2-pb`
  (544) ≈ `resth2-avro` (542). Mesma razão: encode/decode é erro de
  arredondamento ao lado do hop ao DB. Trocar JSON por protobuf/Avro neste
  tamanho de payload não vale a maquinaria de schema só por latência.
- **gRPC é ~15 µs *mais lento* que HTTP/2 puro com o mesmo protobuf**
  (`grpc` 559 vs `resth2-pb` 544, em todos os engines). O framing do gRPC
  custa um pouco mais que h2c numa chamada unária simples; justifica-se com
  streaming, multiplexing sob carga ou contratos cross-language — nada
  exercitado aqui.
- **HTTP/2 é ~25 µs *mais lento* que HTTP/1.1** em JSON (`resth2` 547 vs
  `rest` 522). Numa única conexão loopback sequencial, o multiplexing do
  HTTP/2 não traz benefício e o HPACK/framing acrescentam overhead. O HTTP/2
  compensa com muitas streams concorrentes, que este padrão de ataque não tem.
- **O eixo dominante é a framework, não o protocolo.** O Fiber (359 µs
  in-process) lidera por ~40–70 µs — um espalhamento maior que qualquer
  diferença de transporte/serialização nos backends de rede.
- **`single` vs `cycled` é praticamente idêntico nos 70 cells** — os 150k
  IDs cabem nos 128 MB default do buffer pool InnoDB, logo medimos index walk
  + planner de JOIN, não disk I/O.
- **Um outlier de cauda:** gin/resth2-pb/cycled p99 = 2.31 ms vs ~850 µs nos
  restantes; o p95 está bem comportado, logo é uma pausa pontual
  (provavelmente GC), não regressão.

Conclusões práticas:

- **O hop de rede domina tudo o resto.** In-process (~399 µs) → qualquer
  backend de rede (~520–560 µs) é um imposto plano de ~120–160 µs,
  praticamente igual independentemente de protocolo ou formato de fio. Fica
  in-process quando der.
- **Em payloads pequenos, escolhe o protocolo por ergonomia, não latência.**
  REST/JSON é o mais simples e o *mais rápido* na rede aqui; HTTP/2, gRPC e
  serialização binária custam todos um pouco mais, não menos. protobuf/Avro/
  gRPC justificam-se com payloads grandes, streaming ou tipagem cross-language
  — repete com um agregado maior para o eixo de serialização separar de facto.
- **A escolha de framework importa mais quando a framework é o gargalo.**
  In-process, a vantagem de ~10% do Fiber é real. Atrás de um hop de rede os
  cinco engines colapsam numa banda de ~15 µs — escolhe por ergonomia, não
  por µs.

#### Matriz de payload — onde a serialização finalmente separa

`bench-payload.sh`, macOS arm64, só fiber, 500 req/s × 10s, endpoint de lista
`/v1/transactions/:count`. p50 abaixo; **N é a contagem de transações** (~311 B
cada, logo N=1000 ≈ 300 KB, N=5000 ≈ 1.5 MB de JSON).

| Backend                | N=10   | N=100  | N=1000 | N=5000 *(saturado)* |
|------------------------|-------:|-------:|-------:|--------------------:|
| mysql (in-process)     | 1.20ms | 1.49ms | 2.47ms | 3.36s / 100% ok     |
| rest (h1 / JSON)       | 1.60ms | 2.19ms | 5.49ms | 4.19s / **60% ok**  |
| resth2 (h2 / JSON)     | 1.64ms | 2.20ms | 5.92ms | 3.98s / **62% ok**  |
| resth2-pb (h2 / pb)    | 1.55ms | 1.94ms | 3.73ms | 3.24s / **69% ok**  |
| resth2-avro (h2 / avro)| 1.54ms | 1.89ms | 3.26ms | 3.15s / **70% ok**  |
| grpc (pb)              | 1.53ms | 1.93ms | 3.32ms | 3.88s / 100% ok     |
| grpc-avro              | 1.48ms | 1.86ms | 3.02ms | 3.44s / 100% ok     |
| grpc-flat (FlatBuffers)| 1.49ms | 1.89ms | 3.04ms | 3.38s / 100% ok     |

Leituras:

- **≤ N=100 (~30 KB): ainda empate.** Todos dentro de ~0.3 ms — o hop ao DB
  domina, serialização é ruído. Mesma conclusão do agregado único, agora
  confirmada numa faixa de payload 100× maior.
- **N=1000 (~300 KB): os formatos separam.** JSON (`rest` 5.49 / `resth2` 5.92)
  é **~60–95% mais lento** que os binários (`resth2-pb` 3.73, `resth2-avro` 3.26,
  `grpc-avro` 3.02, `grpc-flat` 3.04). É o retorno que os payloads pequenos nunca
  mostraram — em escala, o encode/parse do JSON + bytes a mais custam ms reais.
- **Avro ≈ protobuf ≈ FlatBuffers** em todos os tamanhos. O zero-copy do
  FlatBuffers **não** dispara à frente aqui — porque o front ainda descodifica a
  resposta do upstream para `app.Transaction` e **re-codifica em JSON para o
  cliente**, logo não há passthrough zero-copy a explorar. Os binários ganham do
  JSON no *hop*, mas entre si dá empate neste padrão de acesso.
- **N=5000 (~1.5 MB): todos saturam a 500 req/s** (até o `mysql` só sustenta
  271 req/s). O sinal é a **taxa de sucesso**: os backends REST/JSON começam a
  **largar pedidos (60–62%)**, os REST binários aguentam um pouco melhor
  (69–70%), e os gRPC mantêm 100% mas pagam em latência de fila multi-segundo
  (p99 12–16 s). O JSON falha primeiro sob pressão de banda.
- **HTTP/1.1 vs HTTP/2 para JSON continua ligeira perda do h2** (`rest` ≤
  `resth2` em todos os tamanhos) — coerente com a matriz do agregado único; o
  multiplexing não traz nada a este padrão um-pedido-de-cada-vez.

Ressalva importante: o front **re-serializa sempre para JSON ao cliente vegeta**,
logo a perna voltada ao cliente é idêntica entre backends — a tabela isola o hop
front↔dataservice, não um pipeline binário ponta-a-ponta. Um cenário zero-copy
real exigiria FlatBuffers até ao cliente.

#### Baseline anterior — `/health` estático (referência)

Corrida em macOS arm64 a 5000 req/s × 30s no endpoint `/health` (JSON
hardcoded, sem MySQL na cadeia):

| Engine | Throughput | p50      | p95      | p99       | Success |
|--------|------------|----------|----------|-----------|---------|
| stdlib | 5000.03/s  | 39.7µs   | 71.4µs   | 134.1µs   | 100.00% |
| gin    | 5000.02/s  | 39.4µs   | 66.9µs   | 124.7µs   | 100.00% |
| fiber  | 5000.04/s  | 32.0µs   | 57.7µs   | 93.7µs    | 100.00% |

A baseline isola o custo puro de framework (sem DB no caminho). Cobre só
três engines porque o Echo e o chi foram adicionados mais tarde. Para os
três engines partilhados, a ordem mantém-se — Fiber à frente, stdlib/gin
empatados em segundo. A corrida com DB cresce o p50 ~10× (~40 µs →
~400 µs), confirmando que ~90% da latência hoje é DB + driver e ~10%
framework.

### Notas

- O Fiber usa fasthttp por baixo, não net/http. O adapter faz `copy()` do body
  porque o fasthttp reutiliza o buffer entre requests.
- As rotas são declaradas no formato `:name` (convenção Gin/Fiber). O adapter
  da stdlib traduz para `{name}` antes de registar no `http.ServeMux`, já que
  o pattern routing do Go 1.22+ usa essa sintaxe.
- O `bench.sh` parseia o formato compacto actual do vegeta — uma única linha
  `Latencies [mean, 50, 95, 99, max]` em vez de linhas separadas por
  percentil. Se usares uma versão antiga do vegeta que ainda produza
  `50th`/`95th`/`99th` em linhas próprias, a tabela comparativa final virá
  vazia (os ataques e os relatórios individuais continuam a funcionar).
- O healthcheck do MySQL usa `mysqladmin -h 127.0.0.1 ping` (TCP, não o socket
  Unix) para só marcar o container `healthy` quando o mysqld real está a
  ouvir em 3306. A variante via socket passava durante o servidor temporário
  de inicialização e causava `unexpected EOF` nos clientes.
- O `mysql-init/*.sql` só corre quando o MySQL inicializa um data dir vazio.
  Para reaplicar (após mudar schema ou seed): `docker compose down && rm -rf
  mysql-data && docker compose up -d`.
- O adapter MySQL usa `parseTime=true` no DSN para que `time.Time` faça scan
  da coluna `DATETIME`. Sem isso, `created_at` vem zero e o teste de integração
  apanha a omissão.
- O warmup antes de cada medição evita que o cold start do Go (GC, alocação
  inicial de goroutines) polua os resultados.
- Para resultados consistentes, fecha aplicações que consumam CPU/rede e corre
  várias iterações.
- Os stubs proto gerados (`proto/transactionspb/`) estão commitados, portanto o
  `go build` funciona sem `protoc`. Para regenerar (só depois de editar
  `transactions.proto`):
  ```bash
  brew install protobuf
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ```
  E depois corre o comando `protoc` na secção do microserviço.
