package engine

import (
	"errors"
	"math/rand/v2"
	"time"
)

// Step is the fixed bucket width. It is not configurable.
const Step = time.Millisecond

// SlowThreshold is the handler wall-clock limit that marks a slow event.
// It measures how long the handler ran, not the event's simulation duration.
const SlowThreshold = time.Millisecond

// Time is a simulation timestamp in milliseconds.
type Time int64

// ID identifies one scheduled event. The first accepted event is 1.
type ID uint64

// Handler runs when an event reaches its simulation time.
// A non-nil error stops the engine.
type Handler func(Context) error

// Event is one named handler submitted to the engine.
// Name must be non-empty and Handler must be non-nil.
type Event struct {
	Name    string
	Handler Handler
}

// Lag is how late a step started relative to its theoretical wall time.
// Step and At are that simulation millisecond. Lag is the wall-clock overrun.
type Lag struct {
	Step int64
	At   Time
	Lag  time.Duration
}

// LagHandler is called once for each step that skips its realtime wait.
// It runs on the event loop and must not perform I/O or call Stop or Wait.
type LagHandler func(Lag)

// SlowEvent is one handler whose wall-clock duration exceeded SlowThreshold.
// Elapsed is the measured duration. Overrun is Elapsed minus SlowThreshold.
type SlowEvent struct {
	ID       ID
	Name     string
	At       Time
	Step     int64
	Elapsed  time.Duration
	Overrun  time.Duration
	Finished time.Time
}

// Snapshot is a point-in-time copy of engine counters and recent slow events.
// LogDropped counts only the engine logger, not the metrics logger.
type Snapshot struct {
	State           string
	Now             Time
	Step            int64
	Lag             time.Duration
	EventsPending   int
	StepPending     int
	EventsScheduled uint64
	EventsProcessed uint64
	EventsCancelled uint64
	EventsRejected  uint64
	HandlerErrors   uint64
	CatchupSteps    uint64
	SlowTotal       uint64
	LogDropped      uint64
	Uptime          time.Duration
	EventRate       float64
	SlowEvents      []SlowEvent
}

// Context is the engine view passed to the handler that is running.
// Scheduling through it follows the same timing rules as Engine.
// Rand is the engine's deterministic source and is not safe for use
// outside the handler.
type Context interface {
	Now() Time
	EventAt() Time
	ID() ID
	Name() string
	AddEvent(at Time, ev Event) (ID, error)
	AddFuture(after time.Duration, ev Event) (ID, error)
	Cancel(id ID) bool
	Rand() *rand.Rand
}

var (
	// ErrName is returned when an event name is empty.
	ErrName = errors.New("empty event name")
	// ErrHandler is returned when an event handler is nil.
	ErrHandler = errors.New("nil handler")
	// ErrPast is returned when the timestamp is before the watermark,
	// or when AddFuture is given a negative delay.
	ErrPast = errors.New("event time is before the current step")
	// ErrClosed is returned when the engine is stopping or has stopped.
	ErrClosed = errors.New("engine is closed")
	// ErrStarted is returned when Start is called after the engine has left
	// its initial state.
	ErrStarted = errors.New("engine already started")
)

type runCtx struct {
	e    *Engine
	id   ID
	name string
	at   Time
}

func (c *runCtx) Now() Time        { return c.e.Now() }
func (c *runCtx) EventAt() Time    { return c.at }
func (c *runCtx) ID() ID           { return c.id }
func (c *runCtx) Name() string     { return c.name }
func (c *runCtx) Rand() *rand.Rand { return c.e.rng }

func (c *runCtx) AddEvent(at Time, ev Event) (ID, error) {
	return c.e.AddEvent(at, ev)
}

func (c *runCtx) AddFuture(after time.Duration, ev Event) (ID, error) {
	return c.e.AddFuture(after, ev)
}

func (c *runCtx) Cancel(id ID) bool { return c.e.Cancel(id) }
