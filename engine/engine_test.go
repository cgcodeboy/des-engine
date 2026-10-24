package engine

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*manualTimer
}

type manualTimer struct {
	clock    *manualClock
	deadline time.Time
	fn       func()
	stopped  bool
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) AfterFunc(d time.Duration, f func()) stopTimer {
	c.mu.Lock()
	deadline := c.now.Add(d)
	if d <= 0 || !c.now.Before(deadline) {
		c.mu.Unlock()
		go f()
		return noopTimer{}
	}
	timer := &manualTimer{clock: c, deadline: deadline, fn: f}
	c.waiters = append(c.waiters, timer)
	c.mu.Unlock()
	return timer
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	kept := c.waiters[:0]
	for _, timer := range c.waiters {
		if timer.stopped {
			continue
		}
		if !c.now.Before(timer.deadline) {
			timer.stopped = true
			due = append(due, timer.fn)
			continue
		}
		kept = append(kept, timer)
	}
	c.waiters = kept
	c.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

type noopTimer struct{}

func (noopTimer) Stop() bool { return false }

type memLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *memLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *memLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *memLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *memLog) WithGroup(string) slog.Handler      { return h }

func (h *memLog) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

func testEngine(t *testing.T, realtime bool, extra ...Option) (*Engine, *manualClock) {
	t.Helper()
	clk := &manualClock{now: time.Unix(1_700_000_000, 0).UTC()}
	opts := []Option{
		WithRealtime(realtime),
		WithOrigin(clk.now),
		WithLogger(slog.New(slog.DiscardHandler)),
		withClock(clk),
		WithExitWhenIdle(true),
	}
	opts = append(opts, extra...)
	eng, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return eng, clk
}

func nop(string) Handler {
	return func(Context) error { return nil }
}

func record(order *[]string, name string) Handler {
	return func(Context) error {
		*order = append(*order, name)
		return nil
	}
}

