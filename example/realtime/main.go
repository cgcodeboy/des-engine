// Command realtime runs one engine in realtime mode. It schedules a same-step
// follow-up, one future event, and one handler that sleeps past the slow-event
// threshold. After interrupt it prints one Snapshot.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	seen := make(chan string, 8)
	go func() {
		for line := range seen {
			log.Print(line)
		}
	}()
	eng, err := engine.NewEngine(
		engine.WithSeed(1),
		engine.WithLagHandler(func(lag engine.Lag) {
			seen <- fmt.Sprintf("lag step=%d lag=%s", lag.Step, lag.Lag)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := eng.Start(ctx); err != nil {
		log.Fatal(err)
	}

	report := func(name string) engine.Handler {
		return func(c engine.Context) error {
			seen <- fmt.Sprintf("%d ms  %s", c.Now(), name)
			return nil
		}
	}

	if _, err := eng.AddEvent(0, engine.Event{
		Name: "tick",
		Handler: func(c engine.Context) error {
			seen <- fmt.Sprintf("%d ms  tick", c.Now())
			_, err := c.AddFuture(0, engine.Event{Name: "follow", Handler: report("follow")})
			return err
		},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := eng.AddFuture(20*time.Millisecond, engine.Event{
		Name:    "later",
		Handler: report("later"),
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := eng.AddFuture(30*time.Millisecond, engine.Event{
		Name: "slow",
		Handler: func(c engine.Context) error {
			seen <- fmt.Sprintf("%d ms  slow", c.Now())
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
	snap := eng.Snapshot()
	log.Printf("state=%s now=%dms slow=%d", snap.State, snap.Now, snap.SlowTotal)
}
