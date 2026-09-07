package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	kafka "github.com/segmentio/kafka-go"
)

// KafkaReader is the narrow capability Consumer needs — satisfied by
// *kafka.Reader, faked in tests without a real broker.
type KafkaReader interface {
	ReadMessage(ctx context.Context) (kafka.Message, error)
}

// Consumer reads domain events from Kafka and writes them into audit_logs
// (spec section 38), idempotently (spec section 22).
type Consumer struct {
	repo   Repository
	reader KafkaReader
	logger *slog.Logger
}

func NewConsumer(repo Repository, reader KafkaReader, logger *slog.Logger) *Consumer {
	return &Consumer{repo: repo, reader: reader, logger: logger}
}

// Run blocks, consuming until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("audit consumer: read message failed", "error", err)
			continue
		}
		c.handle(ctx, msg)
	}
}

func header(msg kafka.Message, key string) string {
	for _, h := range msg.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *Consumer) handle(ctx context.Context, msg kafka.Message) {
	eventType := header(msg, "event_type")
	action, ok := ActionFor(eventType)
	if !ok {
		return // an event type this consumer doesn't map yet — skip, not an error
	}

	eventID := header(msg, "event_id")
	aggregateType := header(msg, "aggregate_type")
	aggregateID := header(msg, "aggregate_id")

	var payload struct {
		TenantID *string `json:"tenant_id"`
	}
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		c.logger.Error("audit consumer: parse payload failed", "event_id", eventID, "error", err)
		return
	}

	inserted, err := c.repo.InsertIdempotent(ctx, Record{
		EventID:      eventID,
		TenantID:     payload.TenantID,
		Action:       action,
		ResourceType: aggregateType,
		ResourceID:   aggregateID,
		Metadata:     msg.Value,
	})
	if err != nil {
		c.logger.Error("audit consumer: insert failed", "event_id", eventID, "error", err)
		return
	}

	if inserted {
		c.logger.Info("audit record written", "event_id", eventID, "action", action)
	} else {
		c.logger.Info("audit record already recorded, skipped duplicate delivery", "event_id", eventID, "action", action)
	}
}
