package devs

import (
	"time"

	"github.com/cgcodeboy/des-engine/internal/kernel"
)

// network is one coupled model: its components and the ports between them.
type network struct {
	components []*component
	byName     map[string]*component
	inputs     map[string][]portRef
	outputs    map[string][]string
	links      map[string][]portRef
	pick       SelectFunc
	stats      *kernel.CoupledStats
}

// component is one child of a network. Its body is an atomic model or a nested network.
type component struct {
	name string
	node any
}

// portRef names a component port inside one network.
type portRef struct {
	child string
	port  string
}

// atomicModel stores one atomic model and the simulation time of its next internal event.
type atomicModel struct {
	model Atomic
	last  kernel.Time
	next  kernel.Time
	has   bool
}

func newNetwork(pick SelectFunc, stats *kernel.CoupledStats) *network {
	return &network{
		byName:  map[string]*component{},
		inputs:  map[string][]portRef{},
		outputs: map[string][]string{},
		links:   map[string][]portRef{},
		pick:    pick,
		stats:   stats,
	}
}

func (g *network) add(name string, node any) {
	item := &component{name: name, node: node}
	g.components = append(g.components, item)
	g.byName[name] = item
}

func (g *network) due() (kernel.Time, bool) {
	var best kernel.Time
	found := false
	for _, comp := range g.components {
		at, ok := dueOf(comp.node)
		if !ok {
			continue
		}
		if !found || at < best {
			best = at
			found = true
		}
	}
	return best, found
}

func (g *network) fire(now kernel.Time) []Message {
	names := g.imminent(now)
	if len(names) == 0 {
		return nil
	}
	chosen := names[0]
	if len(names) > 1 {
		g.stats.Selects++
		if g.pick != nil {
			if name := g.pick(append([]string(nil), names...)); contains(names, name) {
				chosen = name
			}
		}
	}
	child := g.byName[chosen]
	var produced []Message
	switch node := child.node.(type) {
	case *atomicModel:
		g.stats.Due++
		produced = node.pop(now)
	case *network:
		produced = node.fire(now)
	}
	return g.route(now, chosen, produced)
}

func (g *network) imminent(now kernel.Time) []string {
	var names []string
	var best kernel.Time
	found := false
	for _, comp := range g.components {
		at, ok := dueOf(comp.node)
		if !ok || at > now {
			continue
		}
		if !found || at < best {
			best = at
			found = true
			names = append(names[:0], comp.name)
			continue
		}
		if at == best {
			names = append(names, comp.name)
		}
	}
	return names
}

func (g *network) route(now kernel.Time, from string, msgs []Message) []Message {
	var leave []Message
	for _, msg := range msgs {
		key := portKey(from, msg.Port)
		for _, dest := range g.links[key] {
			g.stats.Messages++
			g.send(now, dest.child, Message{Port: dest.port, Payload: msg.Payload})
		}
		for _, port := range g.outputs[key] {
			g.stats.Messages++
			leave = append(leave, Message{Port: port, Payload: clone(msg.Payload)})
		}
	}
	return leave
}

func (g *network) accept(now kernel.Time, msg Message) {
	for _, dest := range g.inputs[msg.Port] {
		g.stats.Messages++
		g.send(now, dest.child, Message{Port: dest.port, Payload: msg.Payload})
	}
}

func (g *network) send(now kernel.Time, child string, msg Message) {
	msg.Payload = clone(msg.Payload)
	switch node := g.byName[child].node.(type) {
	case *atomicModel:
		if node.external(now, msg) {
			g.stats.Reschedules++
		}
	case *network:
		node.accept(now, msg)
	}
}

func (g *network) count() uint64 {
	var n uint64
	for _, comp := range g.components {
		switch node := comp.node.(type) {
		case *atomicModel:
			n++
		case *network:
			n += node.count()
		}
	}
	return n
}

func (a *atomicModel) pop(now kernel.Time) []Message {
	out := a.model.Output()
	a.model.Internal()
	a.last = now
	a.sync()
	return out
}

func (a *atomicModel) external(now kernel.Time, msg Message) bool {
	elapsed := time.Duration(now-a.last) * time.Millisecond
	prev, prevOK := a.next, a.has
	a.model.External(elapsed, msg)
	a.last = now
	a.sync()
	if prevOK != a.has || prev != a.next {
		return true
	}
	return false
}

func (a *atomicModel) sync() {
	after, ok := a.model.Advance()
	if !ok {
		a.has = false
		return
	}
	if after < 0 {
		after = 0
	}
	a.next = a.last + kernel.Time((after+time.Millisecond-1)/time.Millisecond)
	a.has = true
}

func dueOf(node any) (kernel.Time, bool) {
	switch n := node.(type) {
	case *atomicModel:
		return n.next, n.has
	case *network:
		return n.due()
	default:
		return 0, false
	}
}

func portKey(name, port string) string {
	return name + "\x00" + port
}

func contains(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}

func clone(payload []byte) []byte {
	return append([]byte(nil), payload...)
}

func (g *network) save(prefix string) ([]componentState, error) {
	var out []componentState
	for _, comp := range g.components {
		path := comp.name
		if prefix != "" {
			path = prefix + "/" + comp.name
		}
		switch node := comp.node.(type) {
		case *atomicModel:
			item, err := node.save(path)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		case *network:
			nested, err := node.save(path)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
		}
	}
	return out, nil
}

func (a *atomicModel) save(path string) (componentState, error) {
	store, ok := a.model.(Persistent)
	if !ok {
		return componentState{}, ErrState
	}
	state, err := store.Save()
	if err != nil {
		return componentState{}, err
	}
	return componentState{
		Path:  path,
		Last:  int64(a.last),
		Next:  int64(a.next),
		Has:   a.has,
		State: clone(state),
	}, nil
}

func (g *network) restore(prefix string, by map[string]componentState) error {
	for _, comp := range g.components {
		path := comp.name
		if prefix != "" {
			path = prefix + "/" + comp.name
		}
		switch node := comp.node.(type) {
		case *atomicModel:
			item, ok := by[path]
			if !ok {
				return kernel.ErrCheckpoint
			}
			if err := node.load(item); err != nil {
				return err
			}
			delete(by, path)
		case *network:
			if err := node.restore(path, by); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *network) loadAll(prefix string, by map[string]componentState) {
	for _, comp := range g.components {
		path := comp.name
		if prefix != "" {
			path = prefix + "/" + comp.name
		}
		switch node := comp.node.(type) {
		case *atomicModel:
			if item, ok := by[path]; ok {
				_ = node.load(item) // rollback restores the previous bytes
			}
		case *network:
			node.loadAll(path, by)
		}
	}
}

func (a *atomicModel) load(node componentState) error {
	store, ok := a.model.(Persistent)
	if !ok {
		return ErrState
	}
	if err := store.Load(node.State); err != nil {
		return err
	}
	a.last = kernel.Time(node.Last)
	a.next = kernel.Time(node.Next)
	a.has = node.Has
	return nil
}