func TestFutureBucketsRunInTimeOrder(t *testing.T) {
	eng, _ := testEngine(t, false)
	var order []string
	mustAdd(t, eng, 2, "second", record(&order, "second"))
	mustAdd(t, eng, 0, "first", record(&order, "first"))
	mustAdd(t, eng, 2, "second-again", record(&order, "second-again"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "second-again"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestSameStepAppendsToTail(t *testing.T) {
	eng, _ := testEngine(t, false)
	var order []string
	_, err := eng.AddEvent(5, Event{Name: "head", Handler: func(ctx Context) error {
		order = append(order, "head")
		_, err := ctx.AddFuture(0, Event{Name: "tail", Handler: record(&order, "tail")})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, eng, 6, "next", record(&order, "next"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	want := []string{"head", "tail", "next"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("order = %v", order)
	}
}

func TestPastEventRejected(t *testing.T) {
	eng, _ := testEngine(t, false)
	_, err := eng.AddEvent(5, Event{Name: "now", Handler: func(ctx Context) error {
		_, err := ctx.AddEvent(4, Event{Name: "old", Handler: nop("old")})
		if !errors.Is(err, ErrPast) {
			t.Fatalf("err = %v", err)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if eng.Snapshot().EventsRejected != 1 {
		t.Fatalf("rejected = %d", eng.Snapshot().EventsRejected)
	}
}

func TestWatermarkAdvancesAfterStep(t *testing.T) {
	eng, _ := testEngine(t, false)
	_, err := eng.AddEvent(7, Event{Name: "later", Handler: func(ctx Context) error {
		if _, err := ctx.AddEvent(5, Event{Name: "gone", Handler: nop("gone")}); !errors.Is(err, ErrPast) {
			t.Fatalf("err = %v", err)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, eng, 5, "earlier", nop("earlier"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestEarlierBucketInterruptsWait(t *testing.T) {
	eng, clk := testEngine(t, true, WithExitWhenIdle(false))
	var mu sync.Mutex
	var order []string
	note := func(name string) Handler {
		return func(Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}
	mustAdd(t, eng, 10, "late", note("late"))
	mustStart(t, eng)
	waitUntil(t, func() bool {
		s := eng.Snapshot()
		return s.State == "running" && s.EventsPending == 1
	})
	mustAdd(t, eng, 3, "early", note("early"))
	clk.Advance(3 * time.Millisecond)
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 1
	})
	clk.Advance(7 * time.Millisecond)
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 2
	})
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "early" || order[1] != "late" {
		t.Fatalf("order = %v", order)
	}
}

func TestLagCallbackOnceWhenBehind(t *testing.T) {
	clk := &manualClock{now: time.Unix(1_700_000_000, 0).UTC()}
	origin := clk.now
	clk.Advance(5 * time.Millisecond)
	var calls int
	eng, err := New(
		WithRealtime(true),
		WithOrigin(origin),
		withClock(clk),
		WithExitWhenIdle(true),
		WithLogger(slog.New(slog.DiscardHandler)),
		WithLagHandler(func(Lag) { calls++ }),
	)
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, eng, 0, "late-step", nop("late-step"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("callbacks = %d", calls)
	}
	if eng.Snapshot().CatchupSteps != 1 {
		t.Fatalf("catchup = %d", eng.Snapshot().CatchupSteps)
	}
	if eng.Snapshot().Lag != 5*time.Millisecond {
		t.Fatalf("lag = %s", eng.Snapshot().Lag)
	}
}

func TestExternalInsertWakesIdleEngine(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	mustStart(t, eng)
	waitUntil(t, func() bool { return eng.Snapshot().State == "running" })
	mustAdd(t, eng, 1, "wake", nop("wake"))
	waitUntil(t, func() bool { return eng.Snapshot().EventsProcessed == 1 })
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelSkipsHandler(t *testing.T) {
	eng, _ := testEngine(t, false)
	id, err := eng.AddEvent(1, Event{Name: "skip", Handler: func(Context) error {
		t.Fatal("canceled event ran")
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !eng.Cancel(id) || eng.Cancel(id) {
		t.Fatal("cancel result")
	}
	mustAdd(t, eng, 2, "keep", nop("keep"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if eng.Snapshot().EventsCancelled != 1 || eng.Snapshot().EventsProcessed != 1 {
		t.Fatalf("snap = %+v", eng.Snapshot())
	}
}

func TestExitWhenIdleAndHorizon(t *testing.T) {
	eng, _ := testEngine(t, false)
	mustAdd(t, eng, 0, "only", nop("only"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}

	var ran []Time
	limited, _ := testEngine(t, false, WithHorizon(5))
	mustAdd(t, limited, 5, "edge", func(ctx Context) error {
		ran = append(ran, ctx.EventAt())
		return nil
	})
	mustAdd(t, limited, 6, "after", func(Context) error {
		t.Fatal("past horizon")
		return nil
	})
	mustStart(t, limited)
	if err := limited.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != 5 {
		t.Fatalf("ran = %v", ran)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	mustStart(t, eng)
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(1, Event{Name: "x", Handler: nop("x")}); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandlerErrorAndPanicStopEngine(t *testing.T) {
	eng, _ := testEngine(t, false)
	mustAdd(t, eng, 0, "bad", func(Context) error { return errors.New("boom") })
	mustStart(t, eng)
	err := eng.Wait()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}

	panicked, _ := testEngine(t, false)
	mustAdd(t, panicked, 0, "panic", func(Context) error { panic("x") })
	mustStart(t, panicked)
	err = panicked.Wait()
	if err == nil {
		t.Fatal("expected panic error")
	}
}

func TestSlowEventRecordedAndFastEventIgnored(t *testing.T) {
	logs := &memLog{}
	eng, clk := testEngine(t, false, WithLogger(slog.New(logs)))
	mustAdd(t, eng, 0, "rebuild", func(Context) error {
		clk.Advance(2 * time.Millisecond)
		return nil
	})
	mustAdd(t, eng, 1, "fast", nop("fast"))
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	snap := eng.Snapshot()
	if snap.SlowTotal != 1 || len(snap.SlowEvents) != 1 {
		t.Fatalf("slow = %+v", snap.SlowEvents)
	}
	if snap.SlowEvents[0].Name != "rebuild" || snap.SlowEvents[0].At != 0 {
		t.Fatalf("event = %+v", snap.SlowEvents[0])
	}
	if !logs.has("slow event") {
		t.Fatal("missing slow event log")
	}
}

func TestAddFutureFiresAtRoundedTime(t *testing.T) {
	eng, clk := testEngine(t, true, WithExitWhenIdle(false))
	var at Time
	_, err := eng.AddFuture(5*time.Millisecond, Event{Name: "later", Handler: func(ctx Context) error {
		at = ctx.EventAt()
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	waitUntil(t, func() bool { return eng.Snapshot().EventsPending == 1 })
	if at != 0 {
		t.Fatal("event ran before the delay")
	}
	clk.Advance(5 * time.Millisecond)
	waitUntil(t, func() bool { return eng.Snapshot().EventsProcessed == 1 })
	if at != 5 {
		t.Fatalf("at = %d", at)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	rounded, _ := testEngine(t, false)
	var got Time
	if _, err := rounded.AddFuture(1100*time.Microsecond, Event{Name: "ceil", Handler: func(ctx Context) error {
		got = ctx.EventAt()
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, rounded)
	if err := rounded.Wait(); err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("rounded at = %d", got)
	}
	if _, err := rounded.AddFuture(-time.Millisecond, Event{Name: "neg", Handler: nop("neg")}); !errors.Is(err, ErrPast) {
		t.Fatalf("err = %v", err)
	}
}

func TestZeroDelayAppendsAndEarlierFutureInterrupts(t *testing.T) {
	eng, _ := testEngine(t, false)
	var order []string
	_, err := eng.AddEvent(0, Event{Name: "head", Handler: func(ctx Context) error {
		order = append(order, "head")
		_, err := ctx.AddFuture(0, Event{Name: "zero", Handler: record(&order, "zero")})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[1] != "zero" {
		t.Fatalf("order = %v", order)
	}

	live, clk := testEngine(t, true, WithExitWhenIdle(false))
	var mu sync.Mutex
	var ran []string
	note := func(name string) Handler {
		return func(Context) error {
			mu.Lock()
			ran = append(ran, name)
			mu.Unlock()
			return nil
		}
	}
	mustAdd(t, live, 10, "late", note("late"))
	mustStart(t, live)
	waitUntil(t, func() bool { return live.Snapshot().EventsPending == 1 })
	if _, err := live.AddFuture(3*time.Millisecond, Event{Name: "soon", Handler: note("soon")}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(3 * time.Millisecond)
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(ran) >= 1
	})
	clk.Advance(7 * time.Millisecond)
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(ran) >= 2
	})
	if err := live.Stop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || ran[0] != "soon" || ran[1] != "late" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestContextCancel(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
}

func mustAdd(t *testing.T, eng *Engine, at Time, name string, h Handler) {
	t.Helper()
	if _, err := eng.AddEvent(at, Event{Name: name, Handler: h}); err != nil {
		t.Fatal(err)
	}
}

func mustStart(t *testing.T, eng *Engine) {
	t.Helper()
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}
