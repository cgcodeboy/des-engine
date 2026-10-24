package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/cgcodeboy/des-engine/internal/pkgs/asynclog"
	"github.com/cgcodeboy/des-engine/internal/pkgs/bucket"
)

type runState int

const (
	stateCreated runState = iota
	stateRunning
	statePausing
	statePaused
	stateStopping
	stateStopped
)

func (s runState) String() string {
	switch s {
	case stateCreated:
		return "created"
	case stateRunning:
		return "running"
	case statePausing:
		return "pausing"
	case statePaused:
		return "paused"
	case stateStopping:
		return "stopping"
	case stateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type scheduled struct {
	id       ID
	name     string
	at       Time
	seq      uint64
	h        Handler
	canceled bool
	payload  []byte
}

// Engine runs events in millisecond buckets on one goroutine.
type Engine struct {
	mu         sync.Mutex
	cond       *sync.Cond
	state      runState
	stopReq    bool
	stopWhy    string
	loopErr    error
	done       chan struct{}
	finishOnce sync.Once

	watermark int64
	executing int64
	pending   []*scheduled
	head      int
	buckets   bucket.Buckets[*scheduled]
	byID      map[ID]*scheduled
	nextID    ID
	seq       uint64

	realtime     bool
	origin       time.Time
	originSet    bool
	onLag        LagHandler
	exitWhenIdle bool
	horizon      Time
	horizonSet   bool
	clock        clock
	startedAt    time.Time
	pausedAt     time.Time
	pauseReq     bool
	pcg          *rand.PCG
	rng          *rand.Rand
	handlers     map[string]Handler

	slow      []SlowEvent
	slowTotal uint64
	lastLag   time.Duration

	scheduledN uint64
	processed  uint64
	cancelled  uint64
	rejected   uint64
	handlerErr uint64
	catchup    uint64

	logs *asynclog.Logger
	log  *slog.Logger
	ctx  context.Context
}

// New builds an engine. Realtime mode is on and the random seed is 1
// unless options change them.
func New(opts ...Option) (*Engine, error) {
	cfg := config{realtime: true, seed: 1}
	for _, opt := range opts {
		opt(&cfg)
	}
	var sink slog.Handler
	if cfg.loggerSet && cfg.logger != nil {
		sink = cfg.logger.Handler()
	}
	logs := asynclog.New(sink)
	clk := cfg.clock
	if clk == nil {
		clk = wallClock{}
	}
	pcg := rand.NewPCG(uint64(cfg.seed), uint64(cfg.seed))
	e := &Engine{
		state:        stateCreated,
		executing:    -1,
		byID:         make(map[ID]*scheduled),
		handlers:     make(map[string]Handler),
		realtime:     cfg.realtime,
		origin:       cfg.origin,
		originSet:    cfg.originSet,
		onLag:        cfg.onLag,
		exitWhenIdle: cfg.exitWhenIdle,
		horizon:      cfg.horizon,
		horizonSet:   cfg.horizonSet,
		clock:        clk,
		pcg:          pcg,
		rng:          rand.New(pcg),
		logs:         logs,
		log:          slog.New(logs.Handler()),
	}
	e.cond = sync.NewCond(&e.mu)
	return e, nil
}

// Start launches the event loop. A nil context is rejected.
// Calling Start again returns ErrStarted.
// Canceling ctx requests the same shutdown as Stop.
func (e *Engine) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != stateCreated {
		return ErrStarted
	}
	if e.realtime && !e.originSet {
		e.origin = e.clock.Now()
	}
	e.startedAt = e.clock.Now()
	e.ctx = ctx
	e.done = make(chan struct{})
	e.state = stateRunning
	e.log.Info("engine started",
		slog.Time("origin", e.origin),
		slog.Bool("realtime", e.realtime),
		slog.Int("step_ms", 1),
	)
	go e.watch(ctx)
	go e.loop()
	return nil
}

// Stop requests shutdown and waits until the loop exits and the log
// queue finishes flushing. Stop before Start, and a second Stop, return nil.
// Stop also ends a paused engine. A handler error or panic is returned to the caller.
func (e *Engine) Stop() error {
	e.mu.Lock()
	if e.state != stateRunning && e.state != statePausing && e.state != statePaused {
		e.mu.Unlock()
		return nil
	}
	e.stopReq = true
	if e.stopWhy == "" {
		e.stopWhy = "stop"
	}
	done := e.done
	e.cond.Broadcast()
	e.mu.Unlock()
	<-done
	return e.loopErr
}

// Pause parks the loop after the current millisecond drains.
// Pause while already paused returns nil. Handlers must not call Pause.
func (e *Engine) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		switch e.state {
		case statePaused:
			return nil
		case stateRunning, statePausing:
			if e.state == stateRunning {
				e.state = statePausing
				e.pauseReq = true
				e.cond.Broadcast()
			}
			e.cond.Wait()
		case stateStopping, stateStopped:
			return ErrClosed
		default:
			return ErrNotRunning
		}
	}
}

