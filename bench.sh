#!/usr/bin/env bash
# bench.sh — benchmark comparativo: stdlib vs gin vs fiber
# Requisitos: go, vegeta (go install github.com/tsenart/vegeta@latest)
# Uso: ./bench.sh [rate] [duration]
#   rate     — requisições por segundo (default: 5000)
#   duration — duração de cada sessão (default: 30s)

set -euo pipefail

RATE="${1:-5000}"
DURATION="${2:-30s}"
ADDR=":8080"
TARGET="GET http://localhost:${ADDR#:}/health"
RESULTS_DIR="bench-results"
ENGINES=("stdlib" "gin" "fiber")
WARMUP_RATE=500
WARMUP_DURATION="3s"

command -v vegeta >/dev/null 2>&1 || {
    echo "erro: vegeta não encontrado"
    echo "  go install github.com/tsenart/vegeta@latest"
    exit 1
}

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

run_bench() {
    local engine="$1"
    local bin_file="$RESULTS_DIR/${engine}.bin"
    local report_file="$RESULTS_DIR/${engine}.txt"
    local plot_file="$RESULTS_DIR/${engine}.html"

    echo "─────────────────────────────────────────"
    echo "  Engine: $engine"
    echo "  Rate:   $RATE req/s | Duration: $DURATION"
    echo "─────────────────────────────────────────"

    ./httpdi-bench -engine "$engine" -addr "$ADDR" &
    local pid=$!
    wait_for_server

    # Warmup: estabiliza o runtime antes da medição real.
    echo "  warmup: ${WARMUP_RATE} req/s por ${WARMUP_DURATION}..."
    echo "$TARGET" | vegeta attack \
        -rate="$WARMUP_RATE/s" \
        -duration="$WARMUP_DURATION" \
        > /dev/null 2>&1

    # Medição
    echo "  atacando: ${RATE} req/s por ${DURATION}..."
    echo "$TARGET" | vegeta attack \
        -rate="$RATE/s" \
        -duration="$DURATION" \
        > "$bin_file"

    # Relatório texto
    vegeta report < "$bin_file" > "$report_file"

    # Plot HTML individual
    vegeta plot < "$bin_file" > "$plot_file"

    cat "$report_file"
    echo ""

    stop_server "$pid"
}

# ── Executar benchmarks ──────────────────────────────────────────────

echo ""
echo "╔═══════════════════════════════════════════╗"
echo "║  Benchmark: stdlib vs gin vs fiber        ║"
echo "║  Rate: $RATE req/s | Duration: $DURATION          ║"
echo "╚═══════════════════════════════════════════╝"
echo ""

for engine in "${ENGINES[@]}"; do
    run_bench "$engine"
done

# ── Relatório comparativo ────────────────────────────────────────────

SUMMARY="$RESULTS_DIR/summary.txt"

{
    echo "╔═══════════════════════════════════════════════════════════════════════════════════╗"
    echo "║  RELATÓRIO COMPARATIVO                                                          ║"
    echo "║  Rate: $RATE req/s | Duration: $DURATION                                                ║"
    echo "╠═══════════════════════════════════════════════════════════════════════════════════╣"
    echo "║"
    printf "║  %-10s %12s %12s %12s %12s %10s\n" \
        "ENGINE" "THROUGHPUT" "P50" "P95" "P99" "SUCCESS"
    echo "║  ────────── ──────────── ──────────── ──────────── ──────────── ──────────"

    for engine in "${ENGINES[@]}"; do
        report="$RESULTS_DIR/${engine}.txt"

        throughput=$(grep -i "^Throughput" "$report" | awk '{print $2}')
        p50=$(grep -i "50th" "$report" | awk '{print $2}')
        p95=$(grep -i "95th" "$report" | awk '{print $2}')
        p99=$(grep -i "99th" "$report" | awk '{print $2}')
        success=$(grep -i "^Success" "$report" | head -1 | awk '{print $3}')

        printf "║  %-10s %12s %12s %12s %12s %10s\n" \
            "$engine" "$throughput" "$p50" "$p95" "$p99" "$success"
    done

    echo "║"
    echo "╚═══════════════════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Plots individuais em: $RESULTS_DIR/<engine>.html"
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
  iframe { width: 100%; height: 480px; border: 1px solid #333; border-radius: 4px; margin-bottom: 2rem; }
  .grid { display: grid; grid-template-columns: 1fr; gap: 1rem; }
  @media (min-width: 900px) { .grid { grid-template-columns: 1fr 1fr 1fr; } iframe { height: 400px; } }
</style>
</head>
<body>
<h1>Benchmark Comparativo</h1>
<div class="grid">
HTMLHEAD

for engine in "${ENGINES[@]}"; do
    echo "  <div>" >> "$COMBINED_PLOT"
    echo "    <h2>$engine</h2>" >> "$COMBINED_PLOT"
    echo "    <iframe src=\"${engine}.html\"></iframe>" >> "$COMBINED_PLOT"
    echo "  </div>" >> "$COMBINED_PLOT"
done

cat >> "$COMBINED_PLOT" <<'HTMLTAIL'
</div>
</body>
</html>
HTMLTAIL

echo ""
echo "Relatório combinado: $RESULTS_DIR/combined.html"

rm -f httpdi-bench
