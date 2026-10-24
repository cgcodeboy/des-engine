package main

import (
	"strings"
	"testing"
)

func TestLineChartHasTrend(t *testing.T) {
	svg := drawChart([]result{
		{Group: "A", Label: "1", PerSec: 100},
		{Group: "A", Label: "10", PerSec: 80},
		{Group: "B", Label: "轻", PerSec: 120},
		{Group: "B", Label: "重", PerSec: 40},
	})
	if !strings.Contains(svg, "<polyline ") {
		t.Fatal("chart has no line")
	}
	if !strings.Contains(svg, "<linearGradient ") {
		t.Fatal("chart has no area fill")
	}
}
