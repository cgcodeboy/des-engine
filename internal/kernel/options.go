package kernel

import (
	"log/slog"
	"time"
)

// Option configures an Engine.
type Option func(*config)

type config struct {
	realtime     bool
	origin       time.Time
	originSet    bool
	onLag        LagHandler
	logger       *slog.Logger
	loggerSet    bool
	seed         int64
	exitWhenIdle bool
	horizon      Time
	horizonSet   bool
	clock        clock
}

// WithRealtime selects wall-clock alignment. The default is on.
// When off, a due bucket runs immediately instead of waiting.
func WithRealtime(on bool) Option {
	return func(c *config) { c.realtime = on }
}

// WithOrigin sets the wall-clock instant of simulation time zero.
// If it is omitted, Start captures the clock when the loop begins.
func WithOrigin(origin time.Time) Option {
	return func(c *config) {
		c.origin = origin
		c.originSet = true
	}
}

// WithLagHandler sets the callback for steps that miss their wall-clock target.
// Each such step is reported once.
func WithLagHandler(h LagHandler) Option {
	return func(c *config) { c.onLag = h }
}

// WithLogger sets the sink for asynchronous engine logs.
// A nil logger, or omitting this option, uses slog.Default.
// Records are queued; the event loop does not write them itself.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		c.logger = logger
		c.loggerSet = true
	}
}

// WithSeed sets the seed for Context.Rand. The default seed is 1.
func WithSeed(seed int64) Option {
	return func(c *config) { c.seed = seed }
}

// WithExitWhenIdle stops the engine once no events remain.
// The default is to wait until Stop or context cancellation.
func WithExitWhenIdle(exit bool) Option {
	return func(c *config) { c.exitWhenIdle = exit }
}

// WithHorizon stops the engine when the next step is later than until.
// A step equal to until still runs.
func WithHorizon(until Time) Option {
	return func(c *config) {
		c.horizon = until
		c.horizonSet = true
	}
}

func withClock(c clock) Option {
	return func(cfg *config) { cfg.clock = c }
}
