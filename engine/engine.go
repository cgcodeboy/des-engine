package engine

import (
	"log/slog"
	"time"

	"github.com/cgcodeboy/des-engine/internal/application/des"
	"github.com/cgcodeboy/des-engine/internal/application/devs"
)

const (
	// Step is the fixed simulation step.
	Step = des.Step
	// SlowThreshold is the handler duration that counts as a slow event.
	SlowThreshold = des.SlowThreshold
)

type (
	// Time is a simulation timestamp in whole milliseconds.
	Time = des.Time
	// ID identifies one scheduled event.
	ID = des.ID
	// Handler runs one event on the event loop.
	Handler = des.Handler
	// Event is one unit of work at a simulation time.
	Event = des.Event
	// Lag describes a step that missed its wall-clock target.
	Lag = des.Lag
	// LagHandler receives one lag report.
	LagHandler = des.LagHandler
	// SlowEvent is one handler that ran longer than SlowThreshold.
	SlowEvent = des.SlowEvent
	// Snapshot is a point-in-time copy of counters and recent slow events.
	Snapshot = des.Snapshot
	// CoupledStats counts the optional coordinator.
	CoupledStats = des.CoupledStats
	// Context is the engine view passed to the running handler.
	Context = des.Context
	// Option configures an Engine.
	Option = des.Option
	// Engine runs events in millisecond buckets on one goroutine.
	Engine = des.Engine

	// Message is one value crossing a port.
	Message = devs.Message
	// Atomic is one model owned by the caller.
	Atomic = devs.Atomic
	// Persistent exports model state for a checkpoint.
	Persistent = devs.Persistent
	// SelectFunc chooses which component runs when several are due together.
	SelectFunc = devs.SelectFunc
	// CoordinatorOption configures a root coordinator or a nested network.
	CoordinatorOption = devs.CoordinatorOption
	// Coordinator connects atomic models by port on one engine.
	Coordinator = devs.Coordinator
)

var (
	// ErrName is returned when an event name is empty.
	ErrName = des.ErrName
	// ErrHandler is returned when an event handler is nil.
	ErrHandler = des.ErrHandler
	// ErrPast is returned when the timestamp is before the watermark,
	// or when AddFuture is given a negative delay.
	ErrPast = des.ErrPast
	// ErrClosed is returned when the engine is stopping or has stopped.
	ErrClosed = des.ErrClosed
	// ErrStarted is returned when Start or Restore is called after the engine
	// has left its initial state.
	ErrStarted = des.ErrStarted
	// ErrNotRunning is returned when Pause is called and the engine is not running.
	ErrNotRunning = des.ErrNotRunning
	// ErrNotPaused is returned when Resume or Checkpoint is used and the engine is not paused.
	ErrNotPaused = des.ErrNotPaused
	// ErrHandlerName is returned when a checkpoint event name has no registered handler.
	ErrHandlerName = des.ErrHandlerName
	// ErrCheckpoint is returned when checkpoint bytes cannot be read.
	ErrCheckpoint = des.ErrCheckpoint

	// ErrComponent is returned when a component name is empty or contains a slash.
	ErrComponent = devs.ErrComponent
	// ErrPort is returned when a port name is empty.
	ErrPort = devs.ErrPort
	// ErrModel is returned when an atomic model is nil.
	ErrModel = devs.ErrModel
	// ErrEngine is returned when the engine is nil.
	ErrEngine = devs.ErrEngine
	// ErrExists is returned when a sibling already uses the name.
	ErrExists = devs.ErrExists
	// ErrChild is returned when a coupling names an unknown component.
	ErrChild = devs.ErrChild
	// ErrState is returned when a model cannot export its state.
	ErrState = devs.ErrState
	// ErrRoot is returned when a nested coordinator is used as the root.
	ErrRoot = devs.ErrRoot
)

// NewEngine builds an engine. Realtime mode is on and the random seed is 1
// unless options change them.
func NewEngine(opts ...Option) (*Engine, error) {
	return des.New(opts...)
}

// WithRealtime selects wall-clock alignment. The default is on.
// When off, a due bucket runs immediately instead of waiting.
func WithRealtime(on bool) Option {
	return des.WithRealtime(on)
}

// WithOrigin sets the wall-clock instant of simulation time zero.
// If it is omitted, Start captures the clock when the loop begins.
func WithOrigin(origin time.Time) Option {
	return des.WithOrigin(origin)
}

// WithLagHandler sets the callback for steps that miss their wall-clock target.
// Each such step is reported once.
func WithLagHandler(h LagHandler) Option {
	return des.WithLagHandler(h)
}

// WithLogger sets the sink for asynchronous engine logs.
// A nil logger, or omitting this option, uses slog.Default.
// Records are queued; the event loop does not write them itself.
func WithLogger(logger *slog.Logger) Option {
	return des.WithLogger(logger)
}

// WithSeed sets the seed for Context.Rand. The default seed is 1.
func WithSeed(seed int64) Option {
	return des.WithSeed(seed)
}

// WithExitWhenIdle stops the engine once no events remain.
// The default is to wait until Stop or context cancellation.
func WithExitWhenIdle(exit bool) Option {
	return des.WithExitWhenIdle(exit)
}

// WithHorizon stops the engine when the next step is later than until.
// A step equal to until still runs.
func WithHorizon(until Time) Option {
	return des.WithHorizon(until)
}

// NewCoordinator attaches a root coordinator to eng and registers its event name.
func NewCoordinator(eng *Engine, opts ...CoordinatorOption) (*Coordinator, error) {
	return devs.NewCoordinator(eng, opts...)
}

// WithEventName sets the engine event name used by the root coordinator.
// The default name is coordinator. Nested networks ignore this option.
func WithEventName(name string) CoordinatorOption {
	return devs.WithEventName(name)
}

// WithSelect sets the tie break for one network.
// Without it, the earliest registered component runs first.
func WithSelect(fn SelectFunc) CoordinatorOption {
	return devs.WithSelect(fn)
}

// WithOutput receives messages that leave the root coordinator.
// It runs on the event loop and follows the same restrictions as a model.
// Nested networks ignore this option.
func WithOutput(fn func(Message)) CoordinatorOption {
	return devs.WithOutput(fn)
}
