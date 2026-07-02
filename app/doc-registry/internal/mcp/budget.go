package mcp

import (
	"fmt"
	"sync"
	"time"
)

// BudgetTracker enforces per-run limits on repo tool calls and bytes returned.
type BudgetTracker struct {
	maxCalls int
	maxBytes int
	ttl      time.Duration

	mu      sync.Mutex
	entries map[string]*budgetEntry
}

type budgetEntry struct {
	calls     int
	bytes     int
	expiresAt time.Time
}

func NewBudgetTracker(maxCalls, maxBytes int, ttl time.Duration) *BudgetTracker {
	return &BudgetTracker{
		maxCalls: maxCalls,
		maxBytes: maxBytes,
		ttl:      ttl,
		entries:  make(map[string]*budgetEntry),
	}
}

// Before checks whether another repo tool call is allowed for this run (call count)
// and increments the call counter. Must be paired with After on success.
func (b *BudgetTracker) Before(runID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.cleanupExpired(now)

	e := b.getOrResetEntry(runID, now)
	if e.calls >= b.maxCalls {
		return fmt.Errorf("repo_budget_exceeded: %d calls exceeds limit of %d", e.calls, b.maxCalls)
	}
	e.calls++
	return nil
}

// After records the byte cost of a completed repo tool response and enforces the byte budget.
func (b *BudgetTracker) After(runID string, responseBytes int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.cleanupExpired(now)

	e := b.getOrResetEntry(runID, now)
	e.bytes += responseBytes
	if e.bytes > b.maxBytes {
		return fmt.Errorf("repo_budget_exceeded: %d bytes exceeds limit of %d", e.bytes, b.maxBytes)
	}
	return nil
}

func (b *BudgetTracker) cleanupExpired(now time.Time) {
	for k, e := range b.entries {
		if now.After(e.expiresAt) {
			delete(b.entries, k)
		}
	}
}

func (b *BudgetTracker) getOrResetEntry(runID string, now time.Time) *budgetEntry {
	e, ok := b.entries[runID]
	if !ok {
		e = &budgetEntry{expiresAt: now.Add(b.ttl)}
		b.entries[runID] = e
		return e
	}
	if now.After(e.expiresAt) {
		e.calls = 0
		e.bytes = 0
		e.expiresAt = now.Add(b.ttl)
	}
	return e
}
