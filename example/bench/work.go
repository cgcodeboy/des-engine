package main

// work runs a fixed amount of integer mixing. The result is consumed by the
// caller so the compiler keeps the loop.
func work(iters int, salt uint64) uint64 {
	x := salt | 1
	for i := 0; i < iters; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		x += uint64(i)
	}
	return x
}
