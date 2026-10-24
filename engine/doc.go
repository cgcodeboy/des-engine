// Package engine is a fixed-step discrete-event simulation kernel.
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
package engine
