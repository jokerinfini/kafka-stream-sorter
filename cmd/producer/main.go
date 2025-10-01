package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // Enable pprof profiling endpoints
	"os"
	"runtime"
	"sync"
	"time"

	datagen "core-infra-project/internal/data"
	kclient "core-infra-project/internal/kafka"

	gokafka "github.com/segmentio/kafka-go"
)

const (
	totalRecords = 50_000_000
	// Larger queue smooths bursts between generators and writer
	queueSize = 100_000
)

func main() {
	// Start pprof HTTP server for profiling (requirement #6)
	// Access profiling at: http://localhost:6060/debug/pprof/
	go func() {
		log.Println("[pprof] Profiling server starting on :6060")
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()

	fmt.Println("[Producer] Starting generation and production pipeline...")
	start := time.Now()
	brokers := getenv("KAFKA_BROKERS", "kafka:9092")
	sourceTopic := getenv("SOURCE_TOPIC", "source")

	writer := kclient.NewWriter([]string{brokers}, sourceTopic)
	// Don't use defer - we'll explicitly close after wg.Wait() to ensure flush

	// Jobs channel to bound generation to exactly totalRecords
	jobs := make(chan struct{}, queueSize)
	records := make(chan []byte, queueSize)
	// Slightly higher concurrency to better saturate CPU when generating
	numWorkers := runtime.NumCPU() * 3

	var wg sync.WaitGroup
	// Generators
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for range jobs {
				rec := datagen.GenerateRandomRecord()
				records <- rec
			}
		}()
	}

	// Enqueue exactly totalRecords generation jobs
	go func() {
		for i := 0; i < totalRecords; i++ {
			jobs <- struct{}{}
		}
		close(jobs)
	}()

	// Publisher with batching
	fmt.Println("[Producer] Starting Kafka writes...")
	publishStart := time.Now()

	ctx := context.Background()
	sent := 0
	batch := make([]gokafka.Message, 0, 1000)

	for sent < totalRecords {
		// Collect batch
		batch = batch[:0]
		for len(batch) < cap(batch) && sent < totalRecords {
			msg := <-records
			batch = append(batch, gokafka.Message{Value: msg})
			sent++
		}
		if err := writer.WriteMessages(ctx, batch...); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Kafka write error: %v\n", err)
		}
		// Checkpoint logging every 1M records (requirement #4)
		if sent%1_000_000 == 0 {
			fmt.Printf("[Progress] Produced %d / %d records (%.1f%%)\n",
				sent, totalRecords, float64(sent)/float64(totalRecords)*100)
		}
	}

	close(records)
	wg.Wait()

	// Ensure all async writes are flushed before exiting
	fmt.Println("[Producer] Flushing remaining Kafka writes...")
	if err := writer.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to flush Kafka writer: %v\n", err)
	}

	publishDuration := time.Since(publishStart)
	totalDuration := time.Since(start)

	// Performance summary (requirement #7)
	fmt.Printf("\n[Summary] Producer completed successfully\n")
	fmt.Printf("  - Total records: %d\n", totalRecords)
	fmt.Printf("  - Total time: %v\n", totalDuration)
	fmt.Printf("  - Publish time: %v\n", publishDuration)
	fmt.Printf("  - Throughput: %.0f records/sec\n", float64(totalRecords)/totalDuration.Seconds())
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
