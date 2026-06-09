# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

HTTP framework benchmark / hexagonal-architecture proof of concept in Go. The front service ([main.go](main.go)) serves the same routes (`/health`, `/hello/:name`, `/v1/transaction/:transaction_id`) through one of five swappable HTTP backends — stdlib `net/http`, Gin, Fiber, Echo, or chi — selected by the `-engine` flag. The transaction endpoint can run two ways selected by `-repo`:

- `mysql` (default) — in-process: the front talks to MySQL directly via sqlx + `db/mysql`.
- `rest` — over the network: the front delegates to [cmd/dataservice](cmd/dataservice/) (a separate binary that owns MySQL) via HTTP+JSON.

This lets us measure the same `/v1/transaction/:id` aggregate (`transaction` ⋈ `customer` ⋈ `cart_snapshot`) under both in-process and microservice topologies. `bench.sh` runs vegeta against each engine in turn and produces a comparative report.

The README is in Portuguese (`readme.MD`).

## Layout

```
main.go                              # front service: composition root, picks engine + repo
cmd/dataservice/main.go              # data service: owns MySQL, exposes GET /v1/transaction/{id}
cmd/dataservice/handler.go           #   handler logic, constructor-injected with a TransactionRepository
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
db/restclient/restclient.go          # REST client adapter (front → dataservice over HTTP+JSON)
docker-compose.yml                   # MySQL 8.0, bind-mounted data + init scripts
mysql-init/01-schema.sql             # DDL only — runs once on first init
mysql-init/02-seed.sql               # bulk seed via recursive CTE (50k/150k/150k rows)
bench.sh                             # vegeta-based comparative benchmark
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

# Front service — REST repo (the front delegates to the dataservice)
go run ./cmd/dataservice -addr :9090           # terminal A: dataservice owns MySQL
go run main.go -repo rest -repo-addr http://localhost:9090   # terminal B: front

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
./bench.sh
./bench.sh 10000 60s           # custom rate + duration

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
- **[cmd/dataservice/handler_test.go](cmd/dataservice/handler_test.go)** — dataservice handler tests with a `fakeTxRepo`; covers 200/404/500 via `httptest.NewRecorder`. No network, no DB.
- **[db/restclient/restclient_test.go](db/restclient/restclient_test.go)** — REST client tests against `httptest.NewServer` stubs. Covers 200 round-trip (compared with `reflect.DeepEqual`), 404 → `ErrTransactionNotFound`, 5xx → wrapped non-sentinel error, and URL escaping of IDs with special characters.
- **[db/mysql/mysql_test.go](db/mysql/mysql_test.go)** — MySQL adapter integration test, gated by `//go:build integration`. Requires `docker compose up -d` (or another MySQL with the schema/seed). Run with `go test -tags integration ./db/mysql`. Default `go test ./...` skips it.
- Adapter ports: stdlib `:18081`, gin `:18082`, fiber `:18083`, echo `:18084`, chi `:18085`. Bump these if your machine has them busy.

## Architecture

The point of the project is the seams, not the handlers. Hexagonal: driving ports (HTTP) inbound on one side, driven ports (DB, ...) outbound on the other, application core in the middle. Keep that in mind when changing things:

