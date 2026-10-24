// Command checkpoint pauses a running engine, resumes it without a checkpoint,
// then copies the paused queue into bytes and loads those bytes into a second engine.
//
//	go run ./example/checkpoint
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cgcodeboy/des-engine/engine"
)

func main() {
	out := make(chan string, 4)
	eng, err := newEngine()
	if err != nil {
		exit(err)
	}
	if err := eng.Handle("job", report(out)); err != nil {
		exit(err)
	}
	if err := addJob(eng, 0, "alpha"); err != nil {
		exit(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		exit(err)
	}

	fmt.Println(<-out)
	if err := eng.Pause(); err != nil {
		exit(err)
	}
	fmt.Printf("暂停在 %d ms。这一步不取检查点\n", eng.Now())
	if err := addJob(eng, 10, "beta"); err != nil {
		exit(err)
	}
	if err := eng.Resume(); err != nil {
		exit(err)
	}
	fmt.Println(<-out)

	if err := eng.Pause(); err != nil {
		exit(err)
	}
	if err := addJob(eng, 20, "gamma"); err != nil {
		exit(err)
	}
	data, err := eng.Checkpoint()
	if err != nil {
		exit(err)
	}
	fmt.Printf("再次暂停。检查点 %d 字节，引擎不保存这份字节\n", len(data))
	if err := eng.Stop(); err != nil {
		exit(err)
	}

	next, err := newEngine()
	if err != nil {
		exit(err)
	}
	if err := next.Handle("job", report(out)); err != nil {
		exit(err)
	}
	if err := next.Restore(context.Background(), data); err != nil {
		exit(err)
	}
	snap := next.Snapshot()
	fmt.Printf("另一台引擎恢复后  状态 %s  仿真时间 %d ms  待执行 %d\n", snap.State, snap.Now, snap.EventsPending)
	if err := next.Resume(); err != nil {
		exit(err)
	}
	fmt.Println(<-out)
	if err := next.Stop(); err != nil {
		exit(err)
	}
}

func newEngine() (*engine.Engine, error) {
	return engine.New(
		engine.WithRealtime(false),
		engine.WithExitWhenIdle(false),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
}

func addJob(eng *engine.Engine, at engine.Time, name string) error {
	_, err := eng.AddEvent(at, engine.Event{Name: "job", Payload: []byte(name)})
	return err
}

func report(out chan<- string) engine.Handler {
	return func(c engine.Context) error {
		out <- fmt.Sprintf("%4d ms  作业 %s", c.Now(), c.Payload())
		return nil
	}
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
