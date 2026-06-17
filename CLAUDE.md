# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- [.claude/rules/code_quality.md](.claude/rules/code_quality.md) — code quality standards (tests, docblocks, Effective Go, Go Proverbs, Clean Code, YAGNI).
- [.claude/rules/ANSWERING-DIRECT.md](.claude/rules/ANSWERING-DIRECT.md) — tom de resposta: consultor analítico e direto, sem maquiagem nem preenchimento.

## Project

HTTP framework benchmark / hexagonal-architecture proof of concept in Go. The front service ([main.go](main.go)) serves the same routes (`/health`, `/hello/:name`, `/v1/transaction/:transaction_id`) through one of five swappable HTTP backends — stdlib `net/http`, Gin, Fiber, Echo, or chi — selected by the `-engine` flag. The transaction endpoint can run via several driven adapters selected by `-repo` — each a **(transport, serialization)** pair so the benchmark can isolate protocol from wire format:

| `-repo`       | transport      | serialization | dataservice port |
|---------------|----------------|---------------|------------------|
| `mysql` (def) | in-process     | —             | —                |
| `rest`        | HTTP/1.1       | JSON          | `:9090`          |
| `resth2`      | HTTP/2 (h2c)   | JSON          | `:9092`          |
| `resth2-pb`   | HTTP/2 (h2c)   | protobuf      | `:9092`          |
| `resth2-avro` | HTTP/2 (h2c)   | Avro          | `:9092`          |
| `grpc`        | gRPC           | protobuf      | `:9091`          |
| `grpc-avro`   | gRPC           | Avro          | `:9091`          |

(`-repo-addr` is a URL for the `rest*` repos, `host:port` for the `grpc*` repos.) The comparisons this enables: **HTTP/1.1 vs HTTP/2** (`rest` vs `resth2`), **HTTP/2 vs gRPC** with framing isolated (`resth2-pb` vs `grpc`), and **protobuf vs Avro** on each transport (`resth2-pb` vs `resth2-avro`; `grpc` vs `grpc-avro`).

The dataservice exposes all three transports on separate ports (REST/HTTP1 `:9090`, gRPC `:9091`, REST/h2c `:9092`), so a single binary covers every protocol and the front picks at startup. The REST endpoint content-negotiates JSON/protobuf/Avro via the `Accept` header; gRPC serves protobuf and Avro on the same listener via content-subtype. This lets us measure the same `/v1/transaction/:id` aggregate (`transaction` ⋈ `customer` ⋈ `cart_snapshot`) across the whole matrix. `bench.sh` runs vegeta against each engine in turn and produces a comparative report.

The README is in Portuguese (`readme.MD`).

## Layout

```
main.go                              # front service: composition root, picks engine + repo
cmd/dataservice/main.go              # data service: owns MySQL, exposes REST/HTTP1 + REST/h2c + gRPC
cmd/dataservice/handler.go           #   REST handler (stdlib net/http), content-negotiated body
cmd/dataservice/grpc.go              #   gRPC service impl, protobuf (TransactionService.GetById)
cmd/dataservice/grpc_avro.go         #   gRPC service impl, Avro (hand-written ServiceDesc)
proto/transactions.proto             # gRPC wire schema (protobuf)
proto/transactionspb/                # generated Go stubs (committed; only regen when proto changes)
serde/serde.go                       # package serde — Codec port + ForAccept factory
serde/json.go,protobuf.go,avro.go    #   JSON / protobuf / Avro codecs; Avro also a grpc encoding.Codec
app/app.go                           # package app — application core, handlers, driven-port fields
app/transactions.go                  # Transaction type + TransactionRepository port + sentinel error
server/server.go                     # package server — Request/Response/HandlerFunc/Server contract
server/stdlib/stdlib.go              # package stdlib — net/http adapter
server/ginadapt/gin.go               # package ginadapt — Gin adapter
server/fiberadapt/fiber.go           # package fiberadapt — Fiber adapter
server/echoadapt/echo.go             # package echoadapt — Echo adapter
server/chiadapt/chi.go               # package chiadapt — chi adapter
server/internal/routeutil/           # `:name` ↔ `{name}` translation (shared by stdlib + chi)
server/internal/servertest/          # shared HTTP integration suite for adapter tests
db/mysql/mysql.go                    # MySQL adapter for TransactionRepository (sqlx)
db/restclient/restclient.go          # REST client adapter (HTTP/1.1 or h2c; codec-pluggable)
db/grpcclient/grpcclient.go          # gRPC client adapter (front → dataservice, protobuf)
db/grpcavroclient/grpcavroclient.go  # gRPC client adapter (front → dataservice, Avro codec)
docker-compose.yml                   # MySQL 8.0, bind-mounted data + init scripts
mysql-init/01-schema.sql             # DDL only — runs once on first init
mysql-init/02-seed.sql               # bulk seed via recursive CTE (50k/150k/150k rows)
bench.sh                             # engine comparison: 5 engines × 2 modes (mysql repo only)
bench-full.sh                        # full matrix: 5 engines × 7 backends × 2 modes
```

