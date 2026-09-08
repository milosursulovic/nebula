package outbox

import (
	"context"
	"log/slog"
	"time"

	kafka "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("nebula-outbox")

// Topic is the single Kafka topic all domain events publish to. Spec's
// event list (section 22) is small enough (11 named types, 7 wired this
// phase) that per-event-type topics would be premature; consumers filter
// on the event_type header instead.
const Topic = "nebula-events"

const (
	PublisherPollInterval = time.Second
	PublisherBatchSize    = 20
)

// KafkaWriter is the narrow capability Publisher needs — satisfied by
// *kafka.Writer, faked in tests without a real broker.
type KafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// Publisher drains outbox_events into Kafka (spec section 23's "Outbox
// Publisher: read unpublished events -> publish Kafka -> mark published"),
// same dispatcher-loop shape as node.Monitor / job.Pool's dispatcher.
type Publisher struct {
	repo   Repository
	writer KafkaWriter
	logger *slog.Logger
}

func NewPublisher(repo Repository, writer KafkaWriter, logger *slog.Logger) *Publisher {
	return &Publisher{repo: repo, writer: writer, logger: logger}
}

func (p *Publisher) Run(ctx context.Context) {
	ticker := time.NewTicker(PublisherPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Publisher) tick(ctx context.Context) {
	events, err := p.repo.ListUnpublished(ctx, PublisherBatchSize)
	if err != nil {
		p.logger.Error("outbox publisher: list unpublished failed", "error", err)
		return
	}

	for _, e := range events {
		// A fresh trace root per publish (spec section 36's "Kafka" hop)
		// — not chained back to whatever request originally caused the
		// domain event, since that request may be long gone by the time
		// this ticks; see the Phase 14 plan's scope note on why that's
		// an honest simplification, not a gap.
		spanCtx, span := tracer.Start(ctx, "outbox.publish")

		msg := kafka.Message{
			Topic: Topic,
			Key:   []byte(e.AggregateID),
			Value: e.Payload,
			Headers: []kafka.Header{
				{Key: "event_id", Value: []byte(e.ID)},
				{Key: "event_type", Value: []byte(e.EventType)},
				{Key: "aggregate_type", Value: []byte(e.AggregateType)},
				{Key: "aggregate_id", Value: []byte(e.AggregateID)},
			},
		}
		otel.GetTextMapPropagator().Inject(spanCtx, KafkaHeaderCarrier{Headers: &msg.Headers})

		if err := p.writer.WriteMessages(ctx, msg); err != nil {
			p.logger.Error("outbox publisher: publish failed, will retry next tick", "event_id", e.ID, "error", err)
			span.RecordError(err)
			span.End()
			continue // leave unpublished; next tick retries (at-least-once)
		}
		span.End()

		if err := p.repo.MarkPublished(ctx, e.ID); err != nil {
			p.logger.Error("outbox publisher: mark published failed", "event_id", e.ID, "error", err)
			continue
		}

		p.logger.Info("published domain event", "event_id", e.ID, "event_type", e.EventType)
	}
}
