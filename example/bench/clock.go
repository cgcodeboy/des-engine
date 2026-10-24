package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	clockLimit     = 0.01
	clockBlowup    = 0.10
	clockRampMax   = 5 * time.Minute
	clockLineStart = 10000
	clockLineBatch = 1000
	clockLineEvery = time.Second
	clockChartW    = 960
	clockChartH    = 360
	clockMarkStart = "<!-- clock:start -->"
	clockMarkEnd   = "<!-- clock:end -->"
)

type clockPoint struct {
	Wall    time.Duration
	Sim     time.Duration
	Drift   time.Duration
	Ratio   float64
	Sources int
}

func sampleClock(start time.Time, sim time.Duration) clockPoint {
	wall := time.Since(start)
	drift := wall - sim
	ratio := 0.0
	if wall > 0 {
		ratio = math.Abs(float64(drift)) / float64(wall)
	}
	return clockPoint{Wall: wall, Sim: sim, Drift: drift, Ratio: ratio}
}

type clockSeries struct {
	Sources int
	Label   string
	Points  []clockPoint
}

func drawClockChart(series []clockSeries) string {
	const (
		margin = 16
		plotL  = 72
		plotT  = 64
		plotR  = 24
		plotB  = 48
	)
	width := clockChartW
	height := clockChartH
	plotW := float64(width - plotL - plotR)
	plotH := float64(height - plotT - plotB)
	maxPct := 1.2
	var maxWall time.Duration
	for _, s := range series {
		for _, p := range s.Points {
			if v := p.Ratio * 100 * 1.15; v > maxPct {
				maxPct = v
			}
			if p.Wall > maxWall {
				maxWall = p.Wall
			}
		}
	}
	if maxWall <= 0 {
		maxWall = time.Second
	}
	font := "PingFang SC, -apple-system, Segoe UI, Helvetica, Arial, sans-serif"
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", width, height, width, height)
	b.WriteString("<rect width=\"100%\" height=\"100%\" fill=\"#ffffff\"/>\n")
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"8\" fill=\"#ffffff\" stroke=\"#d0d7de\"/>\n", margin, margin, width-2*margin, height-2*margin)
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"15\" font-weight=\"600\" fill=\"#1f2328\">偏差占运行时间的比值</text>\n", margin+20, margin+28, font)
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">横线是 1%%。高于横线记为不准</text>\n", margin+20, margin+48, font)

	base := float64(plotT + int(plotH))
	for i := 0; i <= 4; i++ {
		gy := float64(plotT) + plotH*float64(4-i)/4
		fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#eaeef2\"/>\n", plotL, gy, float64(plotL)+plotW, gy)
		label := fmt.Sprintf("%.2f%%", maxPct*float64(i)/4)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%.1f\" text-anchor=\"end\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">%s</text>\n", plotL-8, gy+4, font, label)
	}
	fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#d0d7de\"/>\n", plotL, base, float64(plotL)+plotW, base)

	limitY := base - plotH*(1/maxPct)
	fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cf222e\" stroke-dasharray=\"4 4\"/>\n", plotL, limitY, float64(plotL)+plotW, limitY)
	fmt.Fprintf(&b, "<text x=\"%.1f\" y=\"%.1f\" font-family=\"%s\" font-size=\"12\" fill=\"#cf222e\">1%%</text>\n", float64(plotL)+plotW-28, limitY-6, font)

	colors := []string{"#0969da", "#1a7f37", "#9a6700", "#8250df", "#cf222e"}
	if len(series) > 0 {
		for si, s := range series {
			if len(s.Points) == 0 {
				continue
			}
			color := colors[si%len(colors)]
			var line strings.Builder
			for i, p := range s.Points {
				x := float64(plotL) + plotW*float64(p.Wall)/float64(maxWall)
				y := base - plotH*(p.Ratio*100)/maxPct
				if y < float64(plotT) {
					y = float64(plotT)
				}
				if y > base {
					y = base
				}
				if i > 0 {
					line.WriteByte(' ')
				}
				fmt.Fprintf(&line, "%.1f,%.1f", x, y)
			}
			fmt.Fprintf(&b, "<polyline fill=\"none\" stroke=\"%s\" stroke-width=\"2\" points=\"%s\"/>\n", color, line.String())
			lx := plotL + si*150
			fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"%s\" stroke-width=\"2\"/>\n", lx, height-margin-6, lx+18, height-margin-6, color)
			label := s.Label
			if label == "" {
				label = fmt.Sprintf("%d 来源", s.Sources)
			}
			fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"12\" fill=\"#1f2328\">%s</text>\n", lx+24, height-margin-2, font, xml(label))
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">0</text>\n", plotL, height-margin-28, font)
		fmt.Fprintf(&b, "<text x=\"%.1f\" y=\"%d\" text-anchor=\"end\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">%s</text>\n", float64(plotL)+plotW, height-margin-28, font, prettyDuration(maxWall))
	}
	b.WriteString("</svg>\n")
	return b.String()
}

