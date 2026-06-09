# HTTP Dependency Inversion Benchmark

[English](#english) · [Português](#português)

---

## English

A proof of concept demonstrating dependency inversion in Go to swap between
five HTTP frameworks (stdlib, Gin, Fiber, Echo, chi) and compare their
performance under steady load with vegeta.

The front service can resolve the transaction aggregate in two ways
selected by the `-repo` flag:

- **`mysql`** *(default)* — in-process: front talks to MySQL directly via
  sqlx.
- **`rest`** — over the network: front delegates to a separate
  **dataservice** (`cmd/dataservice`) over HTTP+JSON. The dataservice owns
  the MySQL connection.

Same shape, same JSON, same `app.TransactionRepository` interface — only
the adapter differs. The benchmark covers the full path: framework + (sqlx
or REST hop) + MySQL.

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
│   └── dataservice/              # Data service: owns MySQL, exposes REST
│       ├── main.go
│       ├── handler.go
│       └── handler_test.go
├── bench.sh                      # Comparative benchmark script
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
│   └── restclient/restclient.go  # REST client adapter (front → dataservice)
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

**REST microservice** (front delegates to the dataservice; two terminals):

```bash
# Terminal A: data tier (owns MySQL, REST on :9090)
go run ./cmd/dataservice -addr :9090

# Terminal B: front (any engine, talks to the dataservice)
go run main.go -engine fiber -repo rest -repo-addr http://localhost:9090
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

```bash
chmod +x bench.sh

# Default: 5000 req/s for 30s per engine
./bench.sh

# Custom: 10000 req/s for 60s
./bench.sh 10000 60s
```

The script runs **two attack modes per engine**, back-to-back (without
restarting the server between them):

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
- **Compared to the baseline** (next section), JOIN + sqlx + MySQL adds
  ~310–390 µs of constant p50 — DB+driver account for ~90% of today's
  latency, framework ≤10%.

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

---

## Português

Prova de conceito que demonstra inversão de dependência em Go para trocar
entre cinco frameworks HTTP (stdlib, Gin, Fiber, Echo, chi) e comparar as
suas performances sob carga constante com vegeta.

O serviço front pode resolver o agregado de duas formas, selecionadas
pela flag `-repo`:

- **`mysql`** *(default)* — in-process: o front fala com o MySQL
  directamente via sqlx.
- **`rest`** — sobre a rede: o front delega num **dataservice** separado
  (`cmd/dataservice`) por HTTP+JSON. O dataservice é o dono da ligação ao
  MySQL.

Mesmo shape, mesmo JSON, mesma interface `app.TransactionRepository` —
só muda o adapter. O benchmark cobre o caminho completo: framework +
(sqlx ou hop REST) + MySQL.

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
│   └── dataservice/              # Serviço de dados: dono do MySQL, expõe REST
│       ├── main.go
│       ├── handler.go
│       └── handler_test.go
├── bench.sh                      # Script de benchmark comparativo
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
│   └── restclient/restclient.go  # Adapter REST client (front → dataservice)
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

**Microserviço REST** (front delega no dataservice; dois terminais):

```bash
# Terminal A: serviço de dados (dono do MySQL, REST em :9090)
go run ./cmd/dataservice -addr :9090

# Terminal B: front (qualquer engine, fala com o dataservice)
go run main.go -engine fiber -repo rest -repo-addr http://localhost:9090
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

```bash
chmod +x bench.sh

# Default: 5000 req/s durante 30s por engine
./bench.sh

# Custom: 10000 req/s durante 60s
./bench.sh 10000 60s
```

O script corre **dois modos por engine**, back-to-back (sem reiniciar o
servidor entre eles):

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
- **Comparado à baseline** (próxima secção), JOIN + sqlx + MySQL adiciona
  ~310–390 µs de p50 constantes — DB+driver representam ~90% da latência
  hoje, framework ≤10%.

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
