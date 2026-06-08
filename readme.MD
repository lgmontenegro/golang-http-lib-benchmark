# HTTP Dependency Inversion Benchmark

Prova de conceito que demonstra inversão de dependência em Go para trocar entre
três frameworks HTTP (stdlib, Gin, Fiber) e comparar as suas performances sob
carga constante com vegeta.

## Requisitos

- Go 1.22+
- Docker + Docker Compose (para o MySQL)
- curl (para o healthcheck do script)
- vegeta

```bash
go install github.com/tsenart/vegeta@latest
```

## Estrutura do projecto

```
go-http-di/
├── main.go                       # Composition root (apenas wiring)
├── bench.sh                      # Script de benchmark comparativo
├── docker-compose.yml            # MySQL 8.0 com bind mount
├── app/
│   ├── app.go                    # Core: App struct + handlers
│   └── transactions.go           # Driven port: Transaction + TransactionRepository + sentinel
├── server/
│   ├── server.go                 # Driving port: interface Server + tipos Request/Response
│   ├── stdlib/stdlib.go          # Adapter net/http
│   ├── ginadapt/gin.go           # Adapter Gin
│   └── fiberadapt/fiber.go       # Adapter Fiber
├── db/
│   └── mysql/mysql.go            # Adapter MySQL (sqlx) para TransactionRepository
├── mysql-init/
│   ├── 01-schema.sql             # DDL das 3 tabelas; corre só no 1º boot
│   └── 02-seed.sql               # Bulk seed via CTE recursiva (50k/150k/150k)
├── mysql-data/                   # Volume bind do MySQL (gitignored)
└── bench-results/                # Gerado após benchmark (gitignored)
    ├── stdlib.bin / .txt / .html
    ├── gin.bin / .txt / .html
    ├── fiber.bin / .txt / .html
    ├── summary.txt
    └── combined.html
```

## Inicializar o módulo

```bash
go mod init example.com/httpdi
go mod tidy
```

## Subir o MySQL

```bash
docker compose up -d              # MySQL 8.0 em :3306, data em ./mysql-data
docker compose logs -f mysql      # acompanhar primeiro boot (~10-20s)
docker compose down               # parar (mantém dados)
rm -rf mysql-data                 # wipe total + re-aplicar 01-schema.sql no próximo up
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

## Executar o servidor manualmente

```bash
# stdlib (default), conecta a MySQL no DSN default
go run main.go

# Gin
go run main.go -engine gin

# Fiber
go run main.go -engine fiber

# Porta custom + DSN custom (também aceita env DB_DSN)
go run main.go -engine gin -addr :3000 -dsn "user:pass@tcp(host:3306)/db?parseTime=true"
```

Testar (smoke):

```bash
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo
curl http://localhost:8080/v1/transaction/00000000-0000-0000-0000-000000000001
```

## Executar a suite de testes

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
  regista rotas, sobe cada adapter em portos dedicados (`:18081/2/3`), faz HTTP
  real e verifica `/health`, `/hello/:name` e 404. Cada adapter tem um
  `_test.go` mínimo que invoca a suite.
- **Integração MySQL** — `db/mysql/mysql_test.go` está atrás da build tag
  `integration` (precisa do container UP). Excluído de `go test ./...` por
  default.

## Executar o benchmark

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
5. Para cada engine: inicia o servidor, espera o healthcheck, corre os
   dois modos (single, cycled), pára o servidor.
6. Gera relatório texto + plot HTML por (engine, mode) — 6 relatórios.
7. Imprime a tabela comparativa de 6 linhas e regista `summary.txt` +
   `combined.html` (3 cols × 2 modes).
8. `trap` no `EXIT` faz `docker compose down` mesmo em erro / Ctrl+C.

## Analisar resultados

Os ficheiros `.bin` são o formato nativo do vegeta e permitem re-análise sem
repetir o teste:

```bash
# Re-gerar relatório texto
vegeta report < bench-results/stdlib.bin

# Histograma de latência
vegeta report -type=hist < bench-results/gin.bin

# Exportar para JSON (integração com jq, scripts, etc.)
vegeta report -type=json < bench-results/fiber.bin

