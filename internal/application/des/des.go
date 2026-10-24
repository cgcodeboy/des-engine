// Package des is the direct-event application.
// It forwards to the lower event loop and does not import the devs application.
package des

import (
	"log/slog"
	"time"

	core "github.com/cgcodeboy/des-engine/internal/kernel"
)

const (
	// Step is the fixed simulation step.
	Step = core.Step
	// SlowThreshold is the handler duration that counts as a slow event.
	SlowThreshold = core.SlowThreshold
)

type (
	// Time is a simulation timestamp in whole milliseconds.
	Time = core.Time
	// ID identifies one scheduled event.
	ID = core.ID
	// Handler runs one event on the event loop.
	Handler = core.Handler
	// Event is one unit of work at a simulation time.
	Event = core.Event
	// Lag describes a step that missed its wall-clock target.
	Lag = core.Lag
	// LagHandler receives one lag report.
	LagHandler = core.LagHandler
	// SlowEvent is one handler that ran longer than SlowThreshold.
	SlowEvent = core.SlowEvent
	// Snapshot is a point-in-time copy of counters and recent slow events.
	Snapshot = core.Snapshot
	// CoupledStats counts the optional coordinator.
	CoupledStats = core.CoupledStats
	// Context is the engine view passed to the running handler.
	Context = core.Context
	// Option configures an Engine.
	Option = core.Option
	// Engine runs events in millisecond buckets on one goroutine.
	Engine = core.Engine
)

var (
	// ErrName is returned when an event name is empty.
	ErrName = core.ErrName
	// ErrHandler is returned when an event handler is nil.
	ErrHandler = core.ErrHandler
	// ErrPast is returned when the timestamp is before the watermark,
	// or when AddFuture is given a negative delay.
	ErrPast = core.ErrPast
	// ErrClosed is returned when the engine is stopping or has stopped.
	ErrClosed = core.ErrClosed
	// ErrStarted is returned when Start or Restore is called after the engine
	// has left its initial state.
	ErrStarted = core.ErrStarted
	// ErrNotRunning is returned when Pause is called and the engine is not running.
	ErrNotRunning = core.ErrNotRunning
	// ErrNotPaused is returned when Resume or Checkpoint is used and the engine is not paused.
	ErrNotPaused = core.ErrNotPaused
	// ErrHandlerName is returned when a checkpoint event name has no registered handler.
	ErrHandlerName = core.ErrHandlerName
	// ErrCheckpoint is returned when checkpoint bytes cannot be read.
	ErrCheckpoint = core.ErrCheckpoint
)

// New builds an engine.
func New(opts ...Option) (*Engine, error) { return core.New(opts...) }

// WithRealtime selects wall-clock alignment.
func WithRealtime(on bool) Option { return core.WithRealtime(on) }

// WithOrigin sets the wall-clock instant of simulation time zero.
func WithOrigin(origin time.Time) Option { return core.WithOrigin(origin) }

// WithLagHandler sets the callback for steps that miss their wall-clock target.
func WithLagHandler(h LagHandler) Option { return core.WithLagHandler(h) }

// WithLogger sets the sink for asynchronous engine logs.
func WithLogger(logger *slog.Logger) Option { return core.WithLogger(logger) }

// WithSeed sets the seed for Context.Rand.
func WithSeed(seed int64) Option { return core.WithSeed(seed) }

// WithExitWhenIdle stops the engine once no events remain.
func WithExitWhenIdle(exit bool) Option { return core.WithExitWhenIdle(exit) }

// WithHorizon stops the engine when the next step is later than until.
func WithHorizon(until Time) Option { return core.WithHorizon(until) }
