package kafka

import (
	"context"
	"time"

	gokafka "github.com/segmentio/kafka-go"
)

func NewWriter(brokers []string, topic string) *gokafka.Writer {
	return &gokafka.Writer{
		Addr:         gokafka.TCP(brokers...),
		Topic:        topic,
		RequiredAcks: gokafka.RequireOne,
		Balancer:     &gokafka.LeastBytes{},
		Async:        true, // Async for better throughput
		BatchTimeout: 150 * time.Millisecond,
		BatchSize:    10000,
		BatchBytes:   16 * 1024 * 1024, // 16MB
		Compression:  gokafka.Snappy,
	}
}

func NewReader(brokers []string, topic string, groupID string) *gokafka.Reader {
	return gokafka.NewReader(gokafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1 * 1024 * 1024,  // 1MB
		MaxBytes:       32 * 1024 * 1024, // 32MB
		CommitInterval: time.Second,
		StartOffset:    gokafka.FirstOffset,
		// Ensure all partitions are assigned to this single consumer
		GroupBalancers: []gokafka.GroupBalancer{
			gokafka.RangeGroupBalancer{},
		},
	})
}

func CloseWriter(ctx context.Context, w *gokafka.Writer) error {
	if w == nil {
		return nil
	}
	// attempt a flush by writing zero messages (no-op) then close
	_ = w.Close()
	return nil
}
