package main

import (
	"testing"
	"time"
)

func TestBayReservesThenPacks(t *testing.T) {
	catalog := parcelCatalog()
	stock := parcelStock(catalog)
	b := &bay{catalog: catalog, stock: stock}

	name, after := b.advance()
	if name != "scan" || after != 2*time.Millisecond {
		t.Fatalf("arrive = %s %s", name, after)
	}
	if stock["小包"] != openingQty-1 {
		t.Fatalf("stock = %d", stock["小包"])
	}

	name, after = b.advance()
	if name != "pack" || after != 2*time.Millisecond {
		t.Fatalf("scan = %s %s", name, after)
	}
	name, after = b.advance()
	if name != "arrive" || after != nextGap {
		t.Fatalf("pack = %s %s", name, after)
	}
	if b.packed != 1 || b.recent[0].name != "小包" || b.recent[0].grams != 400 {
		t.Fatalf("shipment = %+v packed %d", b.recent[0], b.packed)
	}
}

func TestLargeParcelTakesLonger(t *testing.T) {
	catalog := parcelCatalog()
	b := &bay{catalog: catalog, stock: parcelStock(catalog)}
	for i := 0; i < 3; i++ {
		b.advance()
	}
	name, after := b.advance()
	if name != "scan" || after != 4*time.Millisecond {
		t.Fatalf("large arrive = %s %s", name, after)
	}
	name, after = b.advance()
	if name != "pack" || after != 3*time.Millisecond {
		t.Fatalf("large scan = %s %s", name, after)
	}
}

func TestBayRestocksEmptySKU(t *testing.T) {
	catalog := parcelCatalog()
	stock := parcelStock(catalog)
	stock["小包"] = 0
	b := &bay{catalog: catalog, stock: stock}
	b.advance()
	if stock["小包"] != restockQty-1 {
		t.Fatalf("stock = %d", stock["小包"])
	}
}
