package main

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCloseWithTimeoutReturnsPromptlyOnFastClose(t *testing.T) {
	start := time.Now()
	closeWithTimeout(testLogger(), "fast", func() error { return nil }, time.Second)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("closeWithTimeout took %v for an instantly-returning close, want near-instant", elapsed)
	}
}

func TestCloseWithTimeoutLogsCloseError(t *testing.T) {
	// Just confirms it doesn't panic/block on an error — the log line
	// itself isn't asserted (testLogger discards output), the point is
	// closeWithTimeout must still return once closeFn returns any value.
	done := make(chan struct{})
	go func() {
		closeWithTimeout(testLogger(), "erroring", func() error { return errors.New("boom") }, time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closeWithTimeout did not return after closeFn returned an error")
	}
}

func TestCloseWithTimeoutStopsWaitingAfterTimeout(t *testing.T) {
	start := time.Now()
	blocked := make(chan struct{})
	closeWithTimeout(testLogger(), "slow", func() error {
		<-blocked // never actually closes within the test
		return nil
	}, 50*time.Millisecond)

	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("closeWithTimeout returned before its timeout elapsed: %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("closeWithTimeout waited far longer than its timeout: %v", elapsed)
	}
	close(blocked) // let the background goroutine finish, avoid leaking it past the test
}
