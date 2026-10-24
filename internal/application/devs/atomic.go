package devs

import (
	"time"
)

// Message is one value crossing a port.
type Message struct {
	Port    string
	Payload []byte
}

// Atomic is one model owned by the caller.
// Advance reports the delay until the next internal event.
// ok is false when the model is waiting for input.
// A negative delay is treated as immediate.
// Output runs when that event is due, and Internal runs after Output.
// External receives one input. elapsed is the simulation time since the
// previous transition.
type Atomic interface {
	Advance() (after time.Duration, ok bool)
	Output() []Message
	Internal()
	External(elapsed time.Duration, msg Message)
}

// Persistent exports model state for a checkpoint.
// Same-process pause and resume do not call it.
type Persistent interface {
	Save() ([]byte, error)
	Load(data []byte) error
}