- **[server/server.go](server/server.go) is the inbound (driving) port.** Framework-agnostic `Request`/`Response` structs and a `HandlerFunc(ctx, Request) Response` signature. The `Server` interface is `RegisterRoute` + `Start` + `Shutdown`. The application core depends on this package — never on Gin/Fiber/net/http types.
- **Each HTTP adapter is one file, one responsibility.** [server/stdlib/stdlib.go](server/stdlib/stdlib.go), [server/ginadapt/gin.go](server/ginadapt/gin.go), [server/fiberadapt/fiber.go](server/fiberadapt/fiber.go), [server/echoadapt/echo.go](server/echoadapt/echo.go), [server/chiadapt/chi.go](server/chiadapt/chi.go) wrap their framework and translate to/from the `server.Request`/`server.Response` shape. Handlers never see framework-specific context.
- **[app/app.go](app/app.go) is the application core.** The `App` struct holds outbound dependencies (driven ports — repositories, clients, ...) as interface fields. Its methods match the `server.HandlerFunc` signature so they can be registered directly on any `server.Server` adapter. When adding a new endpoint, add a method on `App`; when adding a new outbound dependency, add an interface field on `App` and an adapter package outside `app/`.
- **[app/transactions.go](app/transactions.go) defines a driven port.** `TransactionRepository` is the contract; `Transaction` is the domain aggregate with nested `Customer` and `CartSnapshot`. Domain types carry **only `json:` tags** — they're infrastructure-free. `ErrTransactionNotFound` is the sentinel adapters must return for missing rows so the handler can translate to 404. New driven ports follow the same shape: interface + sentinels in `app/`, implementation in a sibling top-level package.
- **[db/mysql/mysql.go](db/mysql/mysql.go) implements `TransactionRepository`** with sqlx + go-sql-driver/mysql. The JOIN query is held in `getTransactionByIDQuery`; results scan into an internal `transactionRow` DTO (flat, aliased columns, carries the `db:` tags), then `toDomain()` assembles `app.Transaction`. This keeps SQL-shape coupling inside `db/mysql/` instead of leaking into the domain. Pool tuned for the benchmark (`SetMaxOpenConns(100)`, `SetMaxIdleConns(50)`). Maps `sql.ErrNoRows` to `app.ErrTransactionNotFound`.
- **[db/restclient/restclient.go](db/restclient/restclient.go) is the alternate adapter.** Same `TransactionRepository` interface, but the row comes from the dataservice over HTTP+JSON instead of MySQL. HTTP 404 maps back to `app.ErrTransactionNotFound` so the front handler logic is identical regardless of `-repo`. `http.Client` has a 5s timeout; no pooling beyond Go's default transport (good enough for the benchmark).
- **[cmd/dataservice/](cmd/dataservice/)** is the data tier. Owns the MySQL connection (via `db/mysql`), exposes `GET /v1/transaction/{id}` on stdlib `net/http` (deliberately — we want a stable framework-free floor on the back so the measurement isolates *front + protocol + back*, not back-framework noise). `handler.go` is constructor-injected with `app.TransactionRepository`, so tests can drive it with a fake.
- **`transaction` is a MySQL reserved word** — backtick it (`` `transaction` ``) in every query. The constant in `getTransactionByIDQuery` uses Go string concatenation to keep the backticks readable inside the raw-string literal.
- **[main.go](main.go) is the composition root** — wiring only, no handler logic. `newServer(engine)` picks the inbound adapter; `newRepo(kind, dsn, repoAddr)` picks the driven adapter (`mysql` or `rest`) and returns a cleanup func; `app.New(txRepo)` injects it into the core. Adding a new HTTP framework = new adapter package + one case in `newServer`. Adding a new driven adapter (Postgres, in-memory, gRPC client) = new package implementing the existing port; one case in `newRepo`.

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

`bench.sh` runs **two attack modes per engine**, back-to-back without restarting the server:

- **`single`** — every request hits the same row (`…000000000001`). The row stays pinned in the InnoDB buffer pool → measures framework + sqlx + driver + MySQL hot path with zero cache variance.
- **`cycled`** — `vegeta attack -targets=bench-results/targets-cycled.txt -lazy` cycles through `CYCLED_COUNT` (150 000) distinct seeded transaction IDs. Exercises the PK index + JOIN paths across the dataset. The working set still fits in the default 128 MB buffer pool, so it's mostly cache hits after the first pass — what differs from `single` is mostly index-walk cost and `JOIN` planner work, not disk I/O.

Each mode does its own 3s/500 req/s warmup before measurement. The warmup uses the *same source* as the attack (single → SINGLE_TARGET, cycled → targets file), so the Go runtime and any per-target setup costs are paid before the measurement starts. The warmup exists because earlier runs had visibly skewed first-engine results.

Results files: `bench-results/<engine>-<mode>.{bin,txt,html}` (5 engines × 2 modes = 10 trios), plus `summary.txt` (10-row table) and `combined.html` (auto-fit grid of all plots). The `targets-cycled.txt` file (~12 MB) is regenerated each run.
