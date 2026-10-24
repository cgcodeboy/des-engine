package metrics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

type fakeSource struct {
	snap engine.Snapshot
}

func (f fakeSource) Snapshot() engine.Snapshot { return f.snap }

func TestStatusAndMetrics(t *testing.T) {
	src := fakeSource{snap: engine.Snapshot{
		State:           "running",
		Now:             118,
		Step:            118,
		SlowTotal:       1,
		EventsProcessed: 4,
		SlowEvents: []engine.SlowEvent{{
			ID:       7,
			Name:     "rebuild",
			At:       118,
			Step:     118,
			Elapsed:  2500 * time.Microsecond,
			Overrun:  1500 * time.Microsecond,
			Finished: time.Date(2026, 10, 24, 2, 0, 0, 118000000, time.UTC),
		}},
	}}
	srv := httptest.NewServer(newMux(src))
	defer srv.Close()

	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("health = %d %q", health.StatusCode, body)
	}

	status, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer status.Body.Close()
	var decoded statusJSON
	if err := json.NewDecoder(status.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SlowTotal != 1 || len(decoded.SlowEvents) != 1 || decoded.SlowEvents[0].Name != "rebuild" {
		t.Fatalf("status = %+v", decoded)
	}
	if decoded.SlowEvents[0].AtMS != 118 || decoded.SlowEvents[0].ElapsedSeconds <= 0 {
		t.Fatalf("slow = %+v", decoded.SlowEvents[0])
	}

	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer metricsResp.Body.Close()
	text, _ := io.ReadAll(metricsResp.Body)
	got := string(text)
	if !strings.Contains(got, "des_slow_events_total 1") {
		t.Fatalf("metrics = %s", got)
	}
	if strings.Contains(got, "rebuild") || strings.Contains(got, "event_name") {
		t.Fatalf("metrics leaked event name: %s", got)
	}
}

func TestListenStopsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Listen(ctx, addr, fakeSource{snap: engine.Snapshot{State: "running"}}, slog.New(slog.DiscardHandler))
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not return")
	}
}
