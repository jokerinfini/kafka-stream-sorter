package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // Enable pprof profiling endpoints
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	kclient "core-infra-project/internal/kafka"
	extSort "core-infra-project/internal/sort"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: sorter [id|name|continent]")
		os.Exit(1)
	}
	key := strings.ToLower(os.Args[1])
	sortIdx := map[string]int{"id": 0, "name": 1, "continent": 3}[key]
	if sortIdx == 0 && key != "id" {
		fmt.Println("invalid key; must be id, name, or continent")
		os.Exit(1)
	}

	// Start pprof HTTP server for profiling (requirement #6)
	// Each sorter uses a different port to avoid conflicts
	pprofPort := fmt.Sprintf("0.0.0.0:%d", 6061+sortIdx)
	go func() {
		log.Printf("[pprof] Profiling server for '%s' sorter starting on %s\n", key, pprofPort)
		log.Println(http.ListenAndServe(pprofPort, nil))
	}()

	fmt.Printf("[Sorter:%s] Starting external sort pipeline...\n", key)

	brokers := getenv("KAFKA_BROKERS", "kafka:9092")
	sourceTopic := getenv("SOURCE_TOPIC", "source")
	destTopic := map[string]string{
		"id":        getenv("TOPIC_ID", "sorted_id"),
		"name":      getenv("TOPIC_NAME", "sorted_name"),
		"continent": getenv("TOPIC_CONTINENT", "sorted_continent"),
	}[key]

	// Use a unique consumer group per run to start from earliest offsets (fresh group)
	uniqueGroup := "sorter-" + key + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	fmt.Printf("  - Consumer group: %s\n", uniqueGroup)
	reader := kclient.NewReader([]string{brokers}, sourceTopic, uniqueGroup)
	writer := kclient.NewWriter([]string{brokers}, destTopic)
	defer reader.Close()
	defer writer.Close()

	tempDir := filepath.Join(os.TempDir(), "extsort_"+key)

	fmt.Printf("[Sorter:%s] Configuration:\n", key)
	fmt.Printf("  - Source topic: %s\n", sourceTopic)
	fmt.Printf("  - Destination topic: %s\n", destTopic)
	fmt.Printf("  - Temp directory: %s\n", tempDir)
	fmt.Printf("  - Sort key: %s (index: %d)\n", key, sortIdx)

	start := time.Now()
	if err := extSort.ExternalSort(reader, writer, sortIdx, tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Sort error: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	fmt.Printf("\n[Summary] Sorter '%s' completed successfully in %v\n", key, duration)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
