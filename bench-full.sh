#!/usr/bin/env bash
# bench-full.sh — full matrix benchmark: 5 engines × 7 backends × 2 modes
# Builds on bench.sh but adds the -repo dimension as a (transport,
# serialization) matrix: mysql in-process; REST over HTTP/1.1 and HTTP/2
# (h2c) with JSON/protobuf/Avro; gRPC with protobuf/Avro. Output goes to
# ./bench-results-full/ so it doesn't collide with bench.sh's
# ./bench-results/.
#
# Requisitos: go, vegeta, docker, curl
# Uso: ./bench-full.sh [rate] [duration]
#   rate     — requisições por segundo (default: 5000)
#   duration — duração de cada sessão (default: 30s)
#
# Tempo estimado: ~40 min (70 cells × 33s + setup).

set -euo pipefail

RATE="${1:-5000}"
DURATION="${2:-30s}"
FRONT_ADDR=":8080"
DATA_REST_ADDR=":9090"
DATA_GRPC_ADDR=":9091"
DATA_RESTH2_ADDR=":9092"
RESULTS_DIR="bench-results-full"
ENGINES=("stdlib" "gin" "fiber" "echo" "chi")
BACKENDS=("mysql" "rest" "resth2" "resth2-pb" "resth2-avro" "grpc" "grpc-avro")
MODES=("single" "cycled")
BENCH_TX_ID="00000000-0000-0000-0000-000000000001"
SINGLE_TARGET="GET http://localhost:${FRONT_ADDR#:}/v1/transaction/${BENCH_TX_ID}"
CYCLED_COUNT=150000
TARGETS_FILE="$RESULTS_DIR/targets-cycled.txt"
WARMUP_RATE=500
WARMUP_DURATION="3s"
MYSQL_CONTAINER="httpdi-mysql"
FRONT_BIN="httpdi-full-front"
DATA_BIN="httpdi-full-data"

# ── pre-flight ───────────────────────────────────────────────────────

command -v vegeta >/dev/null 2>&1 || {
    echo "erro: vegeta não encontrado"
    echo "  go install github.com/tsenart/vegeta@latest"
    exit 1
}
command -v docker >/dev/null 2>&1 || {
    echo "erro: docker não encontrado"
    exit 1
}
command -v curl >/dev/null 2>&1 || {
    echo "erro: curl não encontrado"
    exit 1
}

# ── helpers ──────────────────────────────────────────────────────────

wait_for_mysql() {
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

wait_for_url() {
    local url="$1"
    local retries=60
    while ! curl -sf "$url" >/dev/null 2>&1; do
        retries=$((retries - 1))
        if [ "$retries" -le 0 ]; then
            echo "erro: $url não respondeu após 60 tentativas"
            exit 1
        fi
        sleep 0.2
    done
}

# Process bookkeeping. PIDs are set as services start and cleared by their
# stop functions; cleanup() trusts whatever is set at EXIT.
DATASERVICE_PID=""
FRONT_PID=""

cleanup() {
    if [ -n "${FRONT_PID:-}" ]; then
        kill "$FRONT_PID" 2>/dev/null || true
        wait "$FRONT_PID" 2>/dev/null || true
    fi
    if [ -n "${DATASERVICE_PID:-}" ]; then
        echo "→ stopping dataservice (PID $DATASERVICE_PID)"
        kill "$DATASERVICE_PID" 2>/dev/null || true
        wait "$DATASERVICE_PID" 2>/dev/null || true
    fi
    echo "→ docker compose down"
    docker compose down >/dev/null 2>&1 || true
    rm -f "$FRONT_BIN" "$DATA_BIN"
}
trap cleanup EXIT

start_front() {
    local engine="$1"
    local backend="$2"
    case "$backend" in
        rest)
            ./"$FRONT_BIN" -engine "$engine" -addr "$FRONT_ADDR" \
                -repo rest -repo-addr "http://localhost:${DATA_REST_ADDR#:}" \
                >/dev/null 2>&1 &
            ;;
        resth2 | resth2-pb | resth2-avro)
            ./"$FRONT_BIN" -engine "$engine" -addr "$FRONT_ADDR" \
                -repo "$backend" -repo-addr "http://localhost:${DATA_RESTH2_ADDR#:}" \
                >/dev/null 2>&1 &
            ;;
        grpc)
            ./"$FRONT_BIN" -engine "$engine" -addr "$FRONT_ADDR" \
                -repo grpc -repo-addr "localhost:${DATA_GRPC_ADDR#:}" \
                >/dev/null 2>&1 &
            ;;
        grpc-avro)
            ./"$FRONT_BIN" -engine "$engine" -addr "$FRONT_ADDR" \
                -repo grpc-avro -repo-addr "localhost:${DATA_GRPC_ADDR#:}" \
                >/dev/null 2>&1 &
            ;;
        *)
            ./"$FRONT_BIN" -engine "$engine" -addr "$FRONT_ADDR" \
                -repo mysql \
                >/dev/null 2>&1 &
            ;;
    esac
    FRONT_PID=$!
    wait_for_url "http://localhost:${FRONT_ADDR#:}/health"
}

