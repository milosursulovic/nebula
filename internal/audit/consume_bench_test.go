package audit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// BenchmarkConsume times real kafka.Reader.ReadMessage throughput (spec
// section 47 names "Kafka consumer throughput" explicitly) — a throwaway
// topic and consumer group, not production's "nebula-events"/"nebula-
// audit". b.N+1 messages are published up front (setup, timer stopped);
// one is consumed before b.ResetTimer to pay the one-time consumer-group
// join cost (JoinGroup/SyncGroup — real, but a per-process startup cost,
// not a per-message one) outside the timed region, so the timed loop
// measures steady-state per-message consume/decode cost instead of
// mostly measuring how long group-join takes.
func BenchmarkConsume(b *testing.B) {
	brokers := os.Getenv("NEBULA_KAFKA_BROKERS")
	if brokers == "" {
		b.Skip("NEBULA_KAFKA_BROKERS not set; run `make compose-up` and re-run with it set (e.g. localhost:9092) to exercise this benchmark")
	}
	brokerList := strings.Split(brokers, ",")
	suffix := time.Now().UnixNano()
	topic := fmt.Sprintf("nebula-bench-consume-%d", suffix)
	group := fmt.Sprintf("nebula-bench-consume-group-%d", suffix)

	if err := createTopic(brokerList, topic); err != nil {
		b.Fatalf("createTopic: %v", err)
	}

	ctx := context.Background()
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokerList...),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
		BatchTimeout:           10 * time.Millisecond,
	}
	payload := []byte(`{"event_type":"InstanceCreated","aggregate_id":"bench"}`)
	msgs := make([]kafka.Message, b.N+1)
	for i := range msgs {
		msgs[i] = kafka.Message{Key: []byte("bench-key"), Value: payload}
	}
	if err := writer.WriteMessages(ctx, msgs...); err != nil {
		writer.Close()
		b.Fatalf("seed WriteMessages: %v", err)
	}
	writer.Close()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokerList, Topic: topic, GroupID: group,
		StartOffset: kafka.FirstOffset,
	})
	defer reader.Close()

	if _, err := reader.ReadMessage(ctx); err != nil {
		b.Fatalf("warmup ReadMessage (pays consumer-group join cost): %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reader.ReadMessage(ctx); err != nil {
			b.Fatalf("ReadMessage: %v", err)
		}
	}
}

// createTopic explicitly creates topic before benchmarking against it —
// relying on AllowAutoTopicCreation alone races the first WriteMessages
// call against the broker's own async topic creation ("Unknown Topic Or
// Partition" on that first call), so create it up front instead.
func createTopic(brokers []string, topic string) error {
	conn, err := kafka.Dial("tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
}
