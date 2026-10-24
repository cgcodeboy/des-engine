package bucket

import "testing"

func TestTakeKeepsInsertionOrder(t *testing.T) {
	var b Buckets[int]
	b.Push(4, 1)
	b.Push(4, 2)
	b.Push(4, 3)
	got := b.Take(4)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("order = %v", got)
	}
}

func TestNextSkipsEarlierAndEmptySteps(t *testing.T) {
	var b Buckets[int]
	b.Push(1, 10)
	b.Push(3, 30)
	b.Take(1)
	step, ok := b.Next(2)
	if !ok || step != 3 {
		t.Fatalf("Next(2) = %d %v", step, ok)
	}
	step, ok = b.Next(0)
	if !ok || step != 3 {
		t.Fatalf("Next(0) = %d %v", step, ok)
	}
	b.Push(1, 11)
	step, ok = b.Next(0)
	if !ok || step != 1 {
		t.Fatalf("restored Next(0) = %d %v", step, ok)
	}
	step, ok = b.Next(2)
	if !ok || step != 3 {
		t.Fatalf("Next(2) after restore = %d %v", step, ok)
	}
}

func TestTakeRemovesStepFromNext(t *testing.T) {
	var b Buckets[string]
	b.Push(8, "a")
	b.Take(8)
	if _, ok := b.Next(0); ok {
		t.Fatal("taken step still returned")
	}
	if b.Len() != 0 {
		t.Fatalf("len = %d", b.Len())
	}
}
