package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	kafka "github.com/segmentio/kafka-go"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeKafkaWriter struct {
	mu       sync.Mutex
	written  []kafka.Message
	failNext bool
}

func (f *fakeKafkaWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("simulated broker failure")
	}
	f.written = append(f.written, msgs...)
	return nil
}

func (f *fakeKafkaWriter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.written)
}

func TestPublisherTickPublishesAndMarks(t *testing.T) {
	repo := newFakeRepository()
	writer := &fakeKafkaWriter{}
	p := NewPublisher(repo, writer, testLogger())

	e, err := newEvent(EventInstanceCreated, AggregateInstance, "instance-1", nil)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	repo.add(e)

	p.tick(context.Background())

	if writer.count() != 1 {
		t.Fatalf("writer got %d messages, want 1", writer.count())
	}

	unpublished, err := repo.ListUnpublished(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListUnpublished: %v", err)
	}
	if len(unpublished) != 0 {
		t.Errorf("expected no unpublished events left, got %d", len(unpublished))
	}
}

func TestPublisherTickLeavesEventUnpublishedOnWriteFailure(t *testing.T) {
	repo := newFakeRepository()
	writer := &fakeKafkaWriter{failNext: true}
	p := NewPublisher(repo, writer, testLogger())

	e, err := newEvent(EventInstanceCreated, AggregateInstance, "instance-1", nil)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	repo.add(e)

	p.tick(context.Background())

	unpublished, err := repo.ListUnpublished(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListUnpublished: %v", err)
	}
	if len(unpublished) != 1 {
		t.Fatalf("expected event to remain unpublished after write failure, got %d unpublished", len(unpublished))
	}

	// Next tick (write succeeds now) should publish it — at-least-once retry.
	p.tick(context.Background())
	if writer.count() != 1 {
		t.Fatalf("writer got %d messages after retry, want 1", writer.count())
	}
	unpublished, _ = repo.ListUnpublished(context.Background(), 10)
	if len(unpublished) != 0 {
		t.Errorf("expected event published after retry, still have %d unpublished", len(unpublished))
	}
}

func TestPublisherSkipsAlreadyPublished(t *testing.T) {
	repo := newFakeRepository()
	writer := &fakeKafkaWriter{}
	p := NewPublisher(repo, writer, testLogger())

	e, err := newEvent(EventInstanceCreated, AggregateInstance, "instance-1", nil)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	repo.add(e)

	p.tick(context.Background())
	p.tick(context.Background()) // nothing left to publish

	if writer.count() != 1 {
		t.Errorf("writer got %d messages, want exactly 1 (no republish of already-published event)", writer.count())
	}
}