func writeClockReport(series []clockSeries, window time.Duration) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	svg := drawClockChart(series)
	dir := filepath.Join(root, "example", "bench")
	if err := os.WriteFile(filepath.Join(dir, "clock.svg"), []byte(svg), 0o644); err != nil {
		return err
	}
	pngErr := renderPNG(svg, filepath.Join(dir, "clock.png"), clockChartW, clockChartH)
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	next := replaceClock(string(readme), clockSection(series, window))
	if err := os.WriteFile(readmePath, []byte(next), 0o644); err != nil {
		return err
	}
	return pngErr
}

func replaceClock(readme, section string) string {
	start := strings.Index(readme, clockMarkStart)
	end := strings.Index(readme, clockMarkEnd)
	block := clockMarkStart + "\n" + section + "\n" + clockMarkEnd + "\n"
	if start >= 0 && end > start {
		return readme[:start] + block + readme[end+len(clockMarkEnd):]
	}
	return readme + "\n" + block
}

func clockSection(series []clockSeries, window time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "真实模式。从 %d 条打包线开始，每秒增加 %d 条，最多测 %s。每条线到货时查商品、扣库存，再记下出货。小包和文件扫描、打包各等 2ms，大包和易碎扫描等 4ms、打包等 3ms，下一单再等 5ms。等待是仿真延迟，处理函数不做空转。比值超过 1%% 记为不准，达到 10%% 停止。测量到 %s。\n\n", clockLineStart, clockLineBatch, prettyDuration(clockRampMax), clockDur(window))
	b.WriteString("![偏差占运行时间的比值](example/bench/clock.png)\n\n")
	b.WriteString("| 线条 | 运行 | 仿真 | 偏差 | 比值 | 结论 |\n")
	b.WriteString("| ---: | --- | --- | --- | ---: | --- |\n")
	if cross, ok := clockCrossing(series, clockLimit); ok {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %.4f%% | 越过 1%% |\n", cross.Sources, clockDur(cross.Wall), clockDur(cross.Sim), clockDur(cross.Drift), cross.Ratio*100)
	}
	for _, s := range series {
		last := clockPoint{}
		if len(s.Points) > 0 {
			last = s.Points[len(s.Points)-1]
		}
		verdict := "准"
		if last.Ratio >= clockBlowup {
			verdict = "离谱"
		} else if last.Ratio > clockLimit {
			verdict = "不准"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %.4f%% | %s |\n", last.Sources, clockDur(last.Wall), clockDur(last.Sim), clockDur(last.Drift), last.Ratio*100, verdict)
	}
	b.WriteString("\n```bash\ngo run ./example/bench -clock\n```\n")
	return b.String()
}

func clockDur(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	d = d.Round(time.Microsecond)
	if d >= time.Minute && d%time.Minute < time.Millisecond {
		return sign + prettyDuration(d.Truncate(time.Minute))
	}
	if d >= time.Second {
		return sign + d.Round(time.Millisecond).String()
	}
	return sign + d.String()
}

func clockCrossing(series []clockSeries, limit float64) (clockPoint, bool) {
	for _, s := range series {
		for _, p := range s.Points {
			if p.Ratio > limit {
				return p, true
			}
		}
	}
	return clockPoint{}, false
}
