#!/usr/bin/env bash
# bench.sh — benchmark comparativo: stdlib vs gin vs fiber vs echo vs chi
# Requisitos: go, vegeta (go install github.com/tsenart/vegeta@latest), docker
# Uso: ./bench.sh [rate] [duration]
#   rate     — requisições por segundo (default: 5000)
#   duration — duração de cada sessão (default: 30s)

set -euo pipefail

RATE="${1:-5000}"
DURATION="${2:-30s}"
ADDR=":8080"
RESULTS_DIR="bench-results"
ENGINES=("stdlib" "gin" "fiber" "echo" "chi")
# Two attack modes, run back-to-back per engine:
#   single — same row every request → InnoDB buffer-pool hit, measures
#            framework + sqlx + driver + MySQL hot path.
#   cycled — cycle through CYCLED_COUNT distinct seeded IDs → exercises
#            the PK index + JOINs across the dataset (still mostly cache
#            since the working set fits in the buffer pool).
MODES=("single" "cycled")
BENCH_TX_ID="00000000-0000-0000-0000-000000000001"
SINGLE_TARGET="GET http://localhost:${ADDR#:}/v1/transaction/${BENCH_TX_ID}"
CYCLED_COUNT=150000
TARGETS_FILE="$RESULTS_DIR/targets-cycled.txt"
WARMUP_RATE=500
WARMUP_DURATION="3s"
MYSQL_CONTAINER="httpdi-mysql"

command -v vegeta >/dev/null 2>&1 || {
    echo "erro: vegeta não encontrado"
    echo "  go install github.com/tsenart/vegeta@latest"
    exit 1
}

command -v docker >/dev/null 2>&1 || {
    echo "erro: docker não encontrado"
    exit 1
}

wait_for_mysql() {
    # First boot runs 01-schema.sql + 02-seed.sql (~150k INSERTs via
    # recursive CTE) before mysqld becomes healthy. Allow up to 5min so
    # an empty mysql-data still works on slower machines. Subsequent boots
    # skip init and hit healthy in ~5s.
    local retries=300
    while [ "$(docker inspect -f '{{.State.Health.Status}}' "$MYSQL_CONTAINER" 2>/dev/null)" != "healthy" ]; do
        retries=$((retries - 1))
        if [ "$retries" -le 0 ]; then
            echo "erro: mysql não ficou healthy após 300s"
            exit 1
        fi
        sleep 1
    done
}

# trap garante que paramos o stack mesmo em caso de erro ou Ctrl+C.
trap 'echo ""; echo "→ docker compose down"; docker compose down >/dev/null 2>&1 || true' EXIT

echo "→ docker compose up -d"
docker compose up -d
wait_for_mysql
echo "✓ mysql healthy"

go build -o httpdi-bench .

rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR"

wait_for_server() {
    local retries=30
    while ! curl -sf "http://localhost:${ADDR#:}/health" >/dev/null 2>&1; do
        retries=$((retries - 1))
        if [ "$retries" -le 0 ]; then
            echo "erro: servidor não respondeu após 30 tentativas"
            exit 1
        fi
        sleep 0.1
    done
}

stop_server() {
    local pid="$1"
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    sleep 1
}

# Generate the cycled targets file once at startup. CYCLED_COUNT seeded
# transaction IDs as vegeta HTTP targets (one per line). awk is fast
# enough for 150k rows (~12MB, sub-second).
generate_cycled_targets() {
    seq 1 "$CYCLED_COUNT" | awk -v addr="${ADDR#:}" '{
        printf "GET http://localhost:%s/v1/transaction/00000000-0000-0000-0000-%012d\n", addr, $1
    }' > "$TARGETS_FILE"
}

run_attack() {
    local engine="$1"
    local mode="$2"
    local bin_file="$RESULTS_DIR/${engine}-${mode}.bin"
    local report_file="$RESULTS_DIR/${engine}-${mode}.txt"
    local plot_file="$RESULTS_DIR/${engine}-${mode}.html"

    echo "  ── mode: $mode ──────────────────────"

    # Warmup uses the same source as the attack so the runtime sees a
    # representative request shape before measurement.
    case "$mode" in
        single)
            echo "    warmup: ${WARMUP_RATE} req/s por ${WARMUP_DURATION}..."
            echo "$SINGLE_TARGET" | vegeta attack \
                -rate="$WARMUP_RATE/s" \
                -duration="$WARMUP_DURATION" \
                > /dev/null 2>&1

            echo "    atacando: ${RATE} req/s por ${DURATION} (single id)..."
            echo "$SINGLE_TARGET" | vegeta attack \
                -rate="$RATE/s" \
                -duration="$DURATION" \
                > "$bin_file"
            ;;
        cycled)
            echo "    warmup: ${WARMUP_RATE} req/s por ${WARMUP_DURATION}..."
            vegeta attack \
                -targets="$TARGETS_FILE" \
                -lazy \
                -rate="$WARMUP_RATE/s" \
                -duration="$WARMUP_DURATION" \
                > /dev/null 2>&1

            echo "    atacando: ${RATE} req/s por ${DURATION} (cycled ${CYCLED_COUNT} ids)..."
            vegeta attack \
                -targets="$TARGETS_FILE" \
                -lazy \
                -rate="$RATE/s" \
                -duration="$DURATION" \
                > "$bin_file"
            ;;
    esac

    vegeta report < "$bin_file" > "$report_file"
    vegeta plot   < "$bin_file" > "$plot_file"
    cat "$report_file"
    echo ""
}

