// Package metrics serves engine snapshots over HTTP with Gin.
//
// GET /healthz reports whether this server is listening, not whether the
// engine is still running. GET /status returns a JSON snapshot.
// GET /metrics returns Prometheus text without event-name labels.
package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

type statusJSON struct {
	State           string     `json:"state"`
	NowMS           int64      `json:"now_ms"`
	Step            int64      `json:"step"`
	LagSeconds      float64    `json:"lag_seconds"`
	EventsPending   int        `json:"events_pending"`
	StepPending     int        `json:"step_pending"`
	EventsScheduled uint64     `json:"events_scheduled"`
	EventsProcessed uint64     `json:"events_processed"`
	EventsCancelled uint64     `json:"events_cancelled"`
	EventsRejected  uint64     `json:"events_rejected"`
	HandlerErrors   uint64     `json:"handler_errors"`
	CatchupSteps    uint64     `json:"catchup_steps"`
	SlowTotal       uint64     `json:"slow_total"`
	LogDropped      uint64     `json:"log_dropped"`
	UptimeSeconds   float64    `json:"uptime_seconds"`
	EventRate       float64    `json:"event_rate"`
	SlowEvents      []slowJSON `json:"slow_events"`
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

func toStatus(s engine.Snapshot) statusJSON {
	out := statusJSON{
		State:           s.State,
		NowMS:           int64(s.Now),
		Step:            s.Step,
		LagSeconds:      seconds(s.Lag),
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
		UptimeSeconds:   seconds(s.Uptime),
		EventRate:       s.EventRate,
		SlowEvents:      make([]slowJSON, len(s.SlowEvents)),
	}
	for i, ev := range s.SlowEvents {
		out.SlowEvents[i] = slowJSON{
			ID:             uint64(ev.ID),
			Name:           ev.Name,
			AtMS:           int64(ev.At),
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

func prometheus(s engine.Snapshot) string {
	up := 0
	if s.State == "running" {
		up = 1
	}
	lines := []struct {
		name  string
		value string
	}{
		{"des_up", strconv.Itoa(up)},
		{"des_sim_time_milliseconds", strconv.FormatInt(int64(s.Now), 10)},
		{"des_step", strconv.FormatInt(s.Step, 10)},
		{"des_lag_seconds", formatFloat(seconds(s.Lag))},
		{"des_events_pending", strconv.Itoa(s.EventsPending)},
		{"des_events_scheduled_total", strconv.FormatUint(s.EventsScheduled, 10)},
		{"des_events_processed_total", strconv.FormatUint(s.EventsProcessed, 10)},
		{"des_events_cancelled_total", strconv.FormatUint(s.EventsCancelled, 10)},
		{"des_events_rejected_total", strconv.FormatUint(s.EventsRejected, 10)},
		{"des_handler_errors_total", strconv.FormatUint(s.HandlerErrors, 10)},
		{"des_catchup_steps_total", strconv.FormatUint(s.CatchupSteps, 10)},
		{"des_slow_events_total", strconv.FormatUint(s.SlowTotal, 10)},
		{"des_log_dropped_total", strconv.FormatUint(s.LogDropped, 10)},
	}
	var b []byte
	for _, line := range lines {
		b = fmt.Appendf(b, "%s %s\n", line.name, line.value)
	}
	return string(b)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
