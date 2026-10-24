// Package metrics renders an engine snapshot as JSON or Prometheus text.
// It does not listen or import the engine.
package metrics

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Report is the snapshot data needed to render status text.
type Report struct {
	State           string
	Now             int64
	Step            int64
	Lag             time.Duration
	EventsPending   int
	StepPending     int
	EventsScheduled uint64
	EventsProcessed uint64
	EventsCancelled uint64
	EventsRejected  uint64
	HandlerErrors   uint64
	CatchupSteps    uint64
	SlowTotal       uint64
	LogDropped      uint64
	Uptime          time.Duration
	EventRate       float64
	Slow            []Slow
	Coupled         *Coupled
}

// Slow is one handler that exceeded the step.
type Slow struct {
	ID       uint64
	Name     string
	At       int64
	Step     int64
	Elapsed  time.Duration
	Overrun  time.Duration
	Finished time.Time
}

// Coupled counts the optional coupling layer.
type Coupled struct {
	Models      uint64
	Messages    uint64
	Due         uint64
	Selects     uint64
	Reschedules uint64
}

type statusJSON struct {
	State           string       `json:"state"`
	NowMS           int64        `json:"now_ms"`
	Step            int64        `json:"step"`
	LagSeconds      float64      `json:"lag_seconds"`
	EventsPending   int          `json:"events_pending"`
	StepPending     int          `json:"step_pending"`
	EventsScheduled uint64       `json:"events_scheduled"`
	EventsProcessed uint64       `json:"events_processed"`
	EventsCancelled uint64       `json:"events_cancelled"`
	EventsRejected  uint64       `json:"events_rejected"`
	HandlerErrors   uint64       `json:"handler_errors"`
	CatchupSteps    uint64       `json:"catchup_steps"`
	SlowTotal       uint64       `json:"slow_total"`
	LogDropped      uint64       `json:"log_dropped"`
	UptimeSeconds   float64      `json:"uptime_seconds"`
	EventRate       float64      `json:"event_rate"`
	SlowEvents      []slowJSON   `json:"slow_events"`
	Coupled         *coupledJSON `json:"coupled,omitempty"`
}

type coupledJSON struct {
	Models      uint64 `json:"models"`
	Messages    uint64 `json:"messages"`
	Due         uint64 `json:"due"`
	Selects     uint64 `json:"selects"`
	Reschedules uint64 `json:"reschedules"`
}

type slowJSON struct {
	ID             uint64  `json:"id"`
	Name           string  `json:"name"`
	AtMS           int64   `json:"at_ms"`
	Step           int64   `json:"step"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	OverrunSeconds float64 `json:"overrun_seconds"`
	Finished       string  `json:"finished"`
}

// JSON encodes r as one status object.
func JSON(r Report) ([]byte, error) {
	return json.Marshal(toStatus(r))
}

func toStatus(r Report) statusJSON {
	out := statusJSON{
		State:           r.State,
		NowMS:           r.Now,
		Step:            r.Step,
		LagSeconds:      seconds(r.Lag),
		EventsPending:   r.EventsPending,
		StepPending:     r.StepPending,
		EventsScheduled: r.EventsScheduled,
		EventsProcessed: r.EventsProcessed,
		EventsCancelled: r.EventsCancelled,
		EventsRejected:  r.EventsRejected,
		HandlerErrors:   r.HandlerErrors,
		CatchupSteps:    r.CatchupSteps,
		SlowTotal:       r.SlowTotal,
		LogDropped:      r.LogDropped,
		UptimeSeconds:   seconds(r.Uptime),
		EventRate:       r.EventRate,
		SlowEvents:      make([]slowJSON, len(r.Slow)),
	}
	if r.Coupled != nil {
		out.Coupled = &coupledJSON{
			Models:      r.Coupled.Models,
			Messages:    r.Coupled.Messages,
			Due:         r.Coupled.Due,
			Selects:     r.Coupled.Selects,
			Reschedules: r.Coupled.Reschedules,
		}
	}
	for i, ev := range r.Slow {
		out.SlowEvents[i] = slowJSON{
			ID:             ev.ID,
			Name:           ev.Name,
			AtMS:           ev.At,
			Step:           ev.Step,
			ElapsedSeconds: seconds(ev.Elapsed),
			OverrunSeconds: seconds(ev.Overrun),
			Finished:       ev.Finished.UTC().Format(time.RFC3339Nano),
		}
	}
	return out
}

func seconds(d time.Duration) float64 {
	return d.Seconds()
}

// Prometheus renders r as Prometheus text without event-name labels.
func Prometheus(r Report) string {
	up := 0
	if r.State == "running" {
		up = 1
	}
	lines := []struct {
		name  string
		value string
	}{
		{"des_up", strconv.Itoa(up)},
		{"des_sim_time_milliseconds", strconv.FormatInt(r.Now, 10)},
		{"des_step", strconv.FormatInt(r.Step, 10)},
		{"des_lag_seconds", formatFloat(seconds(r.Lag))},
		{"des_events_pending", strconv.Itoa(r.EventsPending)},
		{"des_events_scheduled_total", strconv.FormatUint(r.EventsScheduled, 10)},
		{"des_events_processed_total", strconv.FormatUint(r.EventsProcessed, 10)},
		{"des_events_cancelled_total", strconv.FormatUint(r.EventsCancelled, 10)},
		{"des_events_rejected_total", strconv.FormatUint(r.EventsRejected, 10)},
		{"des_handler_errors_total", strconv.FormatUint(r.HandlerErrors, 10)},
		{"des_catchup_steps_total", strconv.FormatUint(r.CatchupSteps, 10)},
		{"des_slow_events_total", strconv.FormatUint(r.SlowTotal, 10)},
		{"des_log_dropped_total", strconv.FormatUint(r.LogDropped, 10)},
	}
	var b []byte
	for _, line := range lines {
		b = fmt.Appendf(b, "%s %s\n", line.name, line.value)
	}
	if r.Coupled != nil {
		extra := []struct {
			name  string
			value string
		}{
			{"coupled_models", strconv.FormatUint(r.Coupled.Models, 10)},
			{"coupled_messages_total", strconv.FormatUint(r.Coupled.Messages, 10)},
			{"coupled_due_total", strconv.FormatUint(r.Coupled.Due, 10)},
			{"coupled_selects_total", strconv.FormatUint(r.Coupled.Selects, 10)},
			{"coupled_reschedules_total", strconv.FormatUint(r.Coupled.Reschedules, 10)},
		}
		for _, line := range extra {
			b = fmt.Appendf(b, "%s %s\n", line.name, line.value)
		}
	}
	return string(b)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
