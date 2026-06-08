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
│   └── 01-schema.sql             # DDL + seed; corre só no primeiro boot do MySQL
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
password `httpdipass`, database `httpdi`. O ficheiro `mysql-init/01-schema.sql`
cria a tabela `transactions` e insere 10 linhas com IDs determinísticos
`00000000-0000-0000-0000-000000000001` a `…010`.

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

O script:

1. Verifica `vegeta` e `docker` no PATH.
2. `docker compose up -d` e espera o MySQL ficar `healthy`.
3. Compila o binário uma vez (`go build`).
4. Para cada engine: inicia o servidor, espera o healthcheck, warmup de
   500 req/s durante 3s para estabilizar o GC, e ataca a taxa/duração
   configuradas contra `GET /v1/transaction/00000000-0000-0000-0000-000000000001`.
5. Gera relatório texto + plot HTML por engine, para o servidor.
6. Imprime a tabela comparativa e regista `summary.txt` + `combined.html`.
7. `trap` no `EXIT` faz `docker compose down` mesmo em erro / Ctrl+C.

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

## Resultado exemplo (baseline anterior, sem DB)

Corrida em macOS arm64 a 5000 req/s × 30s no endpoint `/health` (JSON estático,
sem MySQL):

| Engine | Throughput | p50      | p95      | p99       | Success |
|--------|------------|----------|----------|-----------|---------|
| stdlib | 5000.03/s  | 39.7µs   | 71.4µs   | 134.1µs   | 100.00% |
| gin    | 5000.02/s  | 39.4µs   | 66.9µs   | 124.7µs   | 100.00% |
| fiber  | 5000.04/s  | 32.0µs   | 57.7µs   | 93.7µs    | 100.00% |

Esta tabela serve de baseline para comparar com o novo target
`GET /v1/transaction/:id`, que adiciona sqlx + driver + round-trip MySQL ao hot
path. A 5000 req/s a base era 100% de sucesso sem saturar — o overhead da DB
deve aparecer na latency, não no throughput.

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