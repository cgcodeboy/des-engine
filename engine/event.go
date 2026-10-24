package engine

import (
	"errors"
	"math/rand/v2"
	"time"
)

// Step is the fixed simulation bucket width.
const Step = time.Millisecond

// SlowThreshold is the handler wall-clock duration that marks a slow event.
const SlowThreshold = time.Millisecond

// Time is a simulation timestamp in milliseconds.
type Time int64

// ID identifies one scheduled event.
type ID uint64

// Handler runs when an event reaches its simulation time.
type Handler func(Context) error

// Event is a named handler submitted to the engine.
type Event struct {
	Name    string
	Handler Handler
}

// Lag is the amount by which a step started after its theoretical wall time.
type Lag struct {
	Step int64
	At   Time
	Lag  time.Duration
}

// LagHandler receives lag for a step that skipped its wait.
type LagHandler func(Lag)

// SlowEvent is one handler whose wall-clock duration exceeded SlowThreshold.
type SlowEvent struct {
	ID       ID
	Name     string
	At       Time
	Step     int64
	Elapsed  time.Duration
	Overrun  time.Duration
	Finished time.Time
}

// Snapshot is a consistent copy of engine counters.
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

// Context is the view of the engine available to a running handler.
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
	// ErrPast is returned when an event would move simulation time backward.
	ErrPast = errors.New("event time is before the current step")
	// ErrClosed is returned when the engine is no longer accepting events.
	ErrClosed = errors.New("engine is closed")
	// ErrStarted is returned when Start is called more than once.
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