// Resume continues a paused engine.
// In realtime mode the origin moves forward by the wall time spent paused.
func (e *Engine) Resume() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != statePaused {
		return ErrNotPaused
	}
	if e.realtime && !e.pausedAt.IsZero() {
		e.origin = e.origin.Add(e.clock.Now().Sub(e.pausedAt))
	}
	e.pausedAt = time.Time{}
	e.pauseReq = false
	e.state = stateRunning
	e.cond.Broadcast()
	e.log.Info("engine resumed")
	return nil
}

// Handle registers h under name. Restore uses this table to attach handlers.
// A later registration of the same name replaces the previous function.
func (e *Engine) Handle(name string, h Handler) error {
	if name == "" {
		return ErrName
	}
	if h == nil {
		return ErrHandler
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handlers == nil {
		e.handlers = make(map[string]Handler)
	}
	e.handlers[name] = h
	return nil
}

// Wait blocks until the engine has stopped and returns the same error as Stop.
// Wait before Start returns nil.
func (e *Engine) Wait() error {
	e.mu.Lock()
	if e.state == stateCreated {
		e.mu.Unlock()
		return nil
	}
	done := e.done
	e.mu.Unlock()
	<-done
	return e.loopErr
}

// Now returns the executing millisecond, or the watermark when no step
// is executing. Before Start, Now is 0.
func (e *Engine) Now() Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nowLocked()
}

// AddEvent inserts ev at absolute simulation time at.
// A timestamp equal to the executing millisecond is appended to that step.
// An earlier timestamp returns ErrPast.
func (e *Engine) AddEvent(at Time, ev Event) (ID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.addLocked(at, ev)
}

// AddFuture accepts ev immediately and schedules it at Now plus after,
// rounded up to a whole millisecond. A zero delay appends to the executing
// step. A negative delay returns ErrPast.
func (e *Engine) AddFuture(after time.Duration, ev Event) (ID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if after < 0 {
		e.rejected++
		return 0, ErrPast
	}
	return e.addLocked(e.nowLocked()+millis(after), ev)
}

// Cancel marks a queued event so the loop skips it.
// It returns false when id is unknown or the event has already started.
func (e *Engine) Cancel(id ID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	ev, ok := e.byID[id]
	if !ok || ev.canceled {
		return false
	}
	ev.canceled = true
	e.cond.Broadcast()
	return true
}

// Snapshot copies the counters and the recent slow-event ring.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	var uptime time.Duration
	if !e.startedAt.IsZero() {
		uptime = e.clock.Now().Sub(e.startedAt)
	}
	var rate float64
	if uptime > 0 {
		rate = float64(e.processed) / uptime.Seconds()
	}
	step := e.watermark
	if e.executing >= 0 {
		step = e.executing
	}
	return Snapshot{
		State:           e.state.String(),
		Now:             e.nowLocked(),
		Step:            step,
		Lag:             e.lastLag,
		EventsPending:   e.buckets.Len() + e.pendingCount(),
		StepPending:     e.pendingCount(),
		EventsScheduled: e.scheduledN,
		EventsProcessed: e.processed,
		EventsCancelled: e.cancelled,
		EventsRejected:  e.rejected,
		HandlerErrors:   e.handlerErr,
		CatchupSteps:    e.catchup,
		SlowTotal:       e.slowTotal,
		LogDropped:      e.logs.Dropped(),
		Uptime:          uptime,
		EventRate:       rate,
		SlowEvents:      append([]SlowEvent(nil), e.slow...),
	}
}