stop_front() {
    if [ -n "${FRONT_PID:-}" ]; then
        kill "$FRONT_PID" 2>/dev/null || true
        wait "$FRONT_PID" 2>/dev/null || true
        FRONT_PID=""
    fi
    sleep 1
}

run_attack() {
    local engine="$1"
    local backend="$2"
    local mode="$3"
    local bin_file="$RESULTS_DIR/${engine}-${backend}-${mode}.bin"
    local report_file="$RESULTS_DIR/${engine}-${backend}-${mode}.txt"
    local plot_file="$RESULTS_DIR/${engine}-${backend}-${mode}.html"

    echo "      ── mode: $mode ──"
    case "$mode" in
        single)
            echo "$SINGLE_TARGET" | vegeta attack \
                -rate="$WARMUP_RATE/s" -duration="$WARMUP_DURATION" \
                > /dev/null 2>&1
            echo "$SINGLE_TARGET" | vegeta attack \
                -rate="$RATE/s" -duration="$DURATION" \
                > "$bin_file"
            ;;
        cycled)
            vegeta attack -targets="$TARGETS_FILE" -lazy \
                -rate="$WARMUP_RATE/s" -duration="$WARMUP_DURATION" \
                > /dev/null 2>&1
            vegeta attack -targets="$TARGETS_FILE" -lazy \
                -rate="$RATE/s" -duration="$DURATION" \
                > "$bin_file"
            ;;
    esac
    vegeta report < "$bin_file" > "$report_file"
    vegeta plot   < "$bin_file" > "$plot_file"
}

# ── setup ────────────────────────────────────────────────────────────

echo "→ docker compose up -d"
docker compose up -d
wait_for_mysql
echo "✓ mysql healthy"

echo "→ go build (front + dataservice)"
go build -o "$FRONT_BIN" .
go build -o "$DATA_BIN" ./cmd/dataservice

rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR"

# Generate the 150k-line targets file once. Same id space as bench.sh.
seq 1 "$CYCLED_COUNT" | awk -v addr="${FRONT_ADDR#:}" '{
    printf "GET http://localhost:%s/v1/transaction/00000000-0000-0000-0000-%012d\n", addr, $1
}' > "$TARGETS_FILE"

echo "→ starting dataservice"
./"$DATA_BIN" -rest-addr "$DATA_REST_ADDR" -grpc-addr "$DATA_GRPC_ADDR" \
    -rest-h2c-addr "$DATA_RESTH2_ADDR" \
    >/dev/null 2>&1 &
DATASERVICE_PID=$!
wait_for_url "http://localhost:${DATA_REST_ADDR#:}/health"
echo "✓ dataservice ready (PID $DATASERVICE_PID)"

# ── matrix ───────────────────────────────────────────────────────────

echo ""
echo "╔══════════════════════════════════════════════════════════════════════════╗"
echo "║  Full matrix: 5 engines × 7 backends × 2 modes = 70 cells                ║"
echo "║  Rate: $RATE req/s | Duration: $DURATION                                       ║"
echo "╚══════════════════════════════════════════════════════════════════════════╝"

