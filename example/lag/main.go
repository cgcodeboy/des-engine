// Command lag puts the realtime origin in the past.
// Buckets whose theoretical time has already passed run immediately and report lag.
// A later bucket still waits until its wall-clock target.
//
//	go run ./example/lag
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
	const behind = 40 * time.Millisecond
	origin := time.Now().Add(-behind)
	var started time.Time
	var lines []string
	note := func(text string) {
		lines = append(lines, fmt.Sprintf("墙钟 %3d ms  %s", time.Since(started).Milliseconds(), text))
	}
	eng, err := engine.New(
		engine.WithOrigin(origin),
		engine.WithExitWhenIdle(true),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
		engine.WithLagHandler(func(lag engine.Lag) {
			note(fmt.Sprintf("追赶  第 %d ms  滞后 %s", lag.At, lag.Lag.Round(time.Millisecond)))
		}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, at := range []engine.Time{0, 15, 80} {
		_, err := eng.AddEvent(at, engine.Event{
			Name: fmt.Sprintf("step-%d", at),
			Handler: func(c engine.Context) error {
				note(fmt.Sprintf("%4d ms  执行", c.Now()))
				return nil
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	started = time.Now()
	if err := eng.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := eng.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	elapsed := time.Since(started).Round(time.Millisecond)

	fmt.Printf("墙钟起点设在 %s 之前。0 ms 和 15 ms 已经错过，80 ms 会等到目标时刻。\n", behind)
	for _, line := range lines {
		fmt.Println(line)
	}
	fmt.Printf("进程墙钟 %s，仿真时间 %d ms。错过的毫秒没有逐个补睡。\n", elapsed, eng.Now())
}
