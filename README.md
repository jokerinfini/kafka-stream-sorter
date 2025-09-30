# Core Infra Project: Kafka External Sort Pipeline (Go)

Open the interactive infographic: [infographic.html](./infographic.html)

## Project Overview
A high-performance Go pipeline that generates 50 million CSV records, publishes to Kafka, consumes and sorts the data by three keys (id, name, continent) under a strict ~2GB memory cap using external merge sort, and publishes sorted results to separate Kafka topics. Everything runs with Docker and docker-compose.

## Architecture
- Producer: Generates CSV records and publishes to Kafka topic `source` using `segmentio/kafka-go`.
- Sorters: Three instances (id, name, continent). Each consumes from `source`, performs external merge sort, and publishes to `sorted_id`, `sorted_name`, `sorted_continent`.
- Kafka + Zookeeper: Provided by bitnami images. Kafka memory limited to 512MB to leave headroom for Go services.
- Docker: Multi-stage build producing minimal runtime image.

### Quick Architecture Diagram
```
[Generator goroutines] -> [Buffered Channel] -> [Kafka Writer] ->  source (Kafka)
                                                            \
                                                             \
  sorter id  ------> [Chunk sort + spill] -> [k-way merge] ->  sorted_id (Kafka)
  sorter name  ----> [Chunk sort + spill] -> [k-way merge] ->  sorted_name (Kafka)
  sorter continent -> [Chunk sort + spill] -> [k-way merge] ->  sorted_continent (Kafka)
```

## End‑to‑End Workflow (What the code does)
1) Data generation and publish (cmd/producer)
- Spawns a pool of goroutines to generate random CSV records (id,name,address,continent) with minimal allocations.
- Uses a buffered channel as a queue and a single high-throughput Kafka writer to batch writes to the `source` topic.
- Prints progress every 1,000,000 records and final elapsed time.

2) External sorting (cmd/sorter + internal/sort/external_sort.go)
- Chunk phase: reads up to 1,000,000 messages at a time from `source`, sorts in memory by the chosen key (id/name/continent), and spills each sorted chunk to a temporary file.
- Merge phase: opens all chunk files and does a k‑way merge using a min‑heap, streaming the globally sorted sequence directly to the destination topic (`sorted_id`/`sorted_name`/`sorted_continent`).
- Cleans up temporary files and prints elapsed time per sorter.

3) Orchestration (scripts/run.sh)
- Brings up Kafka/ZooKeeper, creates topics, runs the producer, then runs three sorter containers in parallel and prints total wall‑clock time.

## Algorithm Explanation (External Merge Sort)
1. Chunk Phase: Read fixed-size chunks (e.g., 1,000,000 records) from Kafka. Sort in-memory by the target key and spill to temp files.
2. Merge Phase: Open all chunk files and perform a k-way merge using a min-heap, streaming merged records to the destination Kafka topic.
3. Cleanup: Remove temporary files.

This approach respects the memory limit because it never holds the full dataset in memory.

## Resource Controls (2GB RAM / 4 CPUs)
- Kafka broker heap capped via `KAFKA_HEAP_OPTS=-Xmx384m -Xms384m` and container `mem_limit: 512m`.
- Application container constrained in `docker-compose.yml` with `mem_limit: 1500m` and `cpus: 4` (tune in Docker Desktop if needed).
- External sort chunk size chosen to stay within remaining memory (~1.5GB) even at peak.

## How to Run
1. Build the runtime image:
   ```bash
   ./scripts/build.sh
   ```
2. Start the pipeline (brings up Kafka, creates topics, runs producer, then sorters):
   ```bash
   ./scripts/run.sh
   ```

## How to Verify Correctness
- Consume from sorted topics and check ordering:
  - `sorted_id` should be ascending by integer id
  - `sorted_name` ascending lexicographic by name
  - `sorted_continent` ascending lexicographic by continent
- Example (inside kafka container):
  ```bash
  docker-compose exec kafka /opt/bitnami/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server kafka:9092 --topic sorted_id --from-beginning --max-messages 50
  ```

