# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

HTTP framework benchmark / dependency-inversion proof of concept in Go. A single CLI binary serves the same two routes (`/health`, `/hello/:name`) through one of three swappable backends — stdlib `net/http`, Gin, or Fiber — selected by the `-engine` flag. `bench.sh` runs vegeta against each engine in turn and produces a comparative report.

The README is in Portuguese (`readme.MD`).

## Layout

```
main.go                              # composition root, handlers, CLI
server/server.go                     # package server — Request/Response/HandlerFunc/Server contract
server/stdlib/stdlib.go              # package stdlib — net/http adapter
server/ginadapt/gin.go               # package ginadapt — Gin adapter
server/fiberadapt/fiber.go           # package fiberadapt — Fiber adapter
bench.sh                             # vegeta-based comparative benchmark
```

Module path: `example.com/httpdi`.

## Common commands

```bash
# Run a single engine
go run main.go                 # stdlib (default)
go run main.go -engine gin
go run main.go -engine fiber
go run main.go -engine gin -addr :3000

# Smoke test
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo

# Full comparative benchmark (default: 5000 req/s for 30s per engine)
chmod +x bench.sh
./bench.sh
./bench.sh 10000 60s           # custom rate + duration

# Re-analyze a recorded run without rerunning
vegeta report < bench-results/stdlib.bin
vegeta report -type=hist < bench-results/gin.bin
vegeta plot < bench-results/fiber.bin > new-plot.html
```

`bench.sh` requires `vegeta` on `$PATH` (`go install github.com/tsenart/vegeta@latest`) and `curl` for its healthcheck poll. It builds the binary once, then for each engine: starts the server, waits for `/health`, warms up at 500 req/s for 3s, attacks at the configured rate/duration, generates a text report and HTML plot, kills the server. A `summary.txt` table and a `combined.html` (three plots side-by-side) are written at the end.

There is no test suite, lint config, or CI in the repo.

## Architecture

The point of the project is the seam, not the handlers. Keep that in mind when changing things:

- **[server/server.go](server/server.go) defines the contract.** Framework-agnostic `Request`/`Response` structs and a `HandlerFunc(ctx, Request) Response` signature. The `Server` interface is `RegisterRoute` + `Start` + `Shutdown`. The domain (handlers in [main.go](main.go)) depends only on this package — never on Gin/Fiber/net/http types.
- **Each adapter is one file, one responsibility.** [server/stdlib/stdlib.go](server/stdlib/stdlib.go), [server/ginadapt/gin.go](server/ginadapt/gin.go), [server/fiberadapt/fiber.go](server/fiberadapt/fiber.go) wrap their framework and translate to/from the `server.Request`/`server.Response` shape. Handlers never see framework-specific context.
- **[main.go](main.go) is the composition root.** `newServer(engine)` is the only place that knows about concrete adapters. Adding a new framework = new adapter package + one case in this switch.

### Adapter quirks worth knowing

- **Fiber uses fasthttp, not `net/http`.** Its request body buffer is reused across requests, so [server/fiberadapt/fiber.go](server/fiberadapt/fiber.go) must `copy()` `c.Body()` before handing it to the handler — otherwise concurrent requests race. Do not "optimize" that copy away.
- **stdlib uses Go 1.22+ pattern routing**, which expects `{name}` wildcards — *not* `:name` (Gin/Fiber's convention). The adapter normalises by translating `:name` → `{name}` in `translatePath` ([server/stdlib/stdlib.go](server/stdlib/stdlib.go)) at register-time, then pulls values via `r.PathValue(...)` into `server.Request.Params`. The route string in [main.go](main.go) stays in `:name` form so all three adapters share it. If you add another adapter using a different param syntax, follow the same pattern: keep the canonical form `:name` and translate inside the adapter.
- **Shutdown is not symmetric.** stdlib and Gin call `http.Server.Shutdown(ctx)`. Fiber's `app.Shutdown()` ignores the context entirely ([server/fiberadapt/fiber.go:62](server/fiberadapt/fiber.go#L62)).
- **Gin runs in release mode** (`gin.SetMode(gin.ReleaseMode)` in `New()`), so debug logging is suppressed during benchmarks.

### Benchmark methodology

`bench.sh` deliberately warms up before each measurement to avoid polluting results with Go's cold-start (GC, goroutine pool warmup). The 3-second 500 req/s warmup is per engine. If you change the warmup or remove it, document why — the warmup exists because earlier runs had visibly skewed first-engine results.