Module path: `example.com/httpdi`.

## Common commands

```bash
# MySQL (required for /v1/transaction/:id and the benchmark)
docker compose up -d           # bring MySQL up; data in ./mysql-data
docker compose down            # stop; data persists
rm -rf mysql-data              # wipe and re-run init script

# Front service — in-process MySQL (default; needs MySQL on :3306)
go run main.go                 # stdlib engine + mysql repo (defaults)
go run main.go -engine gin
go run main.go -engine fiber
go run main.go -engine echo
go run main.go -engine chi
go run main.go -engine gin -addr :3000
go run main.go -dsn "user:pass@tcp(host:3306)/db?parseTime=true"

# Microservice topology (front delegates to dataservice)
# Terminal A: dataservice owns MySQL, serves REST/HTTP1 :9090, gRPC :9091, REST/h2c :9092
go run ./cmd/dataservice -rest-addr :9090 -grpc-addr :9091 -rest-h2c-addr :9092

# Terminal B: front picks the (transport, serialization) pair
go run main.go -repo rest        -repo-addr http://localhost:9090   # HTTP/1.1 + JSON
go run main.go -repo resth2      -repo-addr http://localhost:9092   # HTTP/2  + JSON
go run main.go -repo resth2-pb   -repo-addr http://localhost:9092   # HTTP/2  + protobuf
go run main.go -repo resth2-avro -repo-addr http://localhost:9092   # HTTP/2  + Avro
go run main.go -repo grpc        -repo-addr localhost:9091          # gRPC    + protobuf
go run main.go -repo grpc-avro   -repo-addr localhost:9091          # gRPC    + Avro

# Regenerate proto stubs (only needed when proto/transactions.proto changes)
PATH="$(go env GOPATH)/bin:$PATH" protoc \
  --go_out=. --go_opt=module=example.com/httpdi \
  --go-grpc_out=. --go-grpc_opt=module=example.com/httpdi \
  proto/transactions.proto

# Smoke test (works the same regardless of -repo)
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo
curl http://localhost:8080/v1/transaction/00000000-0000-0000-0000-000000000001

# Tests
go test ./...                                    # unit + HTTP integration (~2.5s)
go test -race ./...                              # also catches data races in adapters
go test -tags integration ./db/mysql             # MySQL integration (needs docker compose up)
go test -v ./server/stdlib                       # verbose, one package
go test -run TestTranslatePath ./server/stdlib   # one test

# Full comparative benchmark (default: 5000 req/s for 30s per engine)
# Brings MySQL up/down automatically via trap.
chmod +x bench.sh
./bench.sh                     # 5 engines × 2 modes (~5 min, mysql repo only)
./bench.sh 10000 60s           # custom rate + duration

# Full matrix benchmark — adds the -repo dimension (transport × serialization).
# Auto-starts the dataservice in the background; same trap cleans up.
# Writes to bench-results-full/ (separate from bench.sh's output).
chmod +x bench-full.sh
./bench-full.sh                # 5 × 7 × 2 = 70 cells (~40 min)
./bench-full.sh 10000 60s      # custom rate + duration

# Re-analyze a recorded run without rerunning
vegeta report < bench-results/stdlib.bin
vegeta report -type=hist < bench-results/gin.bin
vegeta plot < bench-results/fiber.bin > new-plot.html
```

