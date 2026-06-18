#!/usr/bin/env bash
# bench-payload.sh — payload-size benchmark for the /v1/transactions/{N} list
# endpoint. Answers Q3: how do the wire formats behave as the artifact grows?
# Fiber is the only engine (the framework axis is settled in bench-full.sh);
# the variable here is backend × payload size.
#
# Matrix: 1 engine (fiber) × 8 backends × 4 sizes = 32 cells.
#   backends: mysql, rest (h1/json), resth2/resth2-pb/resth2-avro (h2c),
#             grpc (pb), grpc-avro, grpc-flat (FlatBuffers, zero-copy).
#   sizes:    N = 10 / 100 / 1000 / 5000 transactions (~3KB → ~1.5MB JSON).
#
# Default rate is much lower than bench-full.sh because the payloads are large
# (N=5000 ≈ 1.5 MB of JSON): at 500 req/s that's already ~750 MB/s for the JSON
# backends. A backend that can't sustain the rate (success < 100% or throughput
# < rate) is itself a finding — that's where the compact formats earn their keep.
#
# Requisitos: go, vegeta, docker, curl
# Uso: ./bench-payload.sh [rate] [duration]
#   rate     — requisições por segundo (default: 500)
#   duration — duração de cada sessão (default: 10s)

set -euo pipefail

RATE="${1:-500}"
DURATION="${2:-10s}"
FRONT_ADDR=":8080"
DATA_REST_ADDR=":9090"
DATA_GRPC_ADDR=":9091"
DATA_RESTH2_ADDR=":9092"
RESULTS_DIR="bench-results-payload"
ENGINE="fiber"
BACKENDS=("mysql" "rest" "resth2" "resth2-pb" "resth2-avro" "grpc" "grpc-avro" "grpc-flat")
SIZES=(10 100 1000 5000)
WARMUP_RATE=200
WARMUP_DURATION="3s"
MYSQL_CONTAINER="httpdi-mysql"
FRONT_BIN="httpdi-payload-front"
DATA_BIN="httpdi-payload-data"

# ── pre-flight ───────────────────────────────────────────────────────

command -v vegeta >/dev/null 2>&1 || { echo "erro: vegeta não encontrado (go install github.com/tsenart/vegeta@latest)"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "erro: docker não encontrado"; exit 1; }
command -v curl   >/dev/null 2>&1 || { echo "erro: curl não encontrado"; exit 1; }

# ── helpers ──────────────────────────────────────────────────────────

wait_for_mysql() {
    local retries=300
    while [ "$(docker inspect -f '{{.State.Health.Status}}' "$MYSQL_CONTAINER" 2>/dev/null)" != "healthy" ]; do
        retries=$((retries - 1))
        [ "$retries" -le 0 ] && { echo "erro: mysql não ficou healthy após 300s"; exit 1; }
        sleep 1
    done
}

wait_for_url() {
    local url="$1" retries=60
    while ! curl -sf "$url" >/dev/null 2>&1; do
        retries=$((retries - 1))
        [ "$retries" -le 0 ] && { echo "erro: $url não respondeu"; exit 1; }
        sleep 0.2
    done
}

DATASERVICE_PID=""
FRONT_PID=""

cleanup() {
    [ -n "${FRONT_PID:-}" ] && { kill "$FRONT_PID" 2>/dev/null || true; wait "$FRONT_PID" 2>/dev/null || true; }
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
    local backend="$1"
    case "$backend" in
        rest)
            ./"$FRONT_BIN" -engine "$ENGINE" -addr "$FRONT_ADDR" -repo rest -repo-addr "http://localhost:${DATA_REST_ADDR#:}" >/dev/null 2>&1 & ;;
        resth2 | resth2-pb | resth2-avro)
            ./"$FRONT_BIN" -engine "$ENGINE" -addr "$FRONT_ADDR" -repo "$backend" -repo-addr "http://localhost:${DATA_RESTH2_ADDR#:}" >/dev/null 2>&1 & ;;
        grpc | grpc-avro | grpc-flat)
            ./"$FRONT_BIN" -engine "$ENGINE" -addr "$FRONT_ADDR" -repo "$backend" -repo-addr "localhost:${DATA_GRPC_ADDR#:}" >/dev/null 2>&1 & ;;
        *)
            ./"$FRONT_BIN" -engine "$ENGINE" -addr "$FRONT_ADDR" -repo mysql >/dev/null 2>&1 & ;;
    esac
    FRONT_PID=$!
    wait_for_url "http://localhost:${FRONT_ADDR#:}/health"
}