for engine in "${ENGINES[@]}"; do
    echo ""
    echo "═══════════════════════════════════════════════════════"
    echo "  Engine: $engine"
    echo "═══════════════════════════════════════════════════════"
    for backend in "${BACKENDS[@]}"; do
        echo "  ── backend: $backend ──"
        start_front "$engine" "$backend"
        for mode in "${MODES[@]}"; do
            run_attack "$engine" "$backend" "$mode"
        done
        stop_front
    done
done

# ── per-engine sub-tables ────────────────────────────────────────────

SUMMARY="$RESULTS_DIR/summary.txt"

{
    echo "╔════════════════════════════════════════════════════════════════════════════════════╗"
    echo "║  RELATÓRIO COMPARATIVO — MATRIZ COMPLETA                                          ║"
    echo "║  Rate: $RATE req/s | Duration: $DURATION                                                 ║"
    echo "╚════════════════════════════════════════════════════════════════════════════════════╝"

    for engine in "${ENGINES[@]}"; do
        echo ""
        echo "── $engine ─────────────────────────────────────────────────────────────────────────"
        printf "  %-12s %-8s %12s %12s %12s %12s %10s\n" \
            "BACKEND" "MODE" "THROUGHPUT" "P50" "P95" "P99" "SUCCESS"
        echo "  ──────────── ──────── ──────────── ──────────── ──────────── ──────────── ──────────"
        for backend in "${BACKENDS[@]}"; do
            for mode in "${MODES[@]}"; do
                report="$RESULTS_DIR/${engine}-${backend}-${mode}.txt"
                throughput=$(awk -F'[[:space:],]+' '/^Requests/ {print $NF}' "$report")
                p50=$(awk -F'[[:space:],]+' '/^Latencies/ {print $8}' "$report")
                p95=$(awk -F'[[:space:],]+' '/^Latencies/ {print $9}' "$report")
                p99=$(awk -F'[[:space:],]+' '/^Latencies/ {print $10}' "$report")
                success=$(awk '/^Success/ {print $3; exit}' "$report")
                printf "  %-12s %-8s %12s %12s %12s %12s %10s\n" \
                    "$backend" "$mode" "$throughput" "$p50" "$p95" "$p99" "$success"
            done
        done
    done

    echo ""
    echo "Plots individuais em: $RESULTS_DIR/<engine>-<backend>-<mode>.html"
} | tee "$SUMMARY"

# ── combined.html, grouped per engine ────────────────────────────────

COMBINED_PLOT="$RESULTS_DIR/combined.html"
cat > "$COMBINED_PLOT" <<'HTMLHEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Benchmark — Full Matrix</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; background: #1a1a2e; color: #e0e0e0; }
  h1 { color: #e94560; }
  h2 { color: #0f3460; background: #16213e; padding: 0.5rem 1rem; border-radius: 4px; color: #e0e0e0; margin-top: 2rem; }
  h3 { color: #aaa; margin: 0.25rem 0; font-size: 0.85rem; font-weight: normal; }
  iframe { width: 100%; height: 320px; border: 1px solid #333; border-radius: 4px; margin-bottom: 1rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; }
</style>
</head>
<body>
<h1>Benchmark — Full Matrix (5 engines × 7 backends × 2 modes)</h1>
HTMLHEAD

for engine in "${ENGINES[@]}"; do
    {
        echo "<h2>$engine</h2>"
        echo '<div class="grid">'
        for backend in "${BACKENDS[@]}"; do
            for mode in "${MODES[@]}"; do
                echo "  <div>"
                echo "    <h3>$backend / $mode</h3>"
                echo "    <iframe src=\"${engine}-${backend}-${mode}.html\"></iframe>"
                echo "  </div>"
            done
        done
        echo '</div>'
    } >> "$COMBINED_PLOT"
done

cat >> "$COMBINED_PLOT" <<'HTMLTAIL'
</body>
</html>
HTMLTAIL

echo ""
echo "Relatório combinado: $RESULTS_DIR/combined.html"
