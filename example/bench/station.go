package main

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

// A bay is one packing line. Arrival looks up the parcel and reserves stock.
// Scan and pack only update that parcel. The gaps are simulation delays, not
// a compute loop.

const (
	stageArrive = iota
	stageScan
	stagePack

	openingQty = 40
	restockQty = 24
	nextGap    = 5 * time.Millisecond
)

type sku struct {
	name  string
	large bool
	grams int
}

type shipment struct {
	name  string
	grams int
	seq   int
}

type bay struct {
	catalog []sku
	stock   map[string]int
	seq     int
	stage   int
	cur     sku
	packed  uint64
	recent  [8]shipment
}

func parcelCatalog() []sku {
	return []sku{
		{name: "小包", grams: 400},
		{name: "大包", large: true, grams: 2400},
		{name: "文件", grams: 80},
		{name: "易碎", large: true, grams: 900},
	}
}

func parcelStock(catalog []sku) map[string]int {
	stock := make(map[string]int, len(catalog))
	for _, item := range catalog {
		stock[item.name] = openingQty
	}
	return stock
}

func (b *bay) advance() (string, time.Duration) {
	switch b.stage {
	case stageArrive:
		return "scan", b.receive()
	case stageScan:
		return "pack", b.scan()
	default:
		return "arrive", b.pack()
	}
}

func (b *bay) receive() time.Duration {
	item := b.catalog[b.seq%len(b.catalog)]
	b.seq++
	onHand := b.stock[item.name]
	if onHand < 1 {
		onHand += restockQty
	}
	b.stock[item.name] = onHand - 1
	b.cur = item
	b.stage = stageScan
	if item.large {
		return 4 * time.Millisecond
	}
	return 2 * time.Millisecond
}

func (b *bay) scan() time.Duration {
	if b.cur.name == "" || b.cur.grams <= 0 {
		b.cur = b.catalog[0]
	}
	b.stage = stagePack
	if b.cur.large {
		return 3 * time.Millisecond
	}
	return 2 * time.Millisecond
}

func (b *bay) pack() time.Duration {
	b.recent[b.packed%uint64(len(b.recent))] = shipment{name: b.cur.name, grams: b.cur.grams, seq: b.seq}
	b.packed++
	b.cur = sku{}
	b.stage = stageArrive
	return nextGap
}

func addLine(eng *engine.Engine, catalog []sku, stock map[string]int, first time.Duration, sink *atomic.Uint64) error {
	b := &bay{catalog: catalog, stock: stock}
	var step engine.Handler
	step = func(c engine.Context) error {
		name, after := b.advance()
		sink.Add(1)
		_, err := c.AddFuture(after, engine.Event{Name: name, Handler: step})
		if errors.Is(err, engine.ErrClosed) {
			return nil
		}
		return err
	}
	_, err := eng.AddFuture(first, engine.Event{Name: "arrive", Handler: step})
	if errors.Is(err, engine.ErrClosed) {
		return nil
	}
	return err
}