stop_front() {
    [ -n "${FRONT_PID:-}" ] && { kill "$FRONT_PID" 2>/dev/null || true; wait "$FRONT_PID" 2>/dev/null || true; FRONT_PID=""; }
    # Wait for the listen port to actually free up before the next front binds —
    # fiber's shutdown is async, and a rebind race would fail wait_for_url under
    # set -e and abort the whole run.
    local tries=50
    while lsof -ti "tcp:${FRONT_ADDR#:}" >/dev/null 2>&1; do
        tries=$((tries - 1)); [ "$tries" -le 0 ] && break; sleep 0.1
    done
}

run_attack() {
    local backend="$1" size="$2"
    local target="GET http://localhost:${FRONT_ADDR#:}/v1/transactions/${size}"
    local bin_file="$RESULTS_DIR/${backend}-n${size}.bin"
    local report_file="$RESULTS_DIR/${backend}-n${size}.txt"
    local plot_file="$RESULTS_DIR/${backend}-n${size}.html"

    echo "      ── N=$size ──"
    echo "$target" | vegeta attack -rate="$WARMUP_RATE/s" -duration="$WARMUP_DURATION" > /dev/null 2>&1
    echo "$target" | vegeta attack -rate="$RATE/s" -duration="$DURATION" > "$bin_file"
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

echo "→ starting dataservice"
./"$DATA_BIN" -rest-addr "$DATA_REST_ADDR" -grpc-addr "$DATA_GRPC_ADDR" -rest-h2c-addr "$DATA_RESTH2_ADDR" >/dev/null 2>&1 &
DATASERVICE_PID=$!
wait_for_url "http://localhost:${DATA_REST_ADDR#:}/health"
echo "✓ dataservice ready (PID $DATASERVICE_PID)"

# ── matrix ───────────────────────────────────────────────────────────

echo ""
echo "╔══════════════════════════════════════════════════════════════════════════╗"
echo "║  Payload matrix: fiber × 8 backends × 4 sizes = 32 cells                 ║"
echo "║  Rate: $RATE req/s | Duration: $DURATION | Sizes: ${SIZES[*]}              "
echo "╚══════════════════════════════════════════════════════════════════════════╝"

for backend in "${BACKENDS[@]}"; do
    echo ""
    echo "  ── backend: $backend ──"
    start_front "$backend"
    for size in "${SIZES[@]}"; do
        run_attack "$backend" "$size"
    done
    stop_front
done

# ── summary: per-size sub-tables ─────────────────────────────────────

SUMMARY="$RESULTS_DIR/summary.txt"
{
    echo "╔════════════════════════════════════════════════════════════════════════════════════╗"
    echo "║  RELATÓRIO PAYLOAD — fiber × backend × tamanho                                    ║"
    echo "║  Rate: $RATE req/s | Duration: $DURATION                                                 ║"
    echo "╚════════════════════════════════════════════════════════════════════════════════════╝"
    for size in "${SIZES[@]}"; do
        echo ""
        echo "── N=$size ─────────────────────────────────────────────────────────────────────────"
        printf "  %-12s %12s %12s %12s %12s %10s\n" "BACKEND" "THROUGHPUT" "P50" "P95" "P99" "SUCCESS"
        echo "  ──────────── ──────────── ──────────── ──────────── ──────────── ──────────"
        for backend in "${BACKENDS[@]}"; do
            report="$RESULTS_DIR/${backend}-n${size}.txt"
            throughput=$(awk -F'[[:space:],]+' '/^Requests/ {print $NF}' "$report")
            p50=$(awk -F'[[:space:],]+' '/^Latencies/ {print $8}' "$report")
            p95=$(awk -F'[[:space:],]+' '/^Latencies/ {print $9}' "$report")
            p99=$(awk -F'[[:space:],]+' '/^Latencies/ {print $10}' "$report")
            success=$(awk '/^Success/ {print $3; exit}' "$report")
            printf "  %-12s %12s %12s %12s %12s %10s\n" "$backend" "$throughput" "$p50" "$p95" "$p99" "$success"
        done
    done
    echo ""
    echo "Plots individuais em: $RESULTS_DIR/<backend>-n<size>.html"
} | tee "$SUMMARY"

# ── combined.html, grouped per size ──────────────────────────────────

COMBINED="$RESULTS_DIR/combined.html"
cat > "$COMBINED" <<'HTMLHEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Benchmark — Payload Matrix</title>
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
<h1>Benchmark — Payload Matrix (fiber × 8 backends × 4 sizes)</h1>
HTMLHEAD

for size in "${SIZES[@]}"; do
    {
        echo "<h2>N = $size</h2>"
        echo '<div class="grid">'
        for backend in "${BACKENDS[@]}"; do
            echo "  <div><h3>$backend / N=$size</h3><iframe src=\"${backend}-n${size}.html\"></iframe></div>"
        done
        echo '</div>'
    } >> "$COMBINED"
done
echo "</body></html>" >> "$COMBINED"

echo ""
echo "Relatório combinado: $COMBINED"
