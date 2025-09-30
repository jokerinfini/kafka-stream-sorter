package main

import (
	"fmt"
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

	brokers := getenv("KAFKA_BROKERS", "kafka:9092")
	sourceTopic := getenv("SOURCE_TOPIC", "source")
	destTopic := map[string]string{
		"id":        getenv("TOPIC_ID", "sorted_id"),
		"name":      getenv("TOPIC_NAME", "sorted_name"),
		"continent": getenv("TOPIC_CONTINENT", "sorted_continent"),
	}[key]

	// Use a unique consumer group per run to start from earliest offsets (fresh group)
	uniqueGroup := "sorter-" + key + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	reader := kclient.NewReader([]string{brokers}, sourceTopic, uniqueGroup)
	writer := kclient.NewWriter([]string{brokers}, destTopic)
	defer reader.Close()
	defer writer.Close()

	tempDir := filepath.Join(os.TempDir(), "extsort_"+key)
	start := time.Now()
	if err := extSort.ExternalSort(reader, writer, sortIdx, tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "sort error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Sorter %s completed in %s\n", key, time.Since(start))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
