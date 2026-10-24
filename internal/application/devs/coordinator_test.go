package devs

import (
	"context"
	"github.com/cgcodeboy/des-engine/internal/kernel"
	"log/slog"
	"testing"
	"time"
)

func mustEngine(t *testing.T, idle bool) *kernel.Engine {
	t.Helper()
	eng, err := kernel.New(
		kernel.WithRealtime(false),
		kernel.WithExitWhenIdle(idle),
		kernel.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestRouteSameMillisecond(t *testing.T) {
	eng := mustEngine(t, true)
	var got []string
	root, err := NewCoordinator(eng, WithOutput(func(msg Message) {
		got = append(got, string(msg.Payload))
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("dock", &pulse{name: "a", gap: 0}); err != nil {
		t.Fatal(err)
	}
	if err := root.Add("mid", &relay{delay: 0}); err != nil {
		t.Fatal(err)
	}
	if err := root.Link("dock", "out", "mid", "in"); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("mid", "out", "done"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("output = %v", got)
	}
	stats := root.Snapshot().Coupled
	if stats == nil || stats.Models != 2 || stats.Messages != 2 || stats.Due < 2 {
		t.Fatalf("stats = %+v", stats)
	}
	if eng.Snapshot().Coupled != nil {
		t.Fatal("engine snapshot filled coupling counters")
	}
}

func TestDelayIsCeiled(t *testing.T) {
	eng := mustEngine(t, true)
	var when []kernel.Time
	root, err := NewCoordinator(eng, WithOutput(func(Message) {
		when = append(when, rootTime(eng))
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("hold", &pulse{name: "a", gap: 1500 * time.Microsecond}); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("hold", "out", "done"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(when) != 1 || when[0] != 2 {
		t.Fatalf("when = %v", when)
	}
}

func TestSelectPicksNamedModel(t *testing.T) {
	eng := mustEngine(t, true)
	var order []string
	root, err := NewCoordinator(eng,
		WithSelect(func(due []string) string {
			for _, name := range due {
				if name == "b" {
					return name
				}
			}
			return due[0]
		}),
		WithOutput(func(msg Message) {
			order = append(order, string(msg.Payload))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("a", &pulse{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := root.Add("b", &pulse{name: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("a", "out", "done"); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("b", "out", "done"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Fatalf("order = %v", order)
	}
	if root.Snapshot().Coupled.Selects != 1 {
		t.Fatalf("selects = %d", root.Snapshot().Coupled.Selects)
	}
}

func TestInjectReschedules(t *testing.T) {
	eng := mustEngine(t, true)
	var when kernel.Time
	root, err := NewCoordinator(eng, WithOutput(func(Message) {
		when = eng.Now()
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("gate", &gate{}); err != nil {
		t.Fatal(err)
	}
	if err := root.In("in", "gate", "in"); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("gate", "out", "done"); err != nil {
		t.Fatal(err)
	}
	if err := root.Inject("in", []byte("go")); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if when != 1 {
		t.Fatalf("fired at %d", when)
	}
	if root.Snapshot().Coupled.Reschedules != 1 {
		t.Fatalf("reschedules = %d", root.Snapshot().Coupled.Reschedules)
	}
}

func TestNestedGroupRoutesOut(t *testing.T) {
	eng := mustEngine(t, true)
	var got []byte
	root, err := NewCoordinator(eng, WithOutput(func(msg Message) {
		got = append([]byte(nil), msg.Payload...)
	}))
	if err != nil {
		t.Fatal(err)
	}
	cell, err := root.Group("cell")
	if err != nil {
		t.Fatal(err)
	}
	if err := cell.Add("inner", &pulse{name: "z"}); err != nil {
		t.Fatal(err)
	}
	if err := cell.Out("inner", "out", "up"); err != nil {
		t.Fatal(err)
	}
	if err := root.Out("cell", "up", "done"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "z" {
		t.Fatalf("output = %q", got)
	}
	if root.Snapshot().Coupled.Models != 1 {
		t.Fatalf("models = %d", root.Snapshot().Coupled.Models)
	}
}

func TestCheckpointRestoresRemainingStep(t *testing.T) {
	eng := mustEngine(t, false)
	first := &steps{block: true, started: make(chan struct{}), release: make(chan struct{})}
	root, err := NewCoordinator(eng)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("step", first); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-first.started
	done := make(chan error, 1)
	go func() { done <- eng.Pause() }()
	waitState(t, eng, "pausing")
	close(first.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, err := root.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	nextEng := mustEngine(t, false)
	second := &steps{done: make(chan struct{})}
	next, err := NewCoordinator(nextEng)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Add("step", second); err != nil {
		t.Fatal(err)
	}
	if err := next.Restore(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if nextEng.Snapshot().State != "paused" {
		t.Fatalf("state = %s", nextEng.Snapshot().State)
	}
	if err := nextEng.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-second.done:
	case <-time.After(2 * time.Second):
		t.Fatal("restored step did not run")
	}
	if second.n != 2 {
		t.Fatalf("n = %d", second.n)
	}
	if err := nextEng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointRequiresState(t *testing.T) {
	eng := mustEngine(t, false)
	root, err := NewCoordinator(eng)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Add("ping", &pulse{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Checkpoint(); err != ErrState {
		t.Fatalf("checkpoint = %v", err)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

type pulse struct {
	name string
	gap  time.Duration
	done bool
}

func (p *pulse) Advance() (time.Duration, bool) {
	if p.done {
		return 0, false
	}
	return p.gap, true
}

func (p *pulse) Output() []Message {
	return []Message{{Port: "out", Payload: []byte(p.name)}}
}

func (p *pulse) Internal() { p.done = true }

func (p *pulse) External(time.Duration, Message) {}

type relay struct {
	delay time.Duration
	hold  []byte
	at    kernel.Time
}

func (r *relay) Advance() (time.Duration, bool) {
	if r.hold == nil {
		return 0, false
	}
	return r.delay, true
}

func (r *relay) Output() []Message {
	return []Message{{Port: "out", Payload: clone(r.hold)}}
}

func (r *relay) Internal() { r.hold = nil }

func (r *relay) External(elapsed time.Duration, msg Message) {
	r.at += kernel.Time(elapsed / time.Millisecond)
	r.hold = clone(msg.Payload)
}

type gate struct {
	open  bool
	fired bool
}

func (g *gate) Advance() (time.Duration, bool) {
	if g.fired {
		return 0, false
	}
	if !g.open {
		return 10 * time.Millisecond, true
	}
	return time.Millisecond, true
}

func (g *gate) Output() []Message {
	return []Message{{Port: "out", Payload: []byte("go")}}
}

func (g *gate) Internal() { g.fired = true }

func (g *gate) External(time.Duration, Message) { g.open = true }

type steps struct {
	n       int
	block   bool
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func (s *steps) Advance() (time.Duration, bool) {
	if s.n >= 2 {
		return 0, false
	}
	if s.n == 0 {
		return 0, true
	}
	return time.Millisecond, true
}

func (s *steps) Output() []Message { return nil }

func (s *steps) Internal() {
	s.n++
	if s.n == 1 && s.block {
		close(s.started)
		<-s.release
	}
	if s.done != nil && s.n >= 2 {
		close(s.done)
	}
}

func (s *steps) External(time.Duration, Message) {}

func (s *steps) Save() ([]byte, error) { return []byte{byte(s.n)}, nil }

func (s *steps) Load(data []byte) error {
	if len(data) != 1 {
		return kernel.ErrCheckpoint
	}
	s.n = int(data[0])
	return nil
}

func rootTime(eng *kernel.Engine) kernel.Time { return eng.Now() }

func waitState(t *testing.T, eng *kernel.Engine, state string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Snapshot().State == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state = %s", eng.Snapshot().State)
}