run_engine() {
    local engine="$1"

    echo "─────────────────────────────────────────"
    echo "  Engine: $engine"
    echo "  Rate:   $RATE req/s | Duration: $DURATION"
    echo "─────────────────────────────────────────"

    ./httpdi-bench -engine "$engine" -addr "$ADDR" &
    local pid=$!
    wait_for_server

    for mode in "${MODES[@]}"; do
        run_attack "$engine" "$mode"
    done

    stop_server "$pid"
}

# ── Executar benchmarks ──────────────────────────────────────────────

generate_cycled_targets

echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  Benchmark: stdlib vs gin vs fiber vs echo vs chi ║"
echo "║  Rate: $RATE req/s | Duration: $DURATION                  ║"
echo "║  Modes: single (hot row) + cycled (${CYCLED_COUNT} ids)          ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""

for engine in "${ENGINES[@]}"; do
    run_engine "$engine"
done

# ── Relatório comparativo ────────────────────────────────────────────

SUMMARY="$RESULTS_DIR/summary.txt"

{
    echo "╔═══════════════════════════════════════════════════════════════════════════════════════════╗"
    echo "║  RELATÓRIO COMPARATIVO                                                                  ║"
    echo "║  Rate: $RATE req/s | Duration: $DURATION                                                        ║"
    echo "╠═══════════════════════════════════════════════════════════════════════════════════════════╣"
    echo "║"
    printf "║  %-8s %-8s %12s %12s %12s %12s %10s\n" \
        "ENGINE" "MODE" "THROUGHPUT" "P50" "P95" "P99" "SUCCESS"
    echo "║  ──────── ──────── ──────────── ──────────── ──────────── ──────────── ──────────"

    for engine in "${ENGINES[@]}"; do
        for mode in "${MODES[@]}"; do
            report="$RESULTS_DIR/${engine}-${mode}.txt"

            # vegeta packs values into single lines:
            #   Requests   [total, rate, throughput]  150000, 5000.04, 5000.03
            #   Latencies  [mean, 50, 95, 99, max]    50µs, 40µs, 71µs, 134µs, 6ms
            throughput=$(awk -F'[[:space:],]+' '/^Requests/ {print $NF}' "$report")
            p50=$(awk -F'[[:space:],]+' '/^Latencies/ {print $8}' "$report")
            p95=$(awk -F'[[:space:],]+' '/^Latencies/ {print $9}' "$report")
            p99=$(awk -F'[[:space:],]+' '/^Latencies/ {print $10}' "$report")
            success=$(awk '/^Success/ {print $3; exit}' "$report")

            printf "║  %-8s %-8s %12s %12s %12s %12s %10s\n" \
                "$engine" "$mode" "$throughput" "$p50" "$p95" "$p99" "$success"
        done
    done

    echo "║"
    echo "╚═══════════════════════════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Plots individuais em: $RESULTS_DIR/<engine>-<mode>.html"
} | tee "$SUMMARY"

# ── Plot combinado (HTML) ────────────────────────────────────────────

COMBINED_PLOT="$RESULTS_DIR/combined.html"
cat > "$COMBINED_PLOT" <<'HTMLHEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Benchmark: stdlib vs gin vs fiber</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; background: #1a1a2e; color: #e0e0e0; }
  h1 { color: #e94560; }
  h2 { color: #0f3460; background: #16213e; padding: 0.5rem 1rem; border-radius: 4px; color: #e0e0e0; }
  h3 { color: #aaa; margin: 0.25rem 0; font-size: 0.85rem; font-weight: normal; }
  iframe { width: 100%; height: 360px; border: 1px solid #333; border-radius: 4px; margin-bottom: 1rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 1rem; }
</style>
</head>
<body>
<h1>Benchmark Comparativo</h1>
<div class="grid">
HTMLHEAD

for engine in "${ENGINES[@]}"; do
    {
        echo "  <div>"
        echo "    <h2>$engine</h2>"
        for mode in "${MODES[@]}"; do
            echo "    <h3>$mode</h3>"
            echo "    <iframe src=\"${engine}-${mode}.html\"></iframe>"
        done
        echo "  </div>"
    } >> "$COMBINED_PLOT"
done

cat >> "$COMBINED_PLOT" <<'HTMLTAIL'
</div>
</body>
</html>
HTMLTAIL

echo ""
echo "Relatório combinado: $RESULTS_DIR/combined.html"

rm -f httpdi-bench
