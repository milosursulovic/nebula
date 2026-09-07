package audit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	kafka "github.com/segmentio/kafka-go"

	"github.com/milosursulovic/nebula/internal/outbox"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func msgWithHeaders(eventID, eventType, aggregateType, aggregateID string, value []byte) kafka.Message {
	return kafka.Message{
		Value: value,
		Headers: []kafka.Header{
			{Key: "event_id", Value: []byte(eventID)},
			{Key: "event_type", Value: []byte(eventType)},
			{Key: "aggregate_type", Value: []byte(aggregateType)},
			{Key: "aggregate_id", Value: []byte(aggregateID)},
		},
	}
}

func TestHandleKnownEventWritesRecord(t *testing.T) {
	repo := newFakeRepository()
	c := NewConsumer(repo, nil, testLogger())

	msg := msgWithHeaders("event-1", outbox.EventInstanceCreated, outbox.AggregateInstance, "instance-1",
		[]byte(`{"tenant_id":"tenant-1","instance_id":"instance-1"}`))

	c.handle(context.Background(), msg)

	rec, ok := repo.byEvent["event-1"]
	if !ok {
		t.Fatal("expected a record to be inserted")
	}
	if rec.Action != "INSTANCE_CREATED" || rec.ResourceType != "instance" || rec.ResourceID != "instance-1" {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.TenantID == nil || *rec.TenantID != "tenant-1" {
		t.Errorf("TenantID = %v, want tenant-1", rec.TenantID)
	}
}

func TestHandleUnknownEventTypeSkipped(t *testing.T) {
	repo := newFakeRepository()
	c := NewConsumer(repo, nil, testLogger())

	msg := msgWithHeaders("event-1", outbox.EventNetworkCreated, "network", "network-1", []byte(`{}`))
	c.handle(context.Background(), msg)

	if repo.count() != 0 {
		t.Errorf("expected unknown event type to be skipped, got %d records", repo.count())
	}
}

func TestHandleDuplicateEventIDIdempotent(t *testing.T) {
	repo := newFakeRepository()
	c := NewConsumer(repo, nil, testLogger())

	msg := msgWithHeaders("event-1", outbox.EventNodeOffline, outbox.AggregateNode, "node-1", []byte(`{"tenant_id":null}`))

	c.handle(context.Background(), msg)
	c.handle(context.Background(), msg) // simulated redelivery

	if repo.count() != 1 {
		t.Errorf("expected exactly 1 record after duplicate delivery, got %d", repo.count())
	}
}

func TestHandleMalformedPayloadSkipsWithoutPanic(t *testing.T) {
	repo := newFakeRepository()
	c := NewConsumer(repo, nil, testLogger())

	msg := msgWithHeaders("event-1", outbox.EventInstanceCreated, outbox.AggregateInstance, "instance-1", []byte(`not-json`))
	c.handle(context.Background(), msg) // must not panic

	if repo.count() != 0 {
		t.Errorf("expected no record for malformed payload, got %d", repo.count())
	}
}

type fakeKafkaReader struct{}

func (f *fakeKafkaReader) ReadMessage(ctx context.Context) (kafka.Message, error) {
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func TestRunStopsOnContextCancel(t *testing.T) {
	repo := newFakeRepository()
	c := NewConsumer(repo, &fakeKafkaReader{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
