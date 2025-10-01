#!/usr/bin/env bash
set -eu
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

echo "Starting Kafka and Zookeeper..."
docker-compose up -d

# Wait for Kafka to be healthy
echo "Waiting for Kafka to be ready..."
for i in {1..60}; do
  if docker-compose exec -T kafka bash -lc "/opt/bitnami/kafka/bin/kafka-topics.sh --bootstrap-server kafka:9092 --list | cat" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "Creating topics..."
docker-compose exec -T kafka bash -lc "\
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic source --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_id --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_name --partitions 3 --replication-factor 1 && \
  kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic sorted_continent --partitions 3 --replication-factor 1 \
" | cat

start_time=$(date +%s)

echo "Running producer..."
docker-compose run --rm pipeline_app ./producer | cat

echo "Running sorters sequentially (each must read complete source topic)..."
docker-compose run --rm -T pipeline_app ./sorter id
docker-compose run --rm -T pipeline_app ./sorter name
docker-compose run --rm -T pipeline_app ./sorter continent

end_time=$(date +%s)
echo "Total pipeline runtime: $((end_time - start_time)) seconds"

# Pause so the window doesn't close immediately when run via double-click
read -p "Press Enter to exit" || true


