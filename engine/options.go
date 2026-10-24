package engine

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

// WithRealtime selects wall-clock alignment. The default is realtime.
func WithRealtime(on bool) Option {
	return func(c *config) { c.realtime = on }
}

// WithOrigin sets the wall-clock time of simulation time zero.
func WithOrigin(origin time.Time) Option {
	return func(c *config) {
		c.origin = origin
		c.originSet = true
	}
}

// WithLagHandler receives one callback for each step that starts late.
func WithLagHandler(h LagHandler) Option {
	return func(c *config) { c.onLag = h }
}

// WithLogger sets the sink that receives asynchronous engine logs.
// A nil logger uses slog.Default.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		c.logger = logger
		c.loggerSet = true
	}
}

// WithSeed sets the deterministic random seed. The default seed is 1.
func WithSeed(seed int64) Option {
	return func(c *config) { c.seed = seed }
}

// WithExitWhenIdle stops the engine when no events remain.
func WithExitWhenIdle(exit bool) Option {
	return func(c *config) { c.exitWhenIdle = exit }
}

// WithHorizon stops the engine before executing a step later than until.
func WithHorizon(until Time) Option {
	return func(c *config) {
		c.horizon = until
		c.horizonSet = true
	}
}

func withClock(c clock) Option {
	return func(cfg *config) { cfg.clock = c }
}
