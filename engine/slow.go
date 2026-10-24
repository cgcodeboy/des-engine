package engine

const slowCap = 128

func (e *Engine) appendSlow(ev SlowEvent) {
	e.slowTotal++
	if len(e.slow) == slowCap {
		copy(e.slow, e.slow[1:])
		e.slow[slowCap-1] = ev
		return
	}
	e.slow = append(e.slow, ev)
}
