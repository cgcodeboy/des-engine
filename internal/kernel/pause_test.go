package kernel

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestPauseDrainsMillisecondThenResume(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	started := make(chan struct{})
	release := make(chan struct{})
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
	mustAdd(t, eng, 0, "a", func(Context) error {
		close(started)
		<-release
		mu.Lock()
		order = append(order, "a")
		mu.Unlock()
		return nil
	})
	mustAdd(t, eng, 0, "b", note("b"))
	mustAdd(t, eng, 5, "c", note("c"))
	mustStart(t, eng)
	<-started
	done := make(chan error, 1)
	go func() { done <- eng.Pause() }()
	waitUntil(t, func() bool { return eng.Snapshot().State == "pausing" })
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("order at pause = %v", got)
	}
	if eng.Snapshot().State != "paused" {
		t.Fatalf("state = %s", eng.Snapshot().State)
	}
	if err := eng.Resume(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 3
	})
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeShiftsOrigin(t *testing.T) {
	eng, clk := testEngine(t, true, WithExitWhenIdle(false))
	ran := make(chan struct{})
	mustAdd(t, eng, 100, "late", func(Context) error {
		close(ran)
		return nil
	})
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5 * time.Second)
	if err := eng.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
		t.Fatal("event ran during the paused wall gap")
	case <-time.After(30 * time.Millisecond):
	}
	clk.Advance(100 * time.Millisecond)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("event did not run at the shifted target")
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointIsExplicitAndRepeatable(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Handle("step", func(Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(1, Event{Name: "step", Payload: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	first, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("repeated checkpoint changed")
	}
	if _, err := eng.AddEvent(2, Event{Name: "step", Payload: []byte("two")}); err != nil {
		t.Fatal(err)
	}
	third, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(third) {
		t.Fatal("checkpoint did not observe the new event")
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreBindsRegisteredHandler(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	var got []byte
	handler := func(c Context) error {
		got = append([]byte(nil), c.Payload()...)
		return nil
	}
	if err := eng.Handle("step", handler); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(1, Event{Name: "step", Payload: []byte("pay")}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	data, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	bare, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := bare.Restore(context.Background(), data); err != ErrHandlerName {
		t.Fatalf("restore = %v", err)
	}
	if bare.Snapshot().State != "created" {
		t.Fatalf("state after failed restore = %s", bare.Snapshot().State)
	}
	if err := bare.Handle("step", handler); err != nil {
		t.Fatal(err)
	}
	if err := bare.Restore(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if bare.Snapshot().State != "paused" {
		t.Fatalf("state = %s", bare.Snapshot().State)
	}
	if err := bare.Resume(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return len(got) == 3 })
	if string(got) != "pay" {
		t.Fatalf("payload = %q", got)
	}
	if err := bare.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointRejectsUnregisteredName(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	mustAdd(t, eng, 1, "loose", func(Context) error { return nil })
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Checkpoint(); err != ErrHandlerName {
		t.Fatalf("checkpoint = %v", err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreContinuesRandomSequence(t *testing.T) {
	ctrl := rand.New(rand.NewPCG(1, 1))
	wantFirst := ctrl.Uint64()
	wantSecond := ctrl.Uint64()
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	var first uint64
	var second uint64
	started := make(chan struct{})
	release := make(chan struct{})
	if err := eng.Handle("draw", func(c Context) error {
		if first == 0 {
			first = c.Rand().Uint64()
			close(started)
			<-release
			return nil
		}
		second = c.Rand().Uint64()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(0, Event{Name: "draw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(1, Event{Name: "draw"}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	<-started
	done := make(chan error, 1)
	go func() { done <- eng.Pause() }()
	waitUntil(t, func() bool { return eng.Snapshot().State == "pausing" })
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Fatal("second draw ran before pause")
	}
	data, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	next, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := next.Handle("draw", func(c Context) error {
		second = c.Rand().Uint64()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := next.Restore(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if err := next.Resume(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return second != 0 })
	if first != wantFirst || second != wantSecond {
		t.Fatalf("draws = %d, %d; want %d, %d", first, second, wantFirst, wantSecond)
	}
	if err := next.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestPauseBeforeStart(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Pause(); err != ErrNotRunning {
		t.Fatalf("pause = %v", err)
	}
	if err := eng.Resume(); err != ErrNotPaused {
		t.Fatalf("resume = %v", err)
	}
}

func TestStopFromPaused(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	mustAdd(t, eng, 1, "later", func(Context) error { return nil })
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	if eng.Snapshot().State != "stopped" {
		t.Fatalf("state = %s", eng.Snapshot().State)
	}
}

func TestCancelWhilePaused(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	ctx, cancel := context.WithCancel(context.Background())
	mustAdd(t, eng, 1, "later", func(Context) error { return nil })
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if eng.Snapshot().State != "stopped" {
		t.Fatalf("state = %s", eng.Snapshot().State)
	}
}

func TestPauseWhilePausedAndCheckpointTiming(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if _, err := eng.Checkpoint(); err != ErrNotPaused {
		t.Fatalf("checkpoint before start = %v", err)
	}
	mustStart(t, eng)
	if _, err := eng.Checkpoint(); err != ErrNotPaused {
		t.Fatalf("checkpoint while running = %v", err)
	}
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Checkpoint(); err != ErrNotPaused {
		t.Fatalf("checkpoint after stop = %v", err)
	}
}

func TestAddWhilePausedRunsAfterResume(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	started := make(chan struct{})
	release := make(chan struct{})
	got := make(chan string, 1)
	if err := eng.Handle("step", func(c Context) error {
		got <- string(c.Payload())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, eng, 0, "hold", func(Context) error {
		close(started)
		<-release
		return nil
	})
	mustStart(t, eng)
	<-started
	done := make(chan error, 1)
	go func() { done <- eng.Pause() }()
	waitUntil(t, func() bool { return eng.Snapshot().State == "pausing" })
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(0, Event{Name: "step", Payload: []byte("old")}); err != ErrPast {
		t.Fatalf("past insert = %v", err)
	}
	if _, err := eng.AddFuture(-time.Millisecond, Event{Name: "step"}); err != ErrPast {
		t.Fatalf("negative delay = %v", err)
	}
	if eng.Snapshot().EventsRejected != 2 {
		t.Fatalf("rejected = %d", eng.Snapshot().EventsRejected)
	}
	if _, err := eng.AddEvent(2, Event{Name: "step", Payload: []byte("later")}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != "later" {
			t.Fatalf("payload = %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event added during pause did not run")
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointCopiesStayIndependent(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Handle("step", func(Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(1, Event{Name: "step", Payload: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	first, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddEvent(2, Event{Name: "step", Payload: []byte("two")}); err != nil {
		t.Fatal(err)
	}
	second, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	early := restoreAndRun(t, first)
	if len(early) != 1 || early[0] != "one" {
		t.Fatalf("first checkpoint ran %v", early)
	}
	later := restoreAndRun(t, second)
	if len(later) != 2 || later[0] != "one" || later[1] != "two" {
		t.Fatalf("second checkpoint ran %v", later)
	}
}

func TestCanceledEventIsOmittedFromCheckpoint(t *testing.T) {
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Handle("step", func(Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	keep, err := eng.AddEvent(1, Event{Name: "step", Payload: []byte("keep")})
	if err != nil {
		t.Fatal(err)
	}
	if keep == 0 {
		t.Fatal("missing id")
	}
	drop, err := eng.AddEvent(1, Event{Name: "step", Payload: []byte("drop")})
	if err != nil {
		t.Fatal(err)
	}
	if !eng.Cancel(drop) {
		t.Fatal("cancel failed")
	}
	if eng.Snapshot().EventsCancelled != 0 {
		t.Fatalf("live cancelled = %d", eng.Snapshot().EventsCancelled)
	}
	data, err := eng.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	next, _ := testEngine(t, false, WithExitWhenIdle(false))
	got := make(chan string, 2)
	if err := next.Handle("step", func(c Context) error {
		got <- string(c.Payload())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := next.Restore(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if next.Snapshot().EventsCancelled != 1 {
		t.Fatalf("restored cancelled = %d", next.Snapshot().EventsCancelled)
	}
	if next.Snapshot().EventsPending != 1 {
		t.Fatalf("restored pending = %d", next.Snapshot().EventsPending)
	}
	if err := next.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != "keep" {
			t.Fatalf("ran %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kept event did not run")
	}
	select {
	case s := <-got:
		t.Fatalf("canceled event ran: %q", s)
	case <-time.After(30 * time.Millisecond):
	}
	if err := next.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsBadInput(t *testing.T) {
	src, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := src.Handle("step", func(Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := src.AddEvent(1, Event{Name: "step"}); err != nil {
		t.Fatal(err)
	}
	mustStart(t, src)
	if err := src.Pause(); err != nil {
		t.Fatal(err)
	}
	data, err := src.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Stop(); err != nil {
		t.Fatal(err)
	}

	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Restore(nil, data); err == nil || err.Error() != "nil context" {
		t.Fatalf("nil context = %v", err)
	}
	if err := eng.Restore(context.Background(), []byte("not a checkpoint")); err != ErrCheckpoint {
		t.Fatalf("garbage = %v", err)
	}
	if eng.Snapshot().State != "created" {
		t.Fatalf("state = %s", eng.Snapshot().State)
	}
	if err := eng.Handle("step", func(Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mustStart(t, eng)
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Restore(context.Background(), data); err != ErrStarted {
		t.Fatalf("restore after stop = %v", err)
	}
	if eng.Snapshot().State != "stopped" {
		t.Fatalf("state = %s", eng.Snapshot().State)
	}
}

func restoreAndRun(t *testing.T, data []byte) []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	eng, _ := testEngine(t, false, WithExitWhenIdle(false))
	if err := eng.Handle("step", func(c Context) error {
		mu.Lock()
		got = append(got, string(c.Payload()))
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Restore(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if err := eng.Resume(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0 && eng.Snapshot().EventsPending == 0
	})
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), got...)
}
