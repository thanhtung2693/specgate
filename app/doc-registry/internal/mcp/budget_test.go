package mcp

import (
	"testing"
	"time"
)

func TestBudget_CountsCallsPerRun(t *testing.T) {
	t.Parallel()
	b := NewBudgetTracker(5, 1024, 2*time.Hour)
	for i := 0; i < 5; i++ {
		if err := b.Before("run-1"); err != nil {
			t.Fatalf("call %d should succeed before: %v", i, err)
		}
		if err := b.After("run-1", 100); err != nil {
			t.Fatalf("call %d should succeed after: %v", i, err)
		}
	}
	if err := b.Before("run-1"); err == nil {
		t.Fatal("6th before should exceed budget")
	}
}

func TestBudget_CountsBytesPerRun(t *testing.T) {
	t.Parallel()
	b := NewBudgetTracker(100, 500, 2*time.Hour)
	if err := b.Before("run-2"); err != nil {
		t.Fatal(err)
	}
	if err := b.After("run-2", 400); err != nil {
		t.Fatal(err)
	}
	if err := b.Before("run-2"); err != nil {
		t.Fatal(err)
	}
	if err := b.After("run-2", 200); err == nil {
		t.Fatal("should exceed bytes budget")
	}
}

func TestBudget_SeparateRunsIndependent(t *testing.T) {
	t.Parallel()
	b := NewBudgetTracker(2, 1024, 2*time.Hour)
	_ = b.Before("run-a")
	_ = b.After("run-a", 100)
	_ = b.Before("run-a")
	_ = b.After("run-a", 100)
	if err := b.Before("run-b"); err != nil {
		t.Fatal("run-b should have its own budget")
	}
}

func TestBudget_TTLExpiry(t *testing.T) {
	t.Parallel()
	b := NewBudgetTracker(1, 1024, 50*time.Millisecond)
	_ = b.Before("run-ttl")
	_ = b.After("run-ttl", 100)
	if err := b.Before("run-ttl"); err == nil {
		t.Fatal("should exceed after 1 call")
	}
	time.Sleep(60 * time.Millisecond)
	if err := b.Before("run-ttl"); err != nil {
		t.Fatalf("should reset after TTL: %v", err)
	}
	if err := b.After("run-ttl", 100); err != nil {
		t.Fatal(err)
	}
}
