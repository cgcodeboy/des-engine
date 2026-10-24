// Package bucket stores future simulation steps in insertion order within each step.
// The caller must hold the engine lock. This package does not lock.
package bucket

import "container/heap"

// Buckets is a set of non-empty steps, ordered by step index.
// T is the stored event. This package does not inspect it.
type Buckets[T any] struct {
	items map[int64][]T
	steps stepHeap
	n     int
}

// Push appends ev to step, preserving earlier events in that step.
// The first event in a step records the step index for Next.
func (b *Buckets[T]) Push(step int64, ev T) {
	if b.items == nil {
		b.items = make(map[int64][]T)
	}
	prev := b.items[step]
	b.items[step] = append(prev, ev)
	if len(prev) == 0 {
		heap.Push(&b.steps, step)
	}
	b.n++
}

// Next returns the smallest occupied step at or after minStep.
// Occupied steps below minStep stay stored.
func (b *Buckets[T]) Next(minStep int64) (int64, bool) {
	var skipped []int64
	for b.steps.Len() > 0 {
		step := b.steps[0]
		if _, ok := b.items[step]; !ok {
			heap.Pop(&b.steps)
			continue
		}
		if step < minStep {
			heap.Pop(&b.steps)
			skipped = append(skipped, step)
			continue
		}
		for _, s := range skipped {
			heap.Push(&b.steps, s)
		}
		return step, true
	}
	for _, s := range skipped {
		heap.Push(&b.steps, s)
	}
	return 0, false
}

// Take removes and returns every event stored at step, in insertion order.
// The step index may remain in the heap until Next observes that it is empty.
func (b *Buckets[T]) Take(step int64) []T {
	events := b.items[step]
	delete(b.items, step)
	b.n -= len(events)
	if b.n < 0 {
		b.n = 0
	}
	return events
}

// Len returns the number of events stored across all future steps.
func (b *Buckets[T]) Len() int {
	return b.n
}

type stepHeap []int64

func (h stepHeap) Len() int           { return len(h) }
func (h stepHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h stepHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *stepHeap) Push(x any) {
	*h = append(*h, x.(int64))
}

func (h *stepHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