`bench.sh` requires `vegeta` on `$PATH` (`go install github.com/tsenart/vegeta@latest`) and `curl` for its healthcheck poll. It builds the binary once, then for each of the five engines: starts the server, waits for `/health`, warms up at 500 req/s for 3s, attacks at the configured rate/duration (in two modes — single + cycled), generates text reports and HTML plots, kills the server. A `summary.txt` table (10 rows) and a `combined.html` (auto-fit grid of plots) are written at the end.

There is no lint config or CI in the repo.

### Test layout

- **[app/app_test.go](app/app_test.go)** — unit tests for `App.Health` / `App.Hello` / `App.GetTransaction`. `fakeTxRepo` is a controllable `TransactionRepository` used to drive 404 / 500 / success paths without a DB. The success case round-trips the response body through `json.Unmarshal` into a `Transaction` and compares with `reflect.DeepEqual` — catches missing fields, renames, or broken nesting in one assert.
- **[server/internal/routeutil/routeutil_test.go](server/internal/routeutil/routeutil_test.go)** — table-driven test for `TranslateColonToBrace` (nested params, mid-segment colons, bare `:`).
- **[server/internal/servertest/servertest.go](server/internal/servertest/servertest.go)** — shared HTTP integration suite. Registers a `/health` and `/hello/:name` on a given `server.Server`, starts it on a port the caller picks, hits it over real HTTP, then `Shutdown`s. Each adapter test calls `servertest.RunSuite(t, New(), ":1808X")` with a unique port so packages can run in parallel.
- **[serde/serde_test.go](serde/serde_test.go)** — codec round-trip (`Marshal`→`Unmarshal` == original via `reflect.DeepEqual`) for JSON/protobuf/Avro, plus `ForAccept` content-type mapping. Uses UTC micro-aligned timestamps so the Avro `timestamp-micros` logical type round-trips exactly.
- **[cmd/dataservice/handler_test.go](cmd/dataservice/handler_test.go)** — dataservice handler tests with a `fakeTxRepo`; covers 200/404/500 via `httptest.NewRecorder`, plus content negotiation (each `Accept` → matching `Content-Type` + a body decodable by that codec). No network, no DB.
- **[db/restclient/restclient_test.go](db/restclient/restclient_test.go)** — REST client tests against `httptest.NewServer` stubs. HTTP/1.1 path: 200 round-trip, 404 → `ErrTransactionNotFound`, 5xx, URL escaping. HTTP/2 path (`TestGetByID_HTTP2`): a real h2c server (`h2c.NewHandler`) with content negotiation, asserting `r.ProtoMajor == 2` and round-tripping each codec, plus 404/5xx.
- **[db/grpcclient/grpcclient_test.go](db/grpcclient/grpcclient_test.go)** — protobuf gRPC client against a real `grpc.Server` (ephemeral port, fake `TransactionServiceServer`). 200 round-trip, `codes.NotFound` → sentinel, `codes.Internal` → wrapped error.
- **[db/grpcavroclient/grpcavroclient_test.go](db/grpcavroclient/grpcavroclient_test.go)** — Avro gRPC client against a real `grpc.Server` registering a fake `TransactionServiceAvro` (mirrors the hand-written `ServiceDesc`; `HandlerType` must be non-nil — `(*any)(nil)` — or `RegisterService` panics). Same 200/NotFound/Internal coverage, exercising the registered Avro codec over real HTTP/2.
- **[db/mysql/mysql_test.go](db/mysql/mysql_test.go)** — MySQL adapter integration test, gated by `//go:build integration`. Requires `docker compose up -d` (or another MySQL with the schema/seed). Run with `go test -tags integration ./db/mysql`. Default `go test ./...` skips it.
- Adapter ports: stdlib `:18081`, gin `:18082`, fiber `:18083`, echo `:18084`, chi `:18085`. Bump these if your machine has them busy.

## Architecture

The point of the project is the seams, not the handlers. Hexagonal: driving ports (HTTP) inbound on one side, driven ports (DB, ...) outbound on the other, application core in the middle. Keep that in mind when changing things:

