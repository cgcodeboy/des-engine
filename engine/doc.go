// Package engine is the public shell of a fixed-step discrete-event simulation kernel.
// Two applications sit on the event loop in internal/kernel.
// internal/application/des is the direct-event application.
// internal/application/devs is the coordinator application.
// The applications do not import each other.
// Callers outside this module use only the names declared here.
//
// Time is a whole number of milliseconds, and each millisecond is one bucket.
// Empty buckets are skipped. In the default realtime mode, bucket k waits
// until origin plus k milliseconds. If the wall clock is already past that
// instant, the bucket runs immediately and the lag is reported.
//
// AddEvent schedules an absolute timestamp. AddFuture schedules a delay from
// Now, rounded up to a whole millisecond. A zero delay, or a timestamp equal
// to the millisecond currently executing, is appended to that step. A
// timestamp before the watermark is rejected.
//
// The event loop is one goroutine. Handlers and lag callbacks run there
// without the engine lock. They must not perform I/O, and they must not call
// Stop, Wait, Pause, Resume, or Checkpoint.
//
// Pause parks the loop after the current millisecond drains. Resume continues
// it. Checkpoint copies a paused engine into bytes owned by the caller.
// Restore loads those bytes into an engine whose handlers were registered
// with Handle, and leaves that engine paused.
//
// Coordinator is optional. It connects atomic models by port and inserts
// one imminent event into the same engine. Callers that schedule events
// themselves do not use it.
//
// Snapshot.StatusJSON and Snapshot.Prometheus render a snapshot for a
// caller-owned server. This package does not listen.
package engine
