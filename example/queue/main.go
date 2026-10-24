// Command queue runs one machine that serves one job at a time.
// The waiting line belongs to this program. The engine only orders the events.
//
//	go run ./example/queue
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

const service = 4 * time.Millisecond

func main() {
	var shop shop
	eng, err := engine.New(
		engine.WithRealtime(false),
		engine.WithExitWhenIdle(true),
		engine.WithHorizon(20),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, job := range []struct {
		at   engine.Time
		name string
	}{
		{0, "A"},
		{5, "B"},
		{5, "C"},
		{8, "E"},
		{12, "D"},
		{30, "F"},
	} {
		id, err := eng.AddEvent(job.at, engine.Event{Name: "arrive-" + job.name, Handler: shop.arrive(job.name)})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if job.name == "E" {
			eng.Cancel(id)
		}
	}
	if err := eng.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := eng.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("单服务台，每件服务 4 ms。同一毫秒里先插入的先执行。")
	fmt.Println("E 在启动前取消。时间上限是 20 ms，F 排在 30 ms。")
	for _, line := range shop.lines {
		fmt.Println(line)
	}
	snap := eng.Snapshot()
	fmt.Printf("结束  状态 %s  仿真时间 %d ms\n", snap.State, snap.Now)
	fmt.Println("时间线里没有 E，也没有 F")
}

type shop struct {
	busy    bool
	wait    []string
	lines   []string
	checked bool
}

func (s *shop) line(now engine.Time, text string) {
	s.lines = append(s.lines, fmt.Sprintf("%4d ms  %s", now, text))
}

func (s *shop) arrive(name string) engine.Handler {
	return func(c engine.Context) error {
		if !s.checked {
			s.checked = true
			_, err := c.AddEvent(c.Now()-1, engine.Event{
				Name:    "past",
				Handler: func(engine.Context) error { return nil },
			})
			if err == nil {
				return fmt.Errorf("past event was accepted")
			}
			if err != engine.ErrPast {
				return err
			}
			s.line(c.Now(), "拒绝更早的时刻")
		}
		s.line(c.Now(), "到达 "+name)
		if s.busy {
			s.wait = append(s.wait, name)
			s.line(c.Now(), fmt.Sprintf("排队 %s  等待 %d", name, len(s.wait)))
			return nil
		}
		return s.begin(c, name)
	}
}

func (s *shop) begin(c engine.Context, name string) error {
	s.busy = true
	s.line(c.Now(), "开始 "+name)
	_, err := c.AddFuture(service, engine.Event{
		Name:    "done-" + name,
		Handler: s.finish(name),
	})
	return err
}

func (s *shop) finish(name string) engine.Handler {
	return func(c engine.Context) error {
		s.line(c.Now(), "完成 "+name)
		if len(s.wait) == 0 {
			s.busy = false
			s.line(c.Now(), "空闲")
			return nil
		}
		next := s.wait[0]
		s.wait = s.wait[1:]
		s.line(c.Now(), "同一毫秒接上 "+next)
		return s.begin(c, next)
	}
}
