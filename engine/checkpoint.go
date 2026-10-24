package engine

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/cgcodeboy/des-engine/internal/bucket"
)

const checkpointVersion = 1

type checkpointFile struct {
	Version      int
	Watermark    int64
	NextID       uint64
	Seq          uint64
	Origin       time.Time
	OriginSet    bool
	PausedAt     time.Time
	Realtime     bool
	ExitWhenIdle bool
	Horizon      int64
	HorizonSet   bool
	Uptime       time.Duration
	PCG          []byte
	Scheduled    uint64
	Processed    uint64
	Cancelled    uint64
	Rejected     uint64
	HandlerErr   uint64
	Catchup      uint64
	SlowTotal    uint64
	LastLag      time.Duration
	Slow         []SlowEvent
	Events       []checkpointEvent
}

type checkpointEvent struct {
	ID      uint64
	Name    string
	At      int64
	Seq     uint64
	Payload []byte
}

// Checkpoint copies the paused engine into bytes owned by the caller.
// The engine does not keep this copy. Names that were not registered with
// Handle make the call fail and produce no bytes.
func (e *Engine) Checkpoint() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != statePaused {
		return nil, ErrNotPaused
	}
	events, droppedCancels := e.queuedEvents()
	for _, ev := range events {
		if _, ok := e.handlers[ev.name]; !ok {
			return nil, ErrHandlerName
		}
	}
	pcg, err := e.pcg.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var up time.Duration
	if !e.startedAt.IsZero() {
		up = e.clock.Now().Sub(e.startedAt)
	}
	file := checkpointFile{
		Version:      checkpointVersion,
		Watermark:    e.watermark,
		NextID:       uint64(e.nextID),
		Seq:          e.seq,
		Origin:       e.origin,
		OriginSet:    e.originSet,
		PausedAt:     e.pausedAt,
		Realtime:     e.realtime,
		ExitWhenIdle: e.exitWhenIdle,
		Horizon:      int64(e.horizon),
		HorizonSet:   e.horizonSet,
		Uptime:       up,
		PCG:          pcg,
		Scheduled:    e.scheduledN,
		Processed:    e.processed,
		Cancelled:    e.cancelled + droppedCancels,
		Rejected:     e.rejected,
		HandlerErr:   e.handlerErr,
		Catchup:      e.catchup,
		SlowTotal:    e.slowTotal,
		LastLag:      e.lastLag,
		Slow:         append([]SlowEvent(nil), e.slow...),
		Events:       make([]checkpointEvent, len(events)),
	}
	for i, ev := range events {
		file.Events[i] = checkpointEvent{
			ID:      uint64(ev.id),
			Name:    ev.name,
			At:      int64(ev.at),
			Seq:     ev.seq,
			Payload: append([]byte(nil), ev.payload...),
		}
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Restore loads a checkpoint into an engine that has not been started.
// Handlers must already be registered with Handle. The engine is left paused.
func (e *Engine) Restore(ctx context.Context, data []byte) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	var file checkpointFile
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&file); err != nil || file.Version != checkpointVersion {
		return ErrCheckpoint
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != stateCreated {
		return ErrStarted
	}
	for _, ev := range file.Events {
		if _, ok := e.handlers[ev.Name]; !ok {
			return ErrHandlerName
		}
	}
	if err := e.pcg.UnmarshalBinary(file.PCG); err != nil {
		return ErrCheckpoint
	}
	e.rng = rand.New(e.pcg)
	e.watermark = file.Watermark
	e.executing = -1
	e.pending = nil
	e.head = 0
	e.nextID = ID(file.NextID)
	e.seq = file.Seq
	e.origin = file.Origin
	e.originSet = file.OriginSet
	e.pausedAt = file.PausedAt
	e.realtime = file.Realtime
	e.exitWhenIdle = file.ExitWhenIdle
	e.horizon = Time(file.Horizon)
	e.horizonSet = file.HorizonSet
	e.scheduledN = file.Scheduled
	e.processed = file.Processed
	e.cancelled = file.Cancelled
	e.rejected = file.Rejected
	e.handlerErr = file.HandlerErr
	e.catchup = file.Catchup
	e.slowTotal = file.SlowTotal
	e.lastLag = file.LastLag
	e.slow = append([]SlowEvent(nil), file.Slow...)
	e.byID = make(map[ID]*scheduled, len(file.Events))
	e.buckets = bucket.Buckets[*scheduled]{}
	for _, ev := range file.Events {
		s := &scheduled{
			id:      ID(ev.ID),
			name:    ev.Name,
			at:      Time(ev.At),
			seq:     ev.Seq,
			h:       e.handlers[ev.Name],
			payload: append([]byte(nil), ev.Payload...),
		}
		e.byID[s.id] = s
		e.buckets.Push(ev.At, s)
	}
	if e.pausedAt.IsZero() {
		e.pausedAt = e.clock.Now()
	}
	if file.Uptime > 0 {
		e.startedAt = e.clock.Now().Add(-file.Uptime)
	} else {
		e.startedAt = e.clock.Now()
	}
	e.ctx = ctx
	e.done = make(chan struct{})
	e.state = statePaused
	e.pauseReq = false
	e.log.Info("engine restored", slog.Int64("sim_time_ms", e.watermark))
	go e.watch(ctx)
	go e.loop()
	return nil
}

func (e *Engine) queuedEvents() ([]*scheduled, uint64) {
	out := make([]*scheduled, 0, len(e.byID))
	var dropped uint64
	for _, ev := range e.byID {
		if ev.canceled {
			dropped++
			continue
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].at != out[j].at {
			return out[i].at < out[j].at
		}
		return out[i].seq < out[j].seq
	})
	return out, dropped
}
