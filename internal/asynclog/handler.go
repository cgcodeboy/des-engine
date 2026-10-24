// Package asynclog queues slog records and writes them on a background goroutine.
package asynclog

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	queueCap     = 4096
	closeTimeout = 2 * time.Second
)

// Logger is a non-blocking slog front end.
type Logger struct {
	h *handler
}

// New starts a writer goroutine. A nil sink uses slog.Default.
func New(sink slog.Handler) *Logger {
	if sink == nil {
		sink = slog.Default().Handler()
	}
	s := &shared{
		ch:   make(chan item, queueCap),
		done: make(chan struct{}),
	}
	h := &handler{inner: sink, s: s}
	go s.loop()
	return &Logger{h: h}
}

// Handler returns the slog.Handler that enqueues records.
func (l *Logger) Handler() slog.Handler {
	return l.h
}

// Dropped returns how many records were discarded.
func (l *Logger) Dropped() uint64 {
	return l.h.s.dropped.Load()
}

// Close stops new records and waits up to 2 seconds for queued records to flush.
func (l *Logger) Close() {
	s := l.h.s
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.ch)
	s.mu.Unlock()

	timer := time.NewTimer(closeTimeout)
	defer timer.Stop()
	select {
	case <-s.done:
	case <-timer.C:
		s.dropped.Add(uint64(len(s.ch)) + 1)
	}
}

type item struct {
	ctx  context.Context
	rec  slog.Record
	sink slog.Handler
}

type shared struct {
	mu      sync.Mutex
	ch      chan item
	dropped atomic.Uint64
	closed  bool
	done    chan struct{}
}

func (s *shared) loop() {
	defer close(s.done)
	for it := range s.ch {
		_ = it.sink.Handle(it.ctx, it.rec)
	}
}

type handler struct {
	inner slog.Handler
	s     *shared
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	h.s.mu.Lock()
	closed := h.s.closed
	h.s.mu.Unlock()
	if closed {
		return false
	}
	return h.inner.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	rec := r.Clone()
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if h.s.closed {
		h.s.dropped.Add(1)
		return nil
	}
	select {
	case h.s.ch <- item{ctx: ctx, rec: rec, sink: h.inner}:
	default:
		h.s.dropped.Add(1)
	}
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs), s: h.s}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), s: h.s}
}
