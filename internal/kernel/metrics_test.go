package kernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatusTextOmitsNamesAndCoupling(t *testing.T) {
	snap := Snapshot{
		State:           "running",
		Now:             118,
		Step:            118,
		SlowTotal:       1,
		EventsProcessed: 4,
		SlowEvents: []SlowEvent{{
			ID:       7,
			Name:     "rebuild",
			At:       118,
			Step:     118,
			Elapsed:  2500 * time.Microsecond,
			Overrun:  1500 * time.Microsecond,
			Finished: time.Date(2026, 10, 24, 2, 0, 0, 118000000, time.UTC),
		}},
	}
	body, err := snap.StatusJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SlowTotal  uint64 `json:"slow_total"`
		SlowEvents []struct {
			Name           string  `json:"name"`
			AtMS           int64   `json:"at_ms"`
			ElapsedSeconds float64 `json:"elapsed_seconds"`
		} `json:"slow_events"`
		Coupled *struct{} `json:"coupled"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SlowTotal != 1 || len(decoded.SlowEvents) != 1 || decoded.SlowEvents[0].Name != "rebuild" {
		t.Fatalf("status = %+v", decoded)
	}
	if decoded.SlowEvents[0].AtMS != 118 || decoded.SlowEvents[0].ElapsedSeconds <= 0 {
		t.Fatalf("slow = %+v", decoded.SlowEvents[0])
	}
	if decoded.Coupled != nil {
		t.Fatal("coupled was present")
	}
	text := snap.Prometheus()
	if !strings.Contains(text, "des_slow_events_total 1\n") || !strings.Contains(text, "des_up 1\n") {
		t.Fatalf("prometheus = %s", text)
	}
	if strings.Contains(text, "rebuild") || strings.Contains(text, "coupled_") {
		t.Fatalf("prometheus leaked name or coupling: %s", text)
	}
}

func TestPrometheusIncludesCoupledWhenSet(t *testing.T) {
	snap := Snapshot{
		State: "paused",
		Coupled: &CoupledStats{
			Models:      3,
			Messages:    4,
			Due:         2,
			Selects:     1,
			Reschedules: 1,
		},
	}
	text := snap.Prometheus()
	if !strings.Contains(text, "des_up 0\n") || !strings.Contains(text, "coupled_models 3\n") || !strings.Contains(text, "coupled_reschedules_total 1\n") {
		t.Fatalf("prometheus = %s", text)
	}
	body, err := snap.StatusJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"models":3`) {
		t.Fatalf("status = %s", body)
	}
}
