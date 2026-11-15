package main

import (
	"testing"
	"time"
)

func TestArrowRepeatBurst(t *testing.T) {
	var gate arrowRepeatGate
	now := time.Now()
	if !gate.accept(1, false, now) {
		t.Fatal("physical press discarded")
	}
	for i := 0; i < 70; i++ {
		if gate.accept(1, true, now) {
			t.Fatal("catch-up repeat delivered")
		}
	}
	if !gate.accept(1, true, now.Add(25*time.Millisecond)) {
		t.Fatal("normal repeat discarded")
	}
	if !gate.accept(1, false, now.Add(26*time.Millisecond)) {
		t.Fatal("rapid physical press discarded")
	}
	if !gate.accept(2, true, now.Add(27*time.Millisecond)) {
		t.Fatal("opposite arrow discarded")
	}
	gate.release(2)
	if !gate.accept(2, false, now.Add(28*time.Millisecond)) {
		t.Fatal("press after release discarded")
	}
}

func TestArrowCatchUpAfterPause(t *testing.T) {
	var gate arrowRepeatGate
	now := time.Now()
	gate.accept(1, false, now)
	later := now.Add(time.Second)
	if !gate.accept(1, true, later) {
		t.Fatal("first repeat after pause discarded")
	}
	for i := 0; i < 70; i++ {
		if gate.accept(1, true, later) {
			t.Fatal("catch-up burst delivered")
		}
	}
}