func (e *Engine) nowLocked() Time {
	if e.executing >= 0 {
		return Time(e.executing)
	}
	return Time(e.watermark)
}

func (e *Engine) pendingCount() int {
	if e.head >= len(e.pending) {
		return 0
	}
	return len(e.pending) - e.head
}

func millis(after time.Duration) Time {
	if after <= 0 {
		return 0
	}
	return Time((after + time.Millisecond - 1) / time.Millisecond)
}

func (e *Engine) addLocked(at Time, ev Event) (ID, error) {
	if e.state == stateStopping || e.state == stateStopped || e.stopReq {
		return 0, ErrClosed
	}
	if ev.Name == "" {
		return 0, ErrName
	}
	h := ev.Handler
	if h == nil {
		h = e.handlers[ev.Name]
	}
	if h == nil {
		return 0, ErrHandler
	}
	if at < 0 || at < e.nowBound() {
		e.rejected++
		return 0, ErrPast
	}
	e.seq++
	e.nextID++
	s := &scheduled{
		id:      e.nextID,
		name:    ev.Name,
		at:      at,
		seq:     e.seq,
		h:       h,
		payload: append([]byte(nil), ev.Payload...),
	}
	e.byID[s.id] = s
	if e.executing >= 0 && at == Time(e.executing) {
		e.pending = append(e.pending, s)
	} else {
		e.buckets.Push(int64(at), s)
	}
	e.scheduledN++
	e.cond.Broadcast()
	e.log.Debug("event queued",
		slog.Uint64("event_id", uint64(s.id)),
		slog.String("event_name", s.name),
		slog.Int64("sim_time_ms", int64(at)),
	)
	return s.id, nil
}

func (e *Engine) nowBound() Time {
	return Time(e.watermark)
}

func (e *Engine) watch(ctx context.Context) {
	select {
	case <-ctx.Done():
		e.mu.Lock()
		if e.state == stateRunning || e.state == statePausing || e.state == statePaused {
			e.stopReq = true
			if e.stopWhy == "" {
				e.stopWhy = "context canceled"
			}
			e.cond.Broadcast()
		}
		e.mu.Unlock()
	case <-e.done:
	}
}

