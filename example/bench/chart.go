package main

import (
	"fmt"
	"math"
	"strings"
)

const (
	chartWidth  = 960
	chartCardH  = 300
	chartGap    = 16
	chartMargin = 16
)

func chartHeight() int {
	return chartMargin + chartCardH + chartGap + chartCardH + chartMargin
}

func drawChart(rows []result) string {
	width := chartWidth
	height := chartHeight()
	maxV := niceCeil(maxPerSec(rows))
	var bld strings.Builder
	fmt.Fprintf(&bld, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&bld, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", width, height, width, height)
	bld.WriteString("<rect width=\"100%\" height=\"100%\" fill=\"#ffffff\"/>\n")
	bld.WriteString("<defs>\n")
	writeGradient(&bld, "fillA", "#0969da")
	writeGradient(&bld, "fillB", "#1a7f37")
	bld.WriteString("</defs>\n")
	writeLinePanel(&bld, chartMargin, chartMargin, width-2*chartMargin, chartCardH, "来源数（中等复杂度）", filterGroup(rows, "A"), maxV, "#0969da", "fillA")
	writeLinePanel(&bld, chartMargin, chartMargin+chartCardH+chartGap, width-2*chartMargin, chartCardH, "复杂度（1000 个来源）", filterGroup(rows, "B"), maxV, "#1a7f37", "fillB")
	fmt.Fprintf(&bld, "</svg>\n")
	return bld.String()
}

func filterGroup(rows []result, group string) []result {
	out := make([]result, 0, len(rows))
	for _, row := range rows {
		if row.Group == group {
			out = append(out, row)
		}
	}
	return out
}

func writeGradient(bld *strings.Builder, id, color string) {
	fmt.Fprintf(bld, "<linearGradient id=\"%s\" x1=\"0\" y1=\"0\" x2=\"0\" y2=\"1\">\n", id)
	fmt.Fprintf(bld, "<stop offset=\"0%%\" stop-color=\"%s\" stop-opacity=\"0.28\"/>\n", color)
	fmt.Fprintf(bld, "<stop offset=\"100%%\" stop-color=\"%s\" stop-opacity=\"0.02\"/>\n", color)
	bld.WriteString("</linearGradient>\n")
}

func writeLinePanel(bld *strings.Builder, x, y, w, h int, title string, rows []result, maxV float64, color, fillID string) {
	font := "PingFang SC, -apple-system, Segoe UI, Helvetica, Arial, sans-serif"
	fmt.Fprintf(bld, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"8\" fill=\"#ffffff\" stroke=\"#d0d7de\"/>\n", x, y, w, h)
	fmt.Fprintf(bld, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"15\" font-weight=\"600\" fill=\"#1f2328\">%s</text>\n", x+20, y+28, font, xml(title))
	fmt.Fprintf(bld, "<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">每秒完成事件数</text>\n", x+20, y+48, font)

	plotX := float64(x + 78)
	plotY := float64(y + 72)
	plotW := float64(w - 118)
	plotH := float64(h - 120)
	base := plotY + plotH
	ticks := 4
	for i := 0; i <= ticks; i++ {
		gy := plotY + plotH*float64(ticks-i)/float64(ticks)
		fmt.Fprintf(bld, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#eaeef2\"/>\n", plotX, gy, plotX+plotW, gy)
		label := axisRate(maxV * float64(i) / float64(ticks))
		fmt.Fprintf(bld, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">%s</text>\n", plotX-10, gy+4, font, xml(label))
	}
	fmt.Fprintf(bld, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#d0d7de\"/>\n", plotX, base, plotX+plotW, base)
	if len(rows) == 0 || maxV <= 0 {
		return
	}

	const inset = 36.0
	xs := make([]float64, len(rows))
	ys := make([]float64, len(rows))
	for i, row := range rows {
		xs[i] = pointX(plotX+inset, plotW-2*inset, i, len(rows))
		ys[i] = plotY + plotH*(1-row.PerSec/maxV)
	}
	fmt.Fprintf(bld, "<path d=\"%s\" fill=\"url(#%s)\"/>\n", areaPath(xs, ys, base), fillID)
	fmt.Fprintf(bld, "<polyline points=\"%s\" fill=\"none\" stroke=\"%s\" stroke-width=\"2.5\" stroke-linejoin=\"round\" stroke-linecap=\"round\"/>\n", linePoints(xs, ys), color)
	for i, row := range rows {
		fmt.Fprintf(bld, "<circle cx=\"%.1f\" cy=\"%.1f\" r=\"4.5\" fill=\"#ffffff\" stroke=\"%s\" stroke-width=\"2.5\"/>\n", xs[i], ys[i], color)
		anchor := "middle"
		labelX := xs[i]
		switch {
		case len(rows) > 1 && i == 0:
			anchor = "start"
			labelX = xs[i] + 10
		case len(rows) > 1 && i == len(rows)-1:
			anchor = "end"
			labelX = xs[i] - 10
		}
		labelY := ys[i] - 12
		if labelY < plotY+14 {
			labelY = ys[i] + 18
		}
		fmt.Fprintf(bld, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"%s\" font-family=\"%s\" font-size=\"12\" fill=\"#1f2328\">%s</text>\n", labelX, labelY, anchor, font, xml(pointRate(row.PerSec)))
		fmt.Fprintf(bld, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-family=\"%s\" font-size=\"12\" fill=\"#656d76\">%s</text>\n", xs[i], base+22, font, xml(row.Label))
	}
}

func pointX(plotX, plotW float64, i, n int) float64 {
	if n <= 1 {
		return plotX + plotW/2
	}
	return plotX + plotW*float64(i)/float64(n-1)
}

func linePoints(xs, ys []float64) string {
	var b strings.Builder
	for i := range xs {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", xs[i], ys[i])
	}
	return b.String()
}

func areaPath(xs, ys []float64, base float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "M%.1f %.1f", xs[0], base)
	for i := range xs {
		fmt.Fprintf(&b, "L%.1f %.1f", xs[i], ys[i])
	}
	fmt.Fprintf(&b, "L%.1f %.1fZ", xs[len(xs)-1], base)
	return b.String()
}

func maxPerSec(rows []result) float64 {
	maxV := 0.0
	for _, row := range rows {
		if row.PerSec > maxV {
			maxV = row.PerSec
		}
	}
	return maxV
}

func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	frac := v / exp
	switch {
	case frac <= 1:
		return exp
	case frac <= 2:
		return 2 * exp
	case frac <= 5:
		return 5 * exp
	default:
		return 10 * exp
	}
}

func axisRate(v float64) string {
	if v < 10_000 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.0f万", v/10_000)
}

func pointRate(v float64) string {
	if v < 10_000 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f万", v/10_000)
}

func xml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
