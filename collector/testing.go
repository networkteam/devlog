package collector

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestCollector collects items from a subscription channel for testing.
// This is a test helper that should only be used in tests.
type TestCollector[T any] struct {
	t       testing.TB
	items   []T
	cancel  func()
	timeout time.Duration
	mu      sync.Mutex
}

// Collect starts collecting from a subscription.
// Use Wait(n) to block until n items are received or timeout.
func Collect[T any](t testing.TB, subscribe func(context.Context) <-chan T) *TestCollector[T] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := subscribe(ctx)

	c := &TestCollector[T]{
		t:       t,
		cancel:  cancel,
		timeout: time.Second, // sensible default for tests
	}

	// Collect items in background
	go func() {
		for item := range ch {
			c.mu.Lock()
			c.items = append(c.items, item)
			c.mu.Unlock()
		}
	}()

	return c
}

// Wait blocks until n items are received or timeout.
// Fails the test on timeout. Returns collected items.
func (c *TestCollector[T]) Wait(n int) []T {
	c.t.Helper()
	deadline := time.Now().Add(c.timeout)

	for time.Now().Before(deadline) {
		c.mu.Lock()
		count := len(c.items)
		if count >= n {
			items := make([]T, count)
			copy(items, c.items)
			c.mu.Unlock()
			c.cancel()
			return items
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond) // small poll interval
	}

	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t.Fatalf("timeout waiting for %d items, got %d", n, len(c.items))
	return nil
}

// Stop cancels collection and returns items collected so far.
func (c *TestCollector[T]) Stop() []T {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	items := make([]T, len(c.items))
	copy(items, c.items)
	return items
}