- **[server/server.go](server/server.go) is the inbound (driving) port.** Framework-agnostic `Request`/`Response` structs and a `HandlerFunc(ctx, Request) Response` signature. The `Server` interface is `RegisterRoute` + `Start` + `Shutdown`. The application core depends on this package — never on Gin/Fiber/net/http types.
- **Each HTTP adapter is one file, one responsibility.** [server/stdlib/stdlib.go](server/stdlib/stdlib.go), [server/ginadapt/gin.go](server/ginadapt/gin.go), [server/fiberadapt/fiber.go](server/fiberadapt/fiber.go), [server/echoadapt/echo.go](server/echoadapt/echo.go), [server/chiadapt/chi.go](server/chiadapt/chi.go) wrap their framework and translate to/from the `server.Request`/`server.Response` shape. Handlers never see framework-specific context.
- **[app/app.go](app/app.go) is the application core.** The `App` struct holds outbound dependencies (driven ports — repositories, clients, ...) as interface fields. Its methods match the `server.HandlerFunc` signature so they can be registered directly on any `server.Server` adapter. When adding a new endpoint, add a method on `App`; when adding a new outbound dependency, add an interface field on `App` and an adapter package outside `app/`.
- **[app/transactions.go](app/transactions.go) defines a driven port.** `TransactionRepository` is the contract; `Transaction` is the domain aggregate with nested `Customer` and `CartSnapshot`. Domain types carry **only `json:` tags** — they're infrastructure-free. `ErrTransactionNotFound` is the sentinel adapters must return for missing rows so the handler can translate to 404. New driven ports follow the same shape: interface + sentinels in `app/`, implementation in a sibling top-level package.
- **[db/mysql/mysql.go](db/mysql/mysql.go) implements `TransactionRepository`** with sqlx + go-sql-driver/mysql. The JOIN query is held in `getTransactionByIDQuery`; results scan into an internal `transactionRow` DTO (flat, aliased columns, carries the `db:` tags), then `toDomain()` assembles `app.Transaction`. This keeps SQL-shape coupling inside `db/mysql/` instead of leaking into the domain. Pool tuned for the benchmark (`SetMaxOpenConns(100)`, `SetMaxIdleConns(50)`). Maps `sql.ErrNoRows` to `app.ErrTransactionNotFound`.
- **[serde/](serde/) is where all wire-format coupling lives.** The `Codec` port (`ContentType`/`Marshal`/`Unmarshal` over `app.Transaction`) keeps protobuf/Avro out of the domain — `app.Transaction` stays JSON-tags-only; the protobuf↔domain and Avro↔domain conversions and the Avro `avro:`-tagged DTO live here. `ForAccept` selects a codec from an HTTP `Accept` header. `serde.Avro` also registers a gRPC `encoding.Codec` (name `avro`) in `init()`, so importing the package is enough to make the Avro gRPC path available. The protobuf conversions (`TransactionToProto`/`TransactionFromProto`) are shared by the gRPC server, gRPC client, and the protobuf REST codec — no duplication.
- **[db/restclient/restclient.go](db/restclient/restclient.go) is the REST adapter.** Same `TransactionRepository` interface, codec-pluggable: `New` is HTTP/1.1 + JSON; `NewHTTP2(addr, codec)` forces HTTP/2 cleartext (h2c, via `golang.org/x/net/http2`) with any `serde.Codec`. It sends `Accept: codec.ContentType()` and decodes the body with the same codec. HTTP 404 maps back to `app.ErrTransactionNotFound` so the front handler logic is identical regardless of `-repo`. 5s timeout; default pooling.
- **[db/grpcclient/grpcclient.go](db/grpcclient/grpcclient.go) is the protobuf gRPC adapter.** Dials the dataservice's `TransactionService` with insecure credentials (loopback/LAN). Generated stubs in [proto/transactionspb/](proto/transactionspb/) — committed so `go build` works without protoc. `codes.NotFound` → `app.ErrTransactionNotFound`. The `ClientConn` is long-lived; close it on shutdown.
- **[db/grpcavroclient/grpcavroclient.go](db/grpcavroclient/grpcavroclient.go) is the Avro gRPC adapter.** Same transport as `grpcclient` but calls the hand-written `TransactionServiceAvro` method via `conn.Invoke(..., grpc.CallContentSubtype("avro"))`, so the gRPC framing is identical and only the serialization differs. The Avro service has no generated stubs (protobuf-generated types can't be Avro-marshalled) — the method name is a constant and the messages are the `serde` Avro DTOs. Server side is [cmd/dataservice/grpc_avro.go](cmd/dataservice/grpc_avro.go), a manual `grpc.ServiceDesc` registered on the same `grpc.Server` as the protobuf service.
- **[cmd/dataservice/](cmd/dataservice/)** is the data tier. Owns the MySQL connection (via `db/mysql`) and serves three transports over the same repository: REST/HTTP1 (`handler.go`), REST/h2c (same `mux` wrapped in `h2c.NewHandler`), and gRPC (`grpc.go` protobuf + `grpc_avro.go` Avro). The REST handler content-negotiates the body via `serde.ForAccept` (the same handler serves both REST ports). Deliberately the dataservice's REST is on stdlib `net/http` so we have a stable, framework-free floor on the back; the measurement isolates *front + protocol + serialization* rather than back-framework noise. All server impls are constructor-injected with `app.TransactionRepository`.
- **[proto/transactions.proto](proto/transactions.proto) is the protobuf gRPC wire schema.** Mirrors `app.Transaction`. `google.protobuf.Timestamp` for dates; field numbers are stable forever (proto3 wire compatibility). Regenerate stubs only when this file changes (see Common commands). The Avro wire schema is **not** here — it lives as a string in [serde/avro.go](serde/avro.go) (no codegen step).
- **`transaction` is a MySQL reserved word** — backtick it (`` `transaction` ``) in every query. The constant in `getTransactionByIDQuery` uses Go string concatenation to keep the backticks readable inside the raw-string literal.
- **[main.go](main.go) is the composition root** — wiring only, no handler logic. `newServer(engine)` picks the inbound adapter; `newRepo(kind, dsn, repoAddr)` picks the driven adapter (`mysql`/`rest`/`resth2`/`resth2-pb`/`resth2-avro`/`grpc`/`grpc-avro`) and returns a cleanup func; `app.New(txRepo)` injects it into the core. Adding a new HTTP framework = new adapter package + one case in `newServer`. Adding a new transport/serialization = new package implementing the existing port (or a new `serde.Codec` reused by the REST adapter); one case in `newRepo`.

### Database schema

DDL in [mysql-init/01-schema.sql](mysql-init/01-schema.sql), bulk seed in [mysql-init/02-seed.sql](mysql-init/02-seed.sql) — both run once when the data directory is empty.

- `customer (id CHAR(36) PK, nome VARCHAR(100), create_date DATETIME)`.
- `` `transaction` (id CHAR(36) PK, value DOUBLE, customer_id CHAR(36) FK → customer, create_date DATETIME) `` — backticked because `TRANSACTION` is a reserved word.
- `cart_snapshot (id CHAR(36) PK, transaction_id CHAR(36) FK → transaction with UNIQUE, create_date DATETIME)` — 1:1 with transactions via UNIQUE.

**Seed scale (matters for realistic benchmark)**: 50 000 customers, 150 000 transactions (3 per customer), 150 000 cart_snapshots (1:1 with transactions). Generated via recursive CTEs (`cte_max_recursion_depth = 200000`) so seed runs in seconds, not minutes. Deterministic IDs: customer N is `11111111-…-LPAD(N,12,'0')`, transaction N is `00000000-…-LPAD(N,12,'0')`, cart_snapshot N is `22222222-…-LPAD(N,12,'0')`. Transaction N belongs to customer `FLOOR((N-1)/3)+1` — so transactions 1/2/3 are all on customer 1. Customer name is `"Customer #N"`.

The endpoint `/v1/transaction/:id` runs `transaction ⋈ customer ⋈ cart_snapshot` (INNER JOIN both). The seeded IDs `…001` line up across all three tables — that's the row the benchmark and integration test hit. Schema/seed changes require wiping `mysql-data/` to re-run the init scripts. First boot now takes ~30–60s because of the seed (bench.sh's `wait_for_mysql` is 5min).

