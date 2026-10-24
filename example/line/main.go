// Command line wires three models on one coordinator.
// Arrival times and service times belong to the models.
// The coordinator only carries a finished job from one port to the next.
//
//	go run ./example/line
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

func main() {
	var lines []string
	eng, err := engine.NewEngine(
		engine.WithRealtime(false),
		engine.WithExitWhenIdle(true),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		exit(err)
	}
	root, err := engine.NewCoordinator(eng)
	if err != nil {
		exit(err)
	}
	if err := root.Add("dock", &dock{lines: &lines, items: []job{
		{name: "小包", gap: 5 * time.Millisecond},
		{name: "大包"},
	}}); err != nil {
		exit(err)
	}
	if err := root.Add("scan", &station{kind: "扫描", lines: &lines}); err != nil {
		exit(err)
	}
	if err := root.Add("pack", &station{kind: "打包", lines: &lines}); err != nil {
		exit(err)
	}
	if err := root.Link("dock", "out", "scan", "in"); err != nil {
		exit(err)
	}
	if err := root.Link("scan", "out", "pack", "in"); err != nil {
		exit(err)
	}
	if err := root.Out("pack", "out", "done"); err != nil {
		exit(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		exit(err)
	}
	if err := eng.Wait(); err != nil {
		exit(err)
	}
	fmt.Println("到货口、扫描台、打包台靠端口连接。处理时间由包裹自己决定。")
	for _, line := range lines {
		fmt.Println(line)
	}
}

type job struct {
	name string
	gap  time.Duration
}

type dock struct {
	items []job
	idx   int
	at    engine.Time
	lines *[]string
}

func (d *dock) Advance() (time.Duration, bool) {
	if d.idx >= len(d.items) {
		return 0, false
	}
	if d.idx == 0 {
		return 0, true
	}
	return d.items[d.idx-1].gap, true
}

func (d *dock) Output() []engine.Message {
	d.note("到货 " + d.items[d.idx].name)
	return []engine.Message{{Port: "out", Payload: []byte(d.items[d.idx].name)}}
}

func (d *dock) Internal() {
	gap := d.items[d.idx].gap
	d.idx++
	if d.idx < len(d.items) {
		d.at += engine.Time(gap / time.Millisecond)
	}
}

func (d *dock) External(time.Duration, engine.Message) {}

func (d *dock) note(text string) {
	*d.lines = append(*d.lines, fmt.Sprintf("%4d ms  %s", d.at, text))
}

type station struct {
	kind  string
	job   []byte
	at    engine.Time
	lines *[]string
}

func (s *station) Advance() (time.Duration, bool) {
	if s.job == nil {
		return 0, false
	}
	return service(s.kind, s.job), true
}

func (s *station) Output() []engine.Message {
	return []engine.Message{{Port: "out", Payload: clone(s.job)}}
}

func (s *station) Internal() {
	s.at += engine.Time(service(s.kind, s.job) / time.Millisecond)
	s.note("完成" + s.kind + " " + string(s.job))
	s.job = nil
}

func (s *station) External(elapsed time.Duration, msg engine.Message) {
	s.at += engine.Time(elapsed / time.Millisecond)
	s.job = clone(msg.Payload)
	s.note("开始" + s.kind + " " + string(s.job))
}

func (s *station) note(text string) {
	*s.lines = append(*s.lines, fmt.Sprintf("%4d ms  %s", s.at, text))
}

func service(kind string, job []byte) time.Duration {
	if string(job) == "大包" {
		if kind == "扫描" {
			return 4 * time.Millisecond
		}
		return 3 * time.Millisecond
	}
	if kind == "扫描" {
		return 2 * time.Millisecond
	}
	return 2 * time.Millisecond
}

func clone(payload []byte) []byte {
	return append([]byte(nil), payload...)
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
