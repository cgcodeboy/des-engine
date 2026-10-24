package devs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"github.com/cgcodeboy/des-engine/internal/kernel"
	"strings"
	"sync"
	"time"
)

var (
	// ErrComponent is returned when a component name is empty or contains a slash.
	ErrComponent = errors.New("invalid component name")
	// ErrPort is returned when a port name is empty.
	ErrPort = errors.New("empty port")
	// ErrModel is returned when an atomic model is nil.
	ErrModel = errors.New("nil model")
	// ErrEngine is returned when the engine is nil.
	ErrEngine = errors.New("nil engine")
	// ErrExists is returned when a sibling already uses the name.
	ErrExists = errors.New("name already exists")
	// ErrChild is returned when a coupling names an unknown component.
	ErrChild = errors.New("unknown component")
	// ErrState is returned when a model cannot export its state.
	ErrState = errors.New("model state is unavailable")
	// ErrRoot is returned when a nested coordinator is used as the root.
	ErrRoot = errors.New("coordinator is not the root")
)

// SelectFunc chooses which component runs when several are due together.
// due contains component names in registration order.
type SelectFunc func(due []string) string

// CoordinatorOption configures a root coordinator or a nested network.
type CoordinatorOption func(*coordinatorConfig)

type coordinatorConfig struct {
	name     string
	selectFn SelectFunc
	output   func(Message)
}

// WithEventName sets the engine event name used by the root coordinator.
// The default name is coordinator. Nested networks ignore this option.
func WithEventName(name string) CoordinatorOption {
	return func(cfg *coordinatorConfig) { cfg.name = name }
}

// WithSelect sets the tie break for one network.
// Without it, the earliest registered component runs first.
func WithSelect(fn SelectFunc) CoordinatorOption {
	return func(cfg *coordinatorConfig) { cfg.selectFn = fn }
}

// WithOutput receives messages that leave the root coordinator.
// It runs on the event loop and follows the same restrictions as a model.
// Nested networks ignore this option.
func WithOutput(fn func(Message)) CoordinatorOption {
	return func(cfg *coordinatorConfig) { cfg.output = fn }
}

// Coordinator wires atomic models and nested networks onto one engine.
type Coordinator struct {
	schedule *schedule
	network  *network
}

// schedule is the root consistency boundary.
// It owns the one imminent engine event, queued inputs, and shared counters.
type schedule struct {
	mu          sync.Mutex
	eng         *kernel.Engine
	name        string
	epoch       uint64
	scheduledID kernel.ID
	scheduledAt kernel.Time
	pending     []Message
	onOut       func(Message)
	stats       kernel.CoupledStats
	network     *network
}

// NewCoordinator attaches a root coordinator to eng and registers its event name.
func NewCoordinator(eng *kernel.Engine, opts ...CoordinatorOption) (*Coordinator, error) {
	cfg := coordinatorConfig{name: "coordinator"}
	for _, opt := range opts {
		opt(&cfg)
	}
	if eng == nil {
		return nil, ErrEngine
	}
	if cfg.name == "" || strings.Contains(cfg.name, "/") {
		return nil, ErrComponent
	}
	s := &schedule{eng: eng, name: cfg.name, onOut: cfg.output}
	s.network = newNetwork(cfg.selectFn, &s.stats)
	if err := eng.Handle(cfg.name, s.handle); err != nil {
		return nil, err
	}
	return &Coordinator{schedule: s, network: s.network}, nil
}

