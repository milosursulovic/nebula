package outbox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// BenchmarkPublish times real kafka.Writer.WriteMessages throughput (spec
// section 47's "Kafka" benchmark target) — a throwaway topic, not the
// production "nebula-events" one. Gated on NEBULA_KAFKA_BROKERS (new gate,
// mirrors NEBULA_DATABASE_URL's shape) so `go test ./...` still passes
// standalone without Docker.
func BenchmarkPublish(b *testing.B) {
	brokers := os.Getenv("NEBULA_KAFKA_BROKERS")
	if brokers == "" {
		b.Skip("NEBULA_KAFKA_BROKERS not set; run `make compose-up` and re-run with it set (e.g. localhost:9092) to exercise this benchmark")
	}

	brokerList := strings.Split(brokers, ",")
	topic := fmt.Sprintf("nebula-bench-publish-%d", time.Now().UnixNano())
	if err := createTopic(brokerList, topic); err != nil {
		b.Fatalf("createTopic: %v", err)
	}

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokerList...),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
		// Matches cmd/nebula-api/main.go's production writer config —
		// this benchmark is what proved the default 1s BatchTimeout was
		// adding ~1s/op of pure waiting to every unbatched publish call
		// (exactly this shape: one message per WriteMessages call), which
		// is what led to setting this explicitly in production.
		BatchTimeout: 10 * time.Millisecond,
	}
	defer writer.Close()

	ctx := context.Background()
	payload := []byte(`{"event_type":"InstanceCreated","aggregate_id":"bench"}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := kafka.Message{
			Key:   []byte("bench-key"),
			Value: payload,
			Headers: []kafka.Header{
				{Key: "event_type", Value: []byte("InstanceCreated")},
			},
		}
		if err := writer.WriteMessages(ctx, msg); err != nil {
			b.Fatalf("WriteMessages: %v", err)
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
