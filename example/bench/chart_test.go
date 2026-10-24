package main

import (
	"strings"
	"testing"
	"time"
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

func TestRobustShapeNeedsAPeakThatFalls(t *testing.T) {
	fell := []clockPoint{
		{Wall: 3 * time.Second, Ratio: 0.001},
		{Wall: 4 * time.Second, Ratio: 0.08},
		{Wall: 4*time.Second + 50*time.Millisecond, Ratio: 0.08},
		{Wall: 4*time.Second + 100*time.Millisecond, Ratio: 0.08},
		{Wall: 5 * time.Second, Ratio: 0.002},
		{Wall: 6 * time.Second, Ratio: 0.09},
		{Wall: 6*time.Second + 50*time.Millisecond, Ratio: 0.09},
		{Wall: 6*time.Second + 100*time.Millisecond, Ratio: 0.09},
		{Wall: 7 * time.Second, Ratio: 0.001},
	}
	peaks, back := robustShape(fell)
	if peaks != 2 || !back {
		t.Fatalf("peaks=%d back=%v", peaks, back)
	}
	growing := []clockPoint{
		{Wall: 3 * time.Second, Ratio: 0.001},
		{Wall: 4 * time.Second, Ratio: 0.02},
		{Wall: 4*time.Second + 50*time.Millisecond, Ratio: 0.04},
		{Wall: 4*time.Second + 100*time.Millisecond, Ratio: 0.05},
	}
	peaks, back = robustShape(growing)
	if peaks != 1 || back {
		t.Fatalf("growing peaks=%d back=%v", peaks, back)
	}
}

func TestClockChartMarksOnePercent(t *testing.T) {
	svg := drawClockChart([]clockSeries{{
		Sources: 1,
		Points:  []clockPoint{{Wall: 60 * time.Second, Ratio: 0.001}},
	}})
	if !strings.Contains(svg, "1%") || !strings.Contains(svg, "<polyline ") {
		t.Fatal("clock chart missing limit or line")
	}
}
