package collector_test

import (
	"context"
	"sync"
	"testing"
)

// TestCollector collects items from a subscription channel for testing.
// This is a test helper that should only be used in tests.
type TestCollector[T any] struct {
	t      testing.TB
	items  []T
	cancel func()
	mu     sync.Mutex
}

// Collect starts collecting from a subscription.
// Use Wait(n) to block until n items are received or timeout.
func Collect[T any](t testing.TB, subscribe func(context.Context) <-chan T) *TestCollector[T] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := subscribe(ctx)

	c := &TestCollector[T]{
		t:      t,
		cancel: cancel,
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

// Stop cancels collection and returns items collected so far.
func (c *TestCollector[T]) Stop() []T {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	items := make([]T, len(c.items))
	copy(items, c.items)
	return items
}