// Add places an atomic model directly inside this coordinator.
func (c *Coordinator) Add(name string, m Atomic) error {
	if name == "" || strings.Contains(name, "/") {
		return ErrComponent
	}
	if m == nil {
		return ErrModel
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	if _, ok := c.network.byName[name]; ok {
		return ErrExists
	}
	item := &atomicModel{model: m}
	item.sync()
	c.network.add(name, item)
	return c.schedule.arm(nil)
}

// Group adds a nested coupled model and returns it so the caller can wire inside it.
// Only WithSelect applies to the nested model.
func (c *Coordinator) Group(name string, opts ...CoordinatorOption) (*Coordinator, error) {
	if name == "" || strings.Contains(name, "/") {
		return nil, ErrComponent
	}
	cfg := coordinatorConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	if _, ok := c.network.byName[name]; ok {
		return nil, ErrExists
	}
	child := newNetwork(cfg.selectFn, c.schedule.network.stats)
	c.network.add(name, child)
	return &Coordinator{schedule: c.schedule, network: child}, nil
}

// In connects an input port of this coordinator to a child input port.
func (c *Coordinator) In(port, child, childPort string) error {
	if port == "" || childPort == "" {
		return ErrPort
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	if _, ok := c.network.byName[child]; !ok {
		return ErrChild
	}
	c.network.inputs[port] = append(c.network.inputs[port], portRef{child: child, port: childPort})
	return nil
}

// Out connects a child output port to an output port of this coordinator.
func (c *Coordinator) Out(child, childPort, port string) error {
	if port == "" || childPort == "" {
		return ErrPort
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	if _, ok := c.network.byName[child]; !ok {
		return ErrChild
	}
	key := portKey(child, childPort)
	c.network.outputs[key] = append(c.network.outputs[key], port)
	return nil
}

// Link connects one sibling output port to another sibling input port.
func (c *Coordinator) Link(from, fromPort, to, toPort string) error {
	if fromPort == "" || toPort == "" {
		return ErrPort
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	if _, ok := c.network.byName[from]; !ok {
		return ErrChild
	}
	if _, ok := c.network.byName[to]; !ok {
		return ErrChild
	}
	key := portKey(from, fromPort)
	c.network.links[key] = append(c.network.links[key], portRef{child: to, port: toPort})
	return nil
}

// Inject queues an input on a root port.
// The model sees it on the event loop. Inject is only valid on the root.
func (c *Coordinator) Inject(port string, payload []byte) error {
	if c.network != c.schedule.network {
		return ErrRoot
	}
	if port == "" {
		return ErrPort
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	c.schedule.pending = append(c.schedule.pending, Message{Port: port, Payload: clone(payload)})
	return c.schedule.arm(nil)
}

// Snapshot returns the engine snapshot plus coupling counters.
func (c *Coordinator) Snapshot() kernel.Snapshot {
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	snap := c.schedule.eng.Snapshot()
	stats := c.schedule.stats
	stats.Models = c.schedule.network.count()
	snap.Coupled = &stats
	return snap
}

func (s *schedule) handle(ctx kernel.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameEpoch(ctx.Payload(), s.epoch) {
		return nil
	}
	s.scheduledID = 0
	now := ctx.Now()
	s.flush(now)
	if t, ok := s.network.due(); ok && t <= now {
		for _, msg := range s.network.fire(now) {
			if s.onOut != nil {
				s.onOut(msg)
			}
		}
	}
	return s.arm(ctx)
}

func (s *schedule) flush(now kernel.Time) {
	pending := s.pending
	s.pending = nil
	for _, msg := range pending {
		s.network.accept(now, msg)
	}
}

func (s *schedule) arm(ctx kernel.Context) error {
	due, ok := s.network.due()
	now := s.current(ctx)
	if !ok && len(s.pending) == 0 {
		s.clear(ctx)
		return nil
	}
	at := now
	if ok {
		at = due
		if len(s.pending) > 0 && now < at {
			at = now
		}
		if at < now {
			at = now
		}
	}
	if s.scheduledID != 0 && s.scheduledAt == at {
		return nil
	}
	s.clear(ctx)
	s.epoch++
	ev := kernel.Event{Name: s.name, Payload: epochPayload(s.epoch)}
	var id kernel.ID
	var err error
	if ctx != nil {
		if at <= ctx.Now() {
			id, err = ctx.AddFuture(0, ev)
		} else {
			id, err = ctx.AddFuture(time.Duration(at-ctx.Now())*time.Millisecond, ev)
		}
	} else {
		id, err = s.eng.AddEvent(at, ev)
	}
	if err != nil {
		return err
	}
	s.scheduledID = id
	s.scheduledAt = at
	return nil
}

func (s *schedule) clear(ctx kernel.Context) {
	if s.scheduledID == 0 {
		return
	}
	id := s.scheduledID
	s.scheduledID = 0
	if ctx != nil {
		ctx.Cancel(id)
		return
	}
	s.eng.Cancel(id)
}

func (s *schedule) current(ctx kernel.Context) kernel.Time {
	if ctx != nil {
		return ctx.Now()
	}
	return s.eng.Now()
}

func epochPayload(epoch uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], epoch)
	return buf[:]
}

func sameEpoch(payload []byte, epoch uint64) bool {
	if len(payload) != 8 {
		return false
	}
	return binary.BigEndian.Uint64(payload) == epoch
}

const coordinatorVersion = 1

type coordinatorFile struct {
	Version     int
	EventName   string
	Epoch       uint64
	ScheduledID uint64
	ScheduledAt int64
	HasSchedule bool
	Pending     []Message
	Nodes       []componentState
	Messages    uint64
	Due         uint64
	Selects     uint64
	Reschedules uint64
	Engine      []byte
}

type componentState struct {
	Path  string
	Last  int64
	Next  int64
	Has   bool
	State []byte
}

// Checkpoint copies the root coordinator while the engine is paused.
// The engine does not keep this copy. Models that do not implement
// Persistent make the call fail and produce no bytes.
func (c *Coordinator) Checkpoint() ([]byte, error) {
	if c.network != c.schedule.network {
		return nil, ErrRoot
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	nodes, err := c.schedule.network.save("")
	if err != nil {
		return nil, err
	}
	eng, err := c.schedule.eng.Checkpoint()
	if err != nil {
		return nil, err
	}
	file := coordinatorFile{
		Version:     coordinatorVersion,
		EventName:   c.schedule.name,
		Epoch:       c.schedule.epoch,
		ScheduledID: uint64(c.schedule.scheduledID),
		ScheduledAt: int64(c.schedule.scheduledAt),
		HasSchedule: c.schedule.scheduledID != 0,
		Pending:     make([]Message, len(c.schedule.pending)),
		Nodes:       nodes,
		Messages:    c.schedule.stats.Messages,
		Due:         c.schedule.stats.Due,
		Selects:     c.schedule.stats.Selects,
		Reschedules: c.schedule.stats.Reschedules,
		Engine:      eng,
	}
	for i, msg := range c.schedule.pending {
		file.Pending[i] = Message{Port: msg.Port, Payload: clone(msg.Payload)}
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Restore loads a checkpoint into a root coordinator whose models are already
// registered with the same names. The engine must still be in its initial
// state. Restore leaves the engine paused.
func (c *Coordinator) Restore(ctx context.Context, data []byte) error {
	if c.network != c.schedule.network {
		return ErrRoot
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	var file coordinatorFile
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&file); err != nil || file.Version != coordinatorVersion {
		return kernel.ErrCheckpoint
	}
	if file.EventName != c.schedule.name {
		return kernel.ErrCheckpoint
	}
	c.schedule.mu.Lock()
	defer c.schedule.mu.Unlock()
	prev, err := c.schedule.network.save("")
	if err != nil {
		return err
	}
	by := make(map[string]componentState, len(file.Nodes))
	for _, node := range file.Nodes {
		if _, ok := by[node.Path]; ok {
			return kernel.ErrCheckpoint
		}
		by[node.Path] = node
	}
	if err := c.schedule.network.restore("", by); err != nil || len(by) != 0 {
		c.schedule.network.loadAll("", indexNodes(prev))
		if err == nil {
			return kernel.ErrCheckpoint
		}
		return err
	}
	c.schedule.epoch = file.Epoch
	c.schedule.scheduledID = kernel.ID(file.ScheduledID)
	c.schedule.scheduledAt = kernel.Time(file.ScheduledAt)
	if !file.HasSchedule {
		c.schedule.scheduledID = 0
	}
	c.schedule.pending = make([]Message, len(file.Pending))
	for i, msg := range file.Pending {
		c.schedule.pending[i] = Message{Port: msg.Port, Payload: clone(msg.Payload)}
	}
	c.schedule.stats.Messages = file.Messages
	c.schedule.stats.Due = file.Due
	c.schedule.stats.Selects = file.Selects
	c.schedule.stats.Reschedules = file.Reschedules
	if err := c.schedule.eng.Restore(ctx, file.Engine); err != nil {
		c.schedule.network.loadAll("", indexNodes(prev))
		return err
	}
	return nil
}

func indexNodes(nodes []componentState) map[string]componentState {
	by := make(map[string]componentState, len(nodes))
	for _, node := range nodes {
		by[node.Path] = node
	}
	return by
}
