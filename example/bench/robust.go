package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

const (
	robustWindow    = 36 * time.Second
	robustSpike     = 400 * time.Millisecond
	robustSample    = 50 * time.Millisecond
	robustMarkStart = "<!-- robust:start -->"
	robustMarkEnd   = "<!-- robust:end -->"
)

func runRobust() error {
	eng, err := engine.NewEngine(
		engine.WithRealtime(true),
		engine.WithExitWhenIdle(false),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		return err
	}
	var waited atomic.Int64
	var spikes atomic.Int64
	expected := 0
	for at := engine.Time(100); at <= 34000; at += 100 {
		expected++
		if _, err := eng.AddEvent(at, engine.Event{
			Name: "wait",
			Handler: func(engine.Context) error {
				waited.Add(1)
				return nil
			},
		}); err != nil {
			return err
		}
	}
	for _, at := range []engine.Time{8000, 16000, 24000} {
		if _, err := eng.AddEvent(at, engine.Event{
			Name:    "heavy",
			Handler: heavy(robustSpike, &spikes),
		}); err != nil {
			return err
		}
	}
	var tick engine.Handler
	tick = func(c engine.Context) error {
		_, err := c.AddFuture(time.Millisecond, engine.Event{Name: "tick", Handler: tick})
		return err
	}
	if _, err := eng.AddEvent(0, engine.Event{Name: "tick", Handler: tick}); err != nil {
		return err
	}
	if err := eng.Start(context.Background()); err != nil {
		return err
	}
	started := time.Now()
	var points []clockPoint
	collect := func() {
		points = append(points, sampleClock(started, time.Duration(eng.Now())*time.Millisecond))
	}
	pulse := time.NewTicker(robustSample)
	defer pulse.Stop()
	end := time.NewTimer(robustWindow)
	defer end.Stop()
wait:
	for {
		select {
		case <-pulse.C:
			collect()
		case <-end.C:
			collect()
			break wait
		}
	}
	if err := eng.Stop(); err != nil {
		return err
	}
	peaks, back := robustShape(points)
	missed := expected - int(waited.Load())
	verdict := "拉回"
	if !back || missed != 0 || peaks < 3 || spikes.Load() != 3 {
		verdict = "拉不回"
	}
	fmt.Fprintf(os.Stderr, "robust peaks=%d back=%v missed=%d spikes=%d %s\n", peaks, back, missed, spikes.Load(), verdict)
	if err := writeRobustReport(points, missed, peaks, verdict); err != nil {
		return err
	}
	if verdict != "拉回" {
		os.Exit(1)
	}
	return nil
}

func heavy(limit time.Duration, spikes *atomic.Int64) engine.Handler {
	return func(engine.Context) error {
		spikes.Add(1)
		deadline := time.Now().Add(limit)
		var x uint64 = 1
		for time.Now().Before(deadline) {
			x = work(32, x)
		}
		return nil
	}
}

// robustShape counts excursions that stay above the 1% line for at least three samples.
// Samples before 2s are ignored because a short wall time turns jitter into a large ratio.
// back is false when the line is still above the limit at the end.
func robustShape(points []clockPoint) (peaks int, back bool) {
	above := false
	run := 0
	for _, p := range points {
		if p.Wall < 2*time.Second {
			continue
		}
		if p.Ratio > clockLimit {
			run++
			if run == 3 {
				peaks++
			}
			above = true
			continue
		}
		run = 0
		above = false
	}
	if len(points) == 0 {
		return 0, false
	}
	return peaks, !above && points[len(points)-1].Ratio <= clockLimit
}

func writeRobustReport(points []clockPoint, missed, peaks int, verdict string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	svg := drawClockChart([]clockSeries{{
		Label:  "复杂函数",
		Points: points,
	}})
	dir := filepath.Join(root, "example", "bench")
	if err := os.WriteFile(filepath.Join(dir, "robust.svg"), []byte(svg), 0o644); err != nil {
		return err
	}
	if err := renderPNG(svg, filepath.Join(dir, "robust.png"), clockChartW, clockChartH); err != nil {
		return err
	}
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	section := robustSection(points, missed, peaks, verdict)
	next := replaceMarked(string(readme), robustMarkStart, robustMarkEnd, section)
	return os.WriteFile(readmePath, []byte(next), 0o644)
}

func robustSection(points []clockPoint, missed, peaks int, verdict string) string {
	last := clockPoint{}
	maxRatio := 0.0
	for _, p := range points {
		if p.Ratio > maxRatio {
			maxRatio = p.Ratio
		}
	}
	if len(points) > 0 {
		last = points[len(points)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "真实模式。每 100 毫秒有一个等待事件，第 8、16、24 秒各有一个处理函数空转 %s。比值高于 1%% 后必须落回来，等待事件不能少。末尾仍高于 1%% 记为拉不回。\n\n", robustSpike)
	b.WriteString("![超长处理后的偏差比值](example/bench/robust.png)\n\n")
	b.WriteString("| 波峰 | 峰值 | 结束比值 | 漏掉的等待事件 | 结论 |\n")
	b.WriteString("| ---: | ---: | ---: | ---: | --- |\n")
	fmt.Fprintf(&b, "| %d | %.2f%% | %.4f%% | %d | %s |\n\n", peaks, maxRatio*100, last.Ratio*100, missed, verdict)
	b.WriteString("```bash\ngo run ./example/bench -robust\n```\n")
	return b.String()
}

func replaceMarked(readme, startMark, endMark, section string) string {
	start := strings.Index(readme, startMark)
	end := strings.Index(readme, endMark)
	block := startMark + "\n" + section + "\n" + endMark + "\n"
	if start >= 0 && end > start {
		return readme[:start] + block + readme[end+len(endMark):]
	}
	return readme + "\n" + block
}
