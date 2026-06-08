# HTTP Dependency Inversion Benchmark

Prova de conceito que demonstra inversão de dependência em Go para trocar entre
três frameworks HTTP (stdlib, Gin, Fiber) e comparar as suas performances sob
carga constante com vegeta.

## Requisitos

- Go 1.22+
- curl (para o healthcheck do script)
- vegeta

```bash
go install github.com/tsenart/vegeta@latest
```

## Estrutura do projecto

```
go-http-di/
├── main.go                      # Composition root com factory e handlers
├── bench.sh                     # Script de benchmark comparativo
├── server/
│   ├── server.go                # Interface Server + tipos Request/Response
│   ├── stdlib/stdlib.go         # Adapter net/http
│   ├── ginadapt/gin.go          # Adapter Gin
│   └── fiberadapt/fiber.go      # Adapter Fiber
└── bench-results/               # Gerado após benchmark
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

## Executar o servidor manualmente

```bash
# stdlib (default)
go run main.go

# Gin
go run main.go -engine gin

# Fiber
go run main.go -engine fiber

# Porta custom
go run main.go -engine gin -addr :3000
```

Testar:

```bash
curl http://localhost:8080/health
curl http://localhost:8080/hello/leonardo
```

## Executar o benchmark

```bash
chmod +x bench.sh

# Default: 5000 req/s durante 30s por engine
./bench.sh

# Custom: 10000 req/s durante 60s
./bench.sh 10000 60s
```

O script executa sequencialmente para cada engine:

1. Compila o binário uma vez (`go build`)
2. Inicia o servidor e aguarda o healthcheck responder
3. Warmup de 500 req/s durante 3s para estabilizar o GC
4. Medição real na taxa e duração configuradas
5. Gera relatório texto, plot HTML e para o servidor
6. Após as 3 sessões, imprime a tabela comparativa

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

Corrida em macOS arm64 a 5000 req/s × 30s no endpoint `/health`:

| Engine | Throughput | p50      | p95      | p99       | Success |
|--------|------------|----------|----------|-----------|---------|
| stdlib | 5000.03/s  | 39.7µs   | 71.4µs   | 134.1µs   | 100.00% |
| gin    | 5000.02/s  | 39.4µs   | 66.9µs   | 124.7µs   | 100.00% |
| fiber  | 5000.04/s  | 32.0µs   | 57.7µs   | 93.7µs    | 100.00% |

A 5000 req/s nenhum engine satura — todos entregam 100%. Para diferenciar a
sério, sobe a taxa (`./bench.sh 50000 30s` ou mais) até algum dos engines
começar a perder throughput ou success ratio.

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
- O warmup antes de cada medição evita que o cold start do Go (GC, alocação
  inicial de goroutines) polua os resultados.
- Para resultados consistentes, fecha aplicações que consumam CPU/rede e corre
  várias iterações.