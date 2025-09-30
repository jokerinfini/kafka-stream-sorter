package main

import (
	"context"
	"fmt"
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
	start := time.Now()
	brokers := getenv("KAFKA_BROKERS", "kafka:9092")
	sourceTopic := getenv("SOURCE_TOPIC", "source")

	writer := kclient.NewWriter([]string{brokers}, sourceTopic)
	defer writer.Close()

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
			fmt.Fprintf(os.Stderr, "write error: %v\n", err)
		}
		if sent%1_000_000 == 0 {
			fmt.Printf("Produced %d records\n", sent)
		}
	}

	close(records)
	wg.Wait()
	fmt.Printf("Generation + production completed in %s\n", time.Since(start))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