# Gerar novo plot
vegeta plot < bench-results/stdlib.bin > novo-plot.html
```

O relatório combinado (`bench-results/combined.html`) mostra os três plots
lado a lado no browser.

## Métricas relevantes

| Métrica     | O que indica                                                         |
|-------------|----------------------------------------------------------------------|
| Throughput  | Requisições processadas por segundo (deve igualar ou aproximar o rate alvo) |
| Latency p50 | Latência mediana — metade dos pedidos respondeu abaixo deste valor   |
| Latency p95 | Tail latency — 5% dos pedidos ficaram acima deste valor              |
| Latency p99 | Outliers — 1% dos pedidos mais lentos                                |
| Success     | Percentagem de respostas com status 2xx/3xx                          |

Se o throughput ficar abaixo do rate alvo, o servidor está a saturar nessa
taxa. Nesse caso, compara o ponto de saturação entre os três engines.

## Resultado exemplo

### Corrida actual — `/v1/transaction/:id` com JOIN MySQL

macOS arm64, 5000 req/s × 30s, dois modos por engine (executados back-to-back):

| Engine  | Mode    | Throughput | p50       | p95       | p99           | Success |
|---------|---------|------------|-----------|-----------|---------------|---------|
| stdlib  | single  | 5000.0/s   | 422.3µs   | 508.1µs   | 651.8µs       | 100.00% |
| stdlib  | cycled  | 5000.0/s   | 420.2µs   | 516.3µs   | 706.0µs       | 100.00% |
| gin     | single  | 5000.0/s   | 417.6µs   | 507.1µs   | 717.2µs       | 100.00% |
| gin     | cycled  | 5000.0/s   | 413.8µs   | 521.4µs   | **4.58ms** ⚠  | 100.00% |
| fiber   | single  | 5000.0/s   | **353.6µs** | **431.3µs** | **605.7µs** | 100.00% |
| fiber   | cycled  | 5000.0/s   | 356.0µs   | 432.2µs   | 598.5µs       | 100.00% |

Leituras:

- **Throughput** — os três aguentam 5000 req/s com 100% sucesso. Ninguém
  satura aqui; é preciso subir a taxa para distinguir limites máximos
  (`./bench.sh 20000 30s`, etc.).
- **Fiber lidera** consistentemente em latência (~17% melhor p50 vs stdlib/gin),
  mesmo padrão da baseline sem DB.
- **`single` vs `cycled` é praticamente igual** em stdlib e fiber — confirma
  que o working set (~15 MB de dados ativos) cabe no buffer pool default do
  InnoDB (128 MB). O custo do JOIN é dominado pelo planner + index walk, não
  por disco.
- **Outlier do Gin em `cycled` p99 (4.58 ms)** — ~6× a média e ~9× o que o Gin
  faz em `single`. Valor isolado dos 9 cells restantes; provavelmente uma
  pausa de GC ou contention pontual. Vale uma re-execução para confirmar se é
  repetível.
- **Comparado à baseline** (próxima secção), JOIN + sqlx + MySQL adiciona
  ~380 µs de p50 — a maior parte da latência agora é o caminho até à DB,
  não framework.

### Baseline anterior — `/health` estático (referência)

Corrida em macOS arm64 a 5000 req/s × 30s no endpoint `/health` (JSON
hardcoded, sem MySQL na cadeia):

| Engine | Throughput | p50      | p95      | p99       | Success |
|--------|------------|----------|----------|-----------|---------|
| stdlib | 5000.03/s  | 39.7µs   | 71.4µs   | 134.1µs   | 100.00% |
| gin    | 5000.02/s  | 39.4µs   | 66.9µs   | 124.7µs   | 100.00% |
| fiber  | 5000.04/s  | 32.0µs   | 57.7µs   | 93.7µs    | 100.00% |

A baseline servia para isolar o custo puro de framework. Comparando linha a
linha com a corrida actual: p50 cresce ~10× (~40 µs → ~400 µs), confirmando
que ~90% da latência hoje é DB + driver e ~10% framework. A ordem entre
engines mantém-se.

## Notas

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
- O `mysql-init/01-schema.sql` só corre quando o MySQL inicializa um data dir
  vazio. Para reaplicar (após mudar o schema ou seed): `docker compose down &&
  rm -rf mysql-data && docker compose up -d`.
- O adapter MySQL usa `parseTime=true` no DSN para que `time.Time` faça scan
  da coluna `DATETIME`. Sem isso, `created_at` vem zero e o teste de integração
  apanha a omissão.
- O warmup antes de cada medição evita que o cold start do Go (GC, alocação
  inicial de goroutines) polua os resultados.
- Para resultados consistentes, fecha aplicações que consumam CPU/rede e corre
  várias iterações.