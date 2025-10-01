#!/usr/bin/env bash
set -eu
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

# First-run setup script for new users
# This script ensures a clean environment and runs the complete pipeline

echo "========================================"
echo " Kafka Sort Pipeline - First Run Setup"
echo "========================================"
echo ""

# Navigate to script directory's parent (project root)
cd "$(dirname "$0")/.."

echo "[Step 1/6] Building Docker image..."
docker build -t core-infra-project:latest .
echo "  ✓ Build complete"
echo ""

echo "[Step 2/6] Starting Kafka and Zookeeper..."
docker compose down -v 2>/dev/null || true
docker compose up -d zookeeper kafka
echo "  ✓ Services started"
echo ""

echo "[Step 3/6] Waiting for Kafka to be ready (this may take 30-60 seconds)..."
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if docker compose exec -T kafka bash -lc "kafka-topics --bootstrap-server kafka:9092 --list" >/dev/null 2>&1; then
        echo "  ✓ Kafka is ready"
        break
    fi
    attempt=$((attempt + 1))
    sleep 2
    if [ $((attempt % 10)) -eq 0 ]; then
        echo "  ... still waiting ($attempt/$max_attempts)"
    fi
done

if [ $attempt -eq $max_attempts ]; then
    echo "  ✗ Kafka failed to become ready. Check logs: docker compose logs kafka"
    exit 1
fi
echo ""

echo "[Step 4/6] Creating Kafka topics..."
docker compose exec -T kafka bash -lc "
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic source --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_id --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_name --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_continent --partitions 3 --replication-factor 1
" >/dev/null 2>&1
echo "  ✓ Topics created"
echo ""

echo "[Step 5/6] Running producer (this will take 10-15 minutes for 50M records)..."
echo "  Tip: To monitor with pprof, open http://localhost:6060/debug/pprof/ in another terminal run:"
echo "       docker compose run --rm -p 6060:6060 pipeline_app ./producer"
echo ""
start_time=$(date +%s)
docker compose run --rm pipeline_app ./producer
echo "  ✓ Producer complete"
echo ""

# Wait for async Kafka writes to fully persist before sorters start reading
echo "  Waiting 30 seconds for Kafka to flush all async writes..."
sleep 30

# Verify source topic has data and prime Kafka consumer mechanics
echo "  Verifying source topic has data and priming Kafka..."
docker compose exec -T kafka bash -lc "kafka-console-consumer --bootstrap-server kafka:9092 --topic source --from-beginning --max-messages 10 --timeout-ms 3000 2>/dev/null" > /dev/null
if [ $? -ne 0 ]; then
    echo "  ✗ Source topic appears empty or unreachable! Check producer logs."
    exit 1
fi
echo "  ✓ Source topic verified and primed (Kafka consumer state initialized)"
echo "  Waiting additional 10 seconds for consumer group coordination to stabilize..."
sleep 10
echo ""

echo "[Step 6/6] Running sorters sequentially..."
echo "  Note: Sorters must run one at a time so each reads the complete source topic"
echo "  Tip: To monitor sorter with pprof, run manually with port mapping:"
echo "       docker compose run --rm -p 6061:6061 pipeline_app ./sorter id"
echo ""

echo "  Running Name sorter first (seems to work more reliably)..."
docker compose run --rm -T pipeline_app ./sorter name
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    echo "  ✗ Name sorter failed with exit code: $EXIT_CODE"
    if [ $EXIT_CODE -eq 137 ]; then
        echo "     (Exit 137 = OOM - container was killed by Docker)"
    fi
    exit 1
fi
echo "  ✓ Name sorter complete"
echo "  Waiting 10 seconds to ensure Kafka writes are flushed..."
sleep 10

echo "  Running ID sorter..."
docker compose run --rm -T pipeline_app ./sorter id
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    echo "  ✗ ID sorter failed with exit code: $EXIT_CODE"
    if [ $EXIT_CODE -eq 137 ]; then
        echo "     (Exit 137 = OOM - container was killed by Docker)"
    fi
    exit 1
fi
echo "  ✓ ID sorter complete"
echo "  Waiting 10 seconds to ensure Kafka writes are flushed..."
sleep 10

echo "  Running Continent sorter..."
docker compose run --rm -T pipeline_app ./sorter continent
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    echo "  ✗ Continent sorter failed with exit code: $EXIT_CODE"
    if [ $EXIT_CODE -eq 137 ]; then
        echo "     (Exit 137 = OOM - container was killed by Docker)"
    fi
    exit 1
fi
echo "  ✓ Continent sorter complete"
echo "  Waiting 10 seconds to ensure all Kafka writes are persisted..."
sleep 10
echo ""

end_time=$(date +%s)
runtime=$((end_time - start_time))

echo "========================================"
echo " Pipeline Execution Complete!"
echo "========================================"
echo "Total runtime: $runtime seconds ($(($runtime / 60)) minutes)"
echo ""
echo "Next steps:"
echo "  1. Run validation: ./scripts/test_validation.sh"
echo "  2. View sample data:"
echo "     docker compose exec -T kafka bash -lc 'kafka-console-consumer --bootstrap-server kafka:9092 --topic sorted_id --from-beginning --max-messages 50 --timeout-ms 10000'"
echo "  3. Cleanup: ./scripts/cleanup.sh"
echo ""

# Pause so the window doesn't close immediately when run via double-click
read -p "Press Enter to exit" || true

