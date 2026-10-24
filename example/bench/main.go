package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

const (
	warmup      = 5 * time.Second
	inFlight    = 8
	horizonMS   = 10_000
	workLight   = 32
	workMedium  = 256
	workHeavy   = 2048
	reportStart = "<!-- bench:start -->"
	reportEnd   = "<!-- bench:end -->"
)

type caseSpec struct {
	group   string
	label   string
	sources int
	work    int
}

type result struct {
	Group    string  `json:"group"`
	Label    string  `json:"label"`
	Sources  int     `json:"sources"`
	Work     int     `json:"work"`
	Events   uint64  `json:"events"`
	PerSec   float64 `json:"per_sec"`
	CPUCores float64 `json:"cpu_cores"`
	HeapMiB  float64 `json:"heap_mib"`
	RSSMiB   float64 `json:"rss_mib"`
}

func main() {
	clock := flag.Bool("clock", false, "ramp packing lines until realtime drift reaches 10 percent")
	robust := flag.Bool("robust", false, "measure recovery after a few handlers that run far longer than 1ms")
	duration := flag.Duration("duration", 10*time.Minute, "measurement window for each throughput scenario")
	flag.Parse()
	if *duration <= 0 {
		fmt.Fprintln(os.Stderr, "duration must be positive")
		os.Exit(1)
	}
	if *clock && *robust {
		fmt.Fprintln(os.Stderr, "choose one of -clock or -robust")
		os.Exit(1)
	}
	if *robust {
		if err := runRobust(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *clock {
		if err := runClock(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	groups := [][]caseSpec{
		{
			{group: "A", label: "1", sources: 1, work: workMedium},
			{group: "A", label: "10", sources: 10, work: workMedium},
			{group: "A", label: "100", sources: 100, work: workMedium},
			{group: "A", label: "1000", sources: 1000, work: workMedium},
			{group: "A", label: "4000", sources: 4000, work: workMedium},
		},
		{
			{group: "B", label: "轻", sources: 1000, work: workLight},
			{group: "B", label: "中", sources: 1000, work: workMedium},
			{group: "B", label: "重", sources: 1000, work: workHeavy},
		},
	}
	var rows []result
	for _, specs := range groups {
		window := *duration / time.Duration(len(specs))
		if window <= 0 {
			fmt.Fprintln(os.Stderr, "duration is too short for the cases in a scenario")
			os.Exit(1)
		}
		for _, spec := range specs {
			fmt.Fprintf(os.Stderr, "run group=%s label=%s sources=%d work=%d window=%s\n", spec.group, spec.label, spec.sources, spec.work, window)
			row, err := runCase(spec, window, warmup)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  events/s=%.0f cpu=%.2f heap=%.1fMiB rss=%.1fMiB\n", row.PerSec, row.CPUCores, row.HeapMiB, row.RSSMiB)
			rows = append(rows, row)
		}
	}
	if err := writeReport(rows, *duration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runClock() error {
	eng, err := engine.NewEngine(
		engine.WithRealtime(true),
		engine.WithExitWhenIdle(false),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		return err
	}
	var sink atomic.Uint64
	catalog := parcelCatalog()
	stock := parcelStock(catalog)
	if err := eng.Start(context.Background()); err != nil {
		return err
	}
	lines := 0
	addLines := func(n int) error {
		for i := 0; i < n; i++ {
			if err := addLine(eng, catalog, stock, time.Duration(i%8)*time.Millisecond, &sink); err != nil {
				return err
			}
			lines++
		}
		return nil
	}
	if err := addLines(clockLineStart); err != nil {
		return err
	}
	started := time.Now()
	var points []clockPoint
	collect := func() clockPoint {
		p := sampleClock(started, time.Duration(eng.Now())*time.Millisecond)
		p.Sources = lines
		points = append(points, p)
		return p
	}
	pulse := time.NewTicker(200 * time.Millisecond)
	defer pulse.Stop()
	next := time.NewTimer(clockLineEvery)
	defer next.Stop()
	cap := time.NewTimer(clockRampMax)
	defer cap.Stop()
	fmt.Fprintf(os.Stderr, "clock ramp from %d lines, +%d every %s, stop at %.0f%% or %s\n", lines, clockLineBatch, clockLineEvery, clockBlowup*100, clockRampMax)
wait:
	for {
		select {
		case <-pulse.C:
			p := collect()
			if p.Ratio >= clockBlowup {
				break wait
			}
		case <-next.C:
			if err := addLines(clockLineBatch); err != nil {
				return err
			}
			p := sampleClock(started, time.Duration(eng.Now())*time.Millisecond)
			fmt.Fprintf(os.Stderr, "  lines=%d ratio=%.3f%%\n", lines, p.Ratio*100)
			next.Reset(clockLineEvery)
		case <-cap.C:
			collect()
			break wait
		}
	}
	if err := eng.Stop(); err != nil && !errors.Is(err, engine.ErrClosed) {
		return err
	}
	if sink.Load() == 0 {
		return fmt.Errorf("packing lines did not run")
	}
	last := points[len(points)-1]
	fmt.Fprintf(os.Stderr, "  线条 %d  运行 %s  仿真 %s  偏差 %s  比值 %.4f%%\n", last.Sources, clockDur(last.Wall), clockDur(last.Sim), clockDur(last.Drift), last.Ratio*100)
	return writeClockReport([]clockSeries{{Label: "偏差比值", Sources: last.Sources, Points: points}}, last.Wall)
}

func runCase(spec caseSpec, measure, warm time.Duration) (result, error) {
	runtime.GC()
	eng, err := engine.NewEngine(
		engine.WithRealtime(false),
		engine.WithExitWhenIdle(false),
		engine.WithSeed(1),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		return result{}, err
	}
	defer eng.Stop()

	var counting atomic.Bool
	var completed atomic.Uint64
	var sink atomic.Uint64
	srcCtx, srcCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(spec.sources)
	for id := 0; id < spec.sources; id++ {
		go func(id int) {
			defer wg.Done()
			runSource(srcCtx, eng, id, spec.work, &counting, &completed, &sink)
		}(id)
	}

	time.Sleep(warm)
	completed.Store(0)
	counting.Store(true)
	cpu0, _, err := resourceNow()
	if err != nil {
		srcCancel()
		wg.Wait()
		return result{}, err
	}
	peak := sampleHeap(measure)
	cpu1, rss, err := resourceNow()
	counting.Store(false)
	if err != nil {
		srcCancel()
		wg.Wait()
		return result{}, err
	}
	events := completed.Load()
	srcCancel()
	wg.Wait()

	cores := 0.0
	if measure > 0 {
		cores = float64(cpu1-cpu0) / float64(measure)
	}
	perSec := 0.0
	if measure > 0 {
		perSec = float64(events) / measure.Seconds()
	}
	return result{
		Group:    spec.group,
		Label:    spec.label,
		Sources:  spec.sources,
		Work:     spec.work,
		Events:   events,
		PerSec:   perSec,
		CPUCores: cores,
		HeapMiB:  mib(peak),
		RSSMiB:   mib(rss),
	}, nil
}

func runSource(ctx context.Context, eng *engine.Engine, id, iters int, counting *atomic.Bool, completed, sink *atomic.Uint64) {
	rng := rand.New(rand.NewPCG(uint64(id+1), 1))
	name := fmt.Sprintf("s%d", id)
	slots := make(chan struct{}, inFlight)
	for {
		select {
		case <-ctx.Done():
			return
		case slots <- struct{}{}:
		}
		delay := time.Duration(rng.Int64N(horizonMS+1)) * time.Millisecond
		salt := rng.Uint64()
		_, err := eng.AddFuture(delay, engine.Event{
			Name: name,
			Handler: func(engine.Context) error {
				sink.Add(work(iters, salt))
				if counting.Load() {
					completed.Add(1)
				}
				<-slots
				return nil
			},
		})
		if err != nil {
			<-slots
			return
		}
	}
}

func sampleHeap(window time.Duration) uint64 {
	stop := make(chan struct{})
	var peak atomic.Uint64
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var stats runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				runtime.ReadMemStats(&stats)
				for {
					old := peak.Load()
					if stats.HeapInuse <= old || peak.CompareAndSwap(old, stats.HeapInuse) {
						break
					}
				}
			}
		}
	}()
	time.Sleep(window)
	close(stop)
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if stats.HeapInuse > peak.Load() {
		return stats.HeapInuse
	}
	return peak.Load()
}
