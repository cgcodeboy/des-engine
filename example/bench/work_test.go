package main

import (
	"testing"
	"time"
)

func TestWorkDependsOnInput(t *testing.T) {
	if work(workLight, 1) == work(workLight, 2) {
		t.Fatal("salt did not change the result")
	}
	if work(workLight, 1) == work(workMedium, 1) {
		t.Fatal("iteration count did not change the result")
	}
}

func TestShortCaseCompletesEvents(t *testing.T) {
	res, err := runCase(caseSpec{group: "A", label: "2", sources: 2, work: workLight}, 200*time.Millisecond, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events == 0 || res.PerSec <= 0 {
		t.Fatalf("result = %+v", res)
	}
}