func (e *Engine) loop() {
	defer e.finish()
	var lagged int64 = -1
	e.mu.Lock()
	for {
		if e.stopReq || (e.ctx != nil && e.ctx.Err() != nil) {
			e.beginStop("")
			e.mu.Unlock()
			return
		}
		if e.pendingCount() == 0 {
			if e.executing >= 0 {
				e.watermark = e.executing + 1
				e.executing = -1
			}
			if e.pauseReq || e.state == statePaused {
				e.enterPausedLocked()
				e.cond.Wait()
				continue
			}
			next, ok := e.buckets.Next(e.watermark)
			if !ok {
				if e.exitWhenIdle {
					e.beginStop("idle")
					e.mu.Unlock()
					return
				}
				e.log.Debug("idle")
				e.cond.Wait()
				continue
			}
			if e.horizonSet && next > int64(e.horizon) {
				e.beginStop("horizon")
				e.mu.Unlock()
				return
			}
			if e.realtime {
				target := e.origin.Add(time.Duration(next) * time.Millisecond)
				now := e.clock.Now()
				if now.Before(target) {
					timer := e.clock.AfterFunc(target.Sub(now), func() {
						e.mu.Lock()
						e.cond.Broadcast()
						e.mu.Unlock()
					})
					e.cond.Wait()
					timer.Stop()
					continue
				}
				if now.After(target) && lagged != next {
					lagged = next
					lag := now.Sub(target)
					e.lastLag = lag
					e.catchup++
					cb := e.onLag
					info := Lag{Step: next, At: Time(next), Lag: lag}
					e.mu.Unlock()
					e.log.Warn("catch-up",
						slog.Int64("step", next),
						slog.Int64("sim_time_ms", next),
						slog.Duration("lag", lag),
					)
					if cb != nil {
						cb(info)
					}
					e.mu.Lock()
					continue
				}
			}
			e.executing = next
			e.watermark = next
			taken := e.buckets.Take(next)
			e.pending = taken
			e.head = 0
			lagged = -1
			e.log.Debug("step opened", slog.Int64("sim_time_ms", next))
			continue
		}

		ev := e.pending[e.head]
		e.head++
		if e.head == len(e.pending) {
			e.pending = nil
			e.head = 0
		}
		if ev.canceled {
			e.cancelled++
			delete(e.byID, ev.id)
			e.log.Debug("event canceled",
				slog.Uint64("event_id", uint64(ev.id)),
				slog.String("event_name", ev.name),
				slog.Int64("sim_time_ms", int64(ev.at)),
			)
			continue
		}
		delete(e.byID, ev.id)
		h, id, name, at, payload := ev.h, ev.id, ev.name, ev.at, ev.payload
		e.mu.Unlock()

		started := e.clock.Now()
		err, panicVal := callHandler(h, &runCtx{e: e, id: id, name: name, at: at, payload: payload})
		elapsed := e.clock.Now().Sub(started)
		if elapsed > SlowThreshold {
			finished := e.clock.Now()
			e.mu.Lock()
			e.appendSlow(SlowEvent{
				ID:       id,
				Name:     name,
				At:       at,
				Step:     int64(at),
				Elapsed:  elapsed,
				Overrun:  elapsed - SlowThreshold,
				Finished: finished,
			})
			e.mu.Unlock()
			e.log.Warn("slow event",
				slog.Uint64("event_id", uint64(id)),
				slog.String("event_name", name),
				slog.Int64("sim_time_ms", int64(at)),
				slog.Int64("step", int64(at)),
				slog.Duration("elapsed", elapsed),
				slog.Duration("threshold", SlowThreshold),
			)
		}
		if panicVal != nil {
			e.log.Error("handler panicked",
				slog.Uint64("event_id", uint64(id)),
				slog.String("event_name", name),
				slog.Int64("sim_time_ms", int64(at)),
				slog.Any("panic", panicVal),
			)
		} else if err != nil {
			e.log.Error("handler failed",
				slog.Uint64("event_id", uint64(id)),
				slog.String("event_name", name),
				slog.Int64("sim_time_ms", int64(at)),
				slog.String("error", err.Error()),
			)
		}

		e.mu.Lock()
		e.processed++
		if err != nil {
			e.handlerErr++
			e.loopErr = err
			e.beginStop("handler failed")
			e.mu.Unlock()
			return
		}
	}
}

func (e *Engine) beginStop(reason string) {
	e.state = stateStopping
	if e.stopWhy == "" {
		if reason != "" {
			e.stopWhy = reason
		} else {
			e.stopWhy = "stop"
		}
	}
	e.cond.Broadcast()
}

func (e *Engine) enterPausedLocked() {
	if e.state != statePaused {
		e.pausedAt = e.clock.Now()
		e.state = statePaused
		e.pauseReq = false
		e.log.Info("engine paused", slog.Int64("sim_time_ms", e.watermark))
	}
	e.cond.Broadcast()
}

func (e *Engine) finish() {
	e.finishOnce.Do(func() {
		reason := e.stopWhy
		if reason == "" {
			reason = "stop"
		}
		e.log.Info("engine stopped", slog.String("reason", reason))
		e.logs.Close()
		e.mu.Lock()
		e.state = stateStopped
		e.mu.Unlock()
		close(e.done)
	})
}

func callHandler(h Handler, c Context) (err error, panicVal any) {
	defer func() {
		if r := recover(); r != nil {
			panicVal = r
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(c), nil
}

type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) stopTimer
}

type stopTimer interface {
	Stop() bool
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func (wallClock) AfterFunc(d time.Duration, f func()) stopTimer {
	return time.AfterFunc(d, f)
}
