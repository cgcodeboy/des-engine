package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
)

func resourceNow() (cpu time.Duration, rss uint64, err error) {
	var usage syscall.Rusage
	if err = syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0, err
	}
	return timeval(usage.Utime) + timeval(usage.Stime), rssBytes(usage.Maxrss), nil
}

func timeval(tv syscall.Timeval) time.Duration {
	return time.Duration(tv.Sec)*time.Second + time.Duration(tv.Usec)*time.Microsecond
}

func rssBytes(maxrss int64) uint64 {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return uint64(maxrss)
	}
	return uint64(maxrss) * 1024
}

func mib(bytes uint64) float64 {
	return float64(bytes) / (1024 * 1024)
}

func formatRate(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.2f 百万", v/1_000_000)
	case v >= 10_000:
		return fmt.Sprintf("%.1f 万", v/10_000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