PowerShell-friendly commands (no pipes/issues):
```powershell
docker compose exec -T kafka bash -lc 'kafka-console-consumer --bootstrap-server kafka:9092 --topic sorted_id --from-beginning --max-messages 50 --timeout-ms 10000'
```

## Performance Optimizations
- Batched Kafka writes via `WriteMessages`
- Goroutine fan-out for record generation, buffered channels
- Efficient preallocated buffers for CSV generation
- External sort with large chunk size to reduce number of merge files
- Heap-based k-way merge with streaming writes
- Minimal data copies in merge path and buffered file I/O for spill/merge
- Snappy compression and LeastBytes balancer for Kafka throughput

## Parameters to Tune
- Producer
  - Records: change `totalRecords` in `cmd/producer/main.go` for faster tests
  - Concurrency: worker count = `runtime.NumCPU() * 2`
  - Kafka batching: `BatchSize`, `BatchBytes`, `BatchTimeout` in `internal/kafka/client.go`
- Sorters
  - Chunk size: `chunkSize` (default 1,000,000) in `internal/sort/external_sort.go`
  - Temp directory: per-key under `/tmp` (disk speed matters)
- Kafka
  - Partitions: topics created with 3 partitions (adjust in `scripts/run.sh`)
  - Compression: Snappy enabled in producer writer
- Docker Resources
  - `mem_limit` and `cpus` for `pipeline_app` in `docker-compose.yml`

## Bottleneck Analysis
- Disk I/O during chunk spill and merge can dominate runtime
- Kafka broker throughput and network bandwidth may limit producer speed
- Heap pressure if chunk size too large; tune chunk size based on available memory

## Scaling Strategy
- Partition source topic and run multiple producer/sorter instances
- Use multiple disks or faster SSDs/NVMe for temp files
- For cross-machine scaling, leverage a distributed framework (Spark, Flink) to coordinate partitioned sorts and merges

## Troubleshooting
- No output when consuming in PowerShell: use single quotes and avoid piping to `cat`, or use Git Bash. Add `--timeout-ms 10000` to auto-exit if topic is empty.
- `pipeline_app` not listed in `docker compose ps`: it’s an ephemeral run container; invoke it via `docker compose run --rm pipeline_app ./producer` or `./sorter ...`.
- If pulls fail for images, ensure network access to Docker Hub or use the Confluent images configured in `docker-compose.yml`.

## Bonus Notes
### Idiomatic Go Style
- Clear package layout: `cmd/` for binaries, `internal/` for libraries (data, kafka, sort)
- Descriptive names and small focused functions
- Avoid unnecessary allocations (e.g., `strings.Builder`, integer parsing without `fmt`)
- Guard clauses and early returns; explicit error handling

### Bottleneck Analysis
- Disk I/O dominates during external sort (spill and k‑way merge)
  - Mitigation: larger chunk size to reduce number of files; buffered I/O; fast SSD/NVMe
- Kafka throughput (broker and network) is secondary
  - Mitigation: batching (`BatchSize/Bytes/Timeout`), compression (Snappy), least‑bytes balancing
- CPU is usually not the limiter; GC pressure minimized via pooling and builders

### What We Tuned to Make It Faster (Code‑level)
- Producer
  - Increased worker pool to `runtime.NumCPU()*3` and queue to `100_000` to better saturate generation
  - Kafka writer batching raised to `BatchSize=10000`, `BatchBytes=16MB`, `BatchTimeout=150ms`
- Reader
  - Larger fetch sizes: `MinBytes=1MB`, `MaxBytes=32MB` for fewer round trips
- External Sort I/O
  - Increased spill/merge buffers from 1MiB to 4MiB to cut syscall overhead
- Comparator
  - Numeric id comparison without conversions; string field slicing without allocations

### If We Had More Data and More Machines
- Horizontal scale with Kafka partitions: shard by key, run multiple sorters per partition
- Multi-disk temp directories and parallel merges per disk to increase IOPS
- Cluster/distributed sort using Spark/Flink: map (local chunk sort) + reduce (global merge) per partition
- Use object storage (e.g., S3) for chunk spill and distributed k‑way merge coordinators


