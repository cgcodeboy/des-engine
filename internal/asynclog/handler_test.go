package asynclog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type gateHandler struct {
	mu      sync.Mutex
	records []slog.Record
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *gateHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *gateHandler) Handle(_ context.Context, r slog.Record) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}

func (h *gateHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *gateHandler) WithGroup(string) slog.Handler      { return h }

func (h *gateHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

type levelHandler struct {
	min     slog.Level
	mu      sync.Mutex
	records []string
}

func (h *levelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.min
}

func (h *levelHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *levelHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *levelHandler) WithGroup(string) slog.Handler      { return h }

func TestHandleReturnsBeforeWrite(t *testing.T) {
	sink := &gateHandler{entered: make(chan struct{}), release: make(chan struct{})}
	log := New(sink)
	logger := slog.New(log.Handler())
	done := make(chan struct{})
	go func() {
		logger.Info("queued")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Handle blocked on the sink")
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not reach the sink")
	}
	if sink.count() != 0 {
		t.Fatal("sink wrote before release")
	}
	close(sink.release)
	log.Close()
	if sink.count() != 1 {
		t.Fatalf("written = %d", sink.count())
	}
}

func TestFullQueueDrops(t *testing.T) {
	sink := &gateHandler{entered: make(chan struct{}), release: make(chan struct{})}
	log := New(sink)
	logger := slog.New(log.Handler())
	logger.Info("block")
	<-sink.entered
	for log.Dropped() == 0 {
		logger.Info("fill")
	}
	if log.Dropped() == 0 {
		t.Fatal("expected a dropped record")
	}
	close(sink.release)
	log.Close()
}

func TestCloseFlushes(t *testing.T) {
	sink := &levelHandler{min: slog.LevelInfo}
	log := New(sink)
	logger := slog.New(log.Handler())
	logger.Info("one")
	logger.Info("two")
	log.Close()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.records) != 2 {
		t.Fatalf("records = %v", sink.records)
	}
}

func TestDisabledLevelDoesNotQueue(t *testing.T) {
	sink := &levelHandler{min: slog.LevelWarn}
	log := New(sink)
	logger := slog.New(log.Handler())
	logger.Info("hidden")
	log.Close()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.records) != 0 || log.Dropped() != 0 {
		t.Fatalf("records=%v dropped=%d", sink.records, log.Dropped())
	}
}
