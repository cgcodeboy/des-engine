// Command realtime runs one engine in realtime mode. It schedules a same-step
// follow-up, one future event, and one handler that sleeps past the slow-event
// threshold, then serves the metrics endpoints until interrupted.
//
//	GET /healthz
//	GET /status
//	GET /metrics
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
	"github.com/cgcodeboy/des-engine/metrics"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	eng, err := engine.New(
		engine.WithSeed(1),
		engine.WithLagHandler(func(lag engine.Lag) {
			log.Printf("lag step=%d lag=%s", lag.Step, lag.Lag)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := eng.Start(ctx); err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := metrics.Listen(ctx, ":9090", eng, nil); err != nil {
			log.Print(err)
		}
	}()

	if _, err := eng.AddEvent(0, engine.Event{
		Name: "tick",
		Handler: func(c engine.Context) error {
			_, err := c.AddFuture(0, engine.Event{Name: "follow", Handler: func(engine.Context) error { return nil }})
			return err
		},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := eng.AddFuture(20*time.Millisecond, engine.Event{
		Name:    "later",
		Handler: func(engine.Context) error { return nil },
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := eng.AddFuture(30*time.Millisecond, engine.Event{
		Name: "slow",
		Handler: func(engine.Context) error {
			time.Sleep(2 * time.Millisecond)
			return nil
		},
	}); err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
	if err := eng.Stop(); err != nil {
		log.Fatal(err)
	}
}
