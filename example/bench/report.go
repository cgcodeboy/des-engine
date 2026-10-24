package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func writeReport(rows []result, window time.Duration) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	svg := drawChart(rows)
	chartDir := filepath.Join(root, "example", "bench")
	if err := os.WriteFile(filepath.Join(chartDir, "result.svg"), []byte(svg), 0o644); err != nil {
		return err
	}
	if err := writeChartPNG(svg, filepath.Join(chartDir, "result.png")); err != nil {
		return err
	}
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	next := replaceSection(string(readme), reportSection(rows, window))
	return os.WriteFile(readmePath, []byte(next), 0o644)
}

func writeChartPNG(svg, pngPath string) error {
	chrome, err := chromeBin()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "des-chart")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	htmlPath := filepath.Join(dir, "chart.html")
	page := "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><style>html,body{margin:0;padding:0;background:#fff;overflow:hidden}</style></head><body>\n" + svg + "</body></html>\n"
	if err := os.WriteFile(htmlPath, []byte(page), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(chrome,
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--force-device-scale-factor=2",
		"--screenshot="+pngPath,
		fmt.Sprintf("--window-size=%d,%d", chartWidth, chartHeight()),
		"file://"+htmlPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("render chart: %w: %s", err, out)
	}
	return nil
}

func chromeBin() (string, error) {
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("chrome not found")
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func replaceSection(readme, section string) string {
	start := strings.Index(readme, reportStart)
	end := strings.Index(readme, reportEnd)
	block := reportStart + "\n" + section + "\n" + reportEnd + "\n"
	if start >= 0 && end > start {
		return readme[:start] + block + readme[end+len(reportEnd):]
	}
	if !strings.HasSuffix(readme, "\n") {
		readme += "\n"
	}
	return readme + "\n" + block
}

func reportSection(rows []result, window time.Duration) string {
	var b strings.Builder
	groupA := filterGroup(rows, "A")
	groupB := filterGroup(rows, "B")
	b.WriteString("## 基准测试\n\n")
	b.WriteString("下面的数字来自一次完整运行。真实模式是关闭的，所以测到的是事件循环的处理能力，而不是睡眠。\n\n")
	fmt.Fprintf(&b, "每种场景测量 %s，场景内各档平分。场景 A 每档 %s，场景 B 每档 %s。每档开始前另有 %s 预热，预热不计入结果。事件源在整个窗口内持续插入，触发时刻在 0 到 10 秒仿真时间之间均匀随机。每个来源最多同时有 %d 个未完成事件。复杂度是处理函数中的整数运算次数。\n\n", prettyDuration(window), prettyDuration(caseWindow(window, len(groupA))), prettyDuration(caseWindow(window, len(groupB))), prettyDuration(warmup), inFlight)
	fmt.Fprintf(&b, "运行环境是 %s/%s，%d 个逻辑 CPU。CPU 列是测量窗口内进程用户态加内核态时间除以墙钟，单位是核。堆峰值是窗口内 Go `HeapInuse` 的最大值。MaxRSS 是进程启动以来的驻留高水位，后面的档不会低于前面的档。\n\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	b.WriteString("![每秒完成事件数](example/bench/result.png)\n\n")
	fmt.Fprintf(&b, "### 场景 A：中等复杂度（%d 次运算）\n\n", workMedium)
	writeTable(&b, groupA, "来源数")
	b.WriteString("\n### 场景 B：1000 个来源\n\n")
	writeTable(&b, groupB, "复杂度")
	b.WriteString("\n事件循环只有一个协程。来源变多不会让处理函数并行，多出来的开销主要在插入时的锁上。复杂度升高后，时间花在处理函数里，吞吐下降。\n\n")
	fmt.Fprintf(&b, "`-duration` 是每种场景的总测量时间。不传则是 %s。\n\n", prettyDuration(window))
	b.WriteString("```bash\ngo run ./example/bench\ngo run ./example/bench -duration 30s\n```\n")
	return b.String()
}

func prettyDuration(d time.Duration) string {
	if d <= 0 {
		return "0 秒"
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d 分钟", int(d/time.Minute))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%d 分 %d 秒", int(d/time.Minute), int((d%time.Minute)/time.Second))
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%d 秒", int(d/time.Second))
	}
	return d.String()
}

func caseWindow(total time.Duration, n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return total / time.Duration(n)
}

func writeTable(b *strings.Builder, rows []result, head string) {
	fmt.Fprintf(b, "| %s | 完成事件 | 每秒事件 | CPU 核 | 堆峰值 MiB | MaxRSS MiB |\n", head)
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %d | %.0f | %.2f | %.1f | %.1f |\n", row.Label, row.Events, row.PerSec, row.CPUCores, row.HeapMiB, row.RSSMiB)
	}
}
