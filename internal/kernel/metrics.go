package kernel

import (
	"github.com/cgcodeboy/des-engine/internal/pkgs/metrics"
)

// StatusJSON encodes the snapshot as one JSON object.
// Coupled is omitted when nil. The engine does not serve this body.
func (s Snapshot) StatusJSON() ([]byte, error) {
	return metrics.JSON(reportOf(s))
}

// Prometheus renders the snapshot as Prometheus text.
// Slow event names are not labels. Coupled lines are omitted when nil.
// The engine does not serve this text.
func (s Snapshot) Prometheus() string {
	return metrics.Prometheus(reportOf(s))
}

func reportOf(s Snapshot) metrics.Report {
	out := metrics.Report{
		State:           s.State,
		Now:             int64(s.Now),
		Step:            s.Step,
		Lag:             s.Lag,
		EventsPending:   s.EventsPending,
		StepPending:     s.StepPending,
		EventsScheduled: s.EventsScheduled,
		EventsProcessed: s.EventsProcessed,
		EventsCancelled: s.EventsCancelled,
		EventsRejected:  s.EventsRejected,
		HandlerErrors:   s.HandlerErrors,
		CatchupSteps:    s.CatchupSteps,
		SlowTotal:       s.SlowTotal,
		LogDropped:      s.LogDropped,
		Uptime:          s.Uptime,
		EventRate:       s.EventRate,
		Slow:            make([]metrics.Slow, len(s.SlowEvents)),
	}
	if s.Coupled != nil {
		out.Coupled = &metrics.Coupled{
			Models:      s.Coupled.Models,
			Messages:    s.Coupled.Messages,
			Due:         s.Coupled.Due,
			Selects:     s.Coupled.Selects,
			Reschedules: s.Coupled.Reschedules,
		}
	}
	for i, ev := range s.SlowEvents {
		out.Slow[i] = metrics.Slow{
			ID:       uint64(ev.ID),
			Name:     ev.Name,
			At:       int64(ev.At),
			Step:     ev.Step,
			Elapsed:  ev.Elapsed,
			Overrun:  ev.Overrun,
			Finished: ev.Finished,
		}
	}
	return out
}