### Adapter quirks worth knowing

- **Fiber uses fasthttp, not `net/http`.** Its request body buffer is reused across requests, so [server/fiberadapt/fiber.go](server/fiberadapt/fiber.go) must `copy()` `c.Body()` before handing it to the handler — otherwise concurrent requests race. Do not "optimize" that copy away.
- **Path-param translation is shared.** Routes are declared canonically in `:name` form (Gin/Fiber/Echo's convention). stdlib and chi expect `{name}` — both translate at register-time via [`server/internal/routeutil.TranslateColonToBrace`](server/internal/routeutil/routeutil.go) and then read values via their respective APIs (`r.PathValue(...)` for stdlib, `chi.URLParam(r, ...)` for chi). If you add another `{name}`-style adapter, call routeutil; if it's `:name`-style, pass the path through unchanged.
- **Shutdown is not symmetric.** stdlib, Gin, Echo, and chi all wrap an `http.Server` and call `Shutdown(ctx)` cleanly. Fiber's `app.Shutdown()` ignores the context entirely ([server/fiberadapt/fiber.go:62](server/fiberadapt/fiber.go#L62)).
- **Noise suppression for benchmarks.** Gin uses `gin.SetMode(gin.ReleaseMode)`; Echo sets `HideBanner = true` and `HidePort = true`. stdlib/chi/fiber are quiet by default.

### Benchmark methodology

Two scripts answer two different questions:

- **[bench.sh](bench.sh)** — *Q1: which framework is fastest?* Holds `-repo mysql` (in-process). Varies engine × mode (5 × 2 = 10 cells). ~5 min. Use this for quick iteration.
- **[bench-full.sh](bench-full.sh)** — *Q1 + Q2 together: how does each framework behave under each (transport, serialization)?* Full matrix engine × backend × mode (5 × 7 × 2 = 70 cells). ~40 min. The 7 backends are `mysql`, `rest` (h1+json), `resth2`/`resth2-pb`/`resth2-avro` (h2c + json/pb/avro), `grpc` (pb), `grpc-avro`. Output is grouped per engine — 5 sub-tables of 14 rows each, so you can read "for engine X" and compare transports and serializations directly: HTTP/1.1 vs HTTP/2 (`rest` vs `resth2`), HTTP/2 vs gRPC (`resth2-pb` vs `grpc`), protobuf vs Avro (`resth2-pb` vs `resth2-avro`; `grpc` vs `grpc-avro`). Writes to `bench-results-full/` (separate dir; targets regenerated). bench-full spins up `cmd/dataservice` (all three ports) in the background and tears it down on EXIT. The `BACKENDS` array is the trim point if 70 cells is too long — drop `mysql` or `resth2`.

Both scripts run **two attack modes per engine** (or per engine×backend in bench-full), back-to-back without restarting the server:

- **`single`** — every request hits the same row (`…000000000001`). The row stays pinned in the InnoDB buffer pool → measures framework + sqlx + driver + MySQL hot path with zero cache variance.
- **`cycled`** — `vegeta attack -targets=bench-results/targets-cycled.txt -lazy` cycles through `CYCLED_COUNT` (150 000) distinct seeded transaction IDs. Exercises the PK index + JOIN paths across the dataset. The working set still fits in the default 128 MB buffer pool, so it's mostly cache hits after the first pass — what differs from `single` is mostly index-walk cost and `JOIN` planner work, not disk I/O.

Each mode does its own 3s/500 req/s warmup before measurement. The warmup uses the *same source* as the attack (single → SINGLE_TARGET, cycled → targets file), so the Go runtime and any per-target setup costs are paid before the measurement starts. The warmup exists because earlier runs had visibly skewed first-engine results.

Results files: `bench-results/<engine>-<mode>.{bin,txt,html}` (5 engines × 2 modes = 10 trios), plus `summary.txt` (10-row table) and `combined.html` (auto-fit grid of all plots). The `targets-cycled.txt` file (~12 MB) is regenerated each run.
