// Package engine is a fixed-step discrete event simulation kernel.
//
// Simulation time is an integer number of milliseconds. One bucket is one
// millisecond. Realtime mode aligns bucket k to origin + k milliseconds.
// AddFuture accepts an event and a delay, then runs it at the future
// millisecond produced by rounding the delay up.
//
// Handlers and lag callbacks run on the event loop. They must not perform
// I/O, and they must not call Stop or Wait.
package engine
