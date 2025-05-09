package collector

import (
	"context"
	"fmt"
	"sync"
)

// Notifier is a generic notification system for collected data
type Notifier[T any] struct {
	mu sync.RWMutex
	// subscribers holds the channels for each subscriber while allowing to find a subscribe by its read channel
	subscribers map[<-chan T]chan T
	bufferSize  int
	notifyCh    chan T
	closeOnce   sync.Once
	closed      bool
}

// NotifierOptions configures a notifier
type NotifierOptions struct {
	// SubscriberBufferSize is the buffer size for each subscriber channel
	SubscriberBufferSize int

	// NotificationBufferSize is the buffer size for the internal notification channel
	NotificationBufferSize int
}

// DefaultNotifierOptions returns default options for a notifier
func DefaultNotifierOptions() NotifierOptions {
	return NotifierOptions{
		SubscriberBufferSize:   100,
		NotificationBufferSize: 1000,
	}
}

// NewNotifier creates a new notifier with default options
func NewNotifier[T any]() *Notifier[T] {
	return NewNotifierWithOptions[T](DefaultNotifierOptions())
}

// NewNotifierWithOptions creates a new notifier with specified options
func NewNotifierWithOptions[T any](options NotifierOptions) *Notifier[T] {
	n := &Notifier[T]{
		subscribers: make(map[<-chan T]chan T),
		bufferSize:  options.SubscriberBufferSize,
		notifyCh:    make(chan T, options.NotificationBufferSize),
	}

	// Start background goroutine to handle notifications
	go n.processNotifications()

	return n
}

// Subscribe returns a channel that receives notifications
// The context is used to automatically unsubscribe when done
func (n *Notifier[T]) Subscribe(ctx context.Context) <-chan T {
	n.mu.RLock()
	if n.closed {
		n.mu.RUnlock()
		// Return a closed channel if the notifier is already closed
		ch := make(chan T)
		close(ch)
		return ch
	}
	n.mu.RUnlock()

	// Create a new buffered channel for this subscriber
	ch := make(chan T, n.bufferSize)

	n.mu.Lock()
	n.subscribers[ch] = ch
	n.mu.Unlock()

	// Auto-unsubscribe when context is done
	go func() {
		<-ctx.Done()
		n.Unsubscribe(ch)
	}()

	fmt.Println("Subscribed to notifier")

	return ch
}

// Unsubscribe removes a subscription
func (n *Notifier[T]) Unsubscribe(ch <-chan T) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Convert to writeable channel to find in map
	if realCh, exists := n.subscribers[ch]; exists {
		delete(n.subscribers, ch)
		close(realCh)
	}

	fmt.Println("Unsubscribed from notifier")
}

// Notify sends a notification to all subscribers
// This is non-blocking - if the internal channel is full, the notification is dropped
func (n *Notifier[T]) Notify(item T) {
	n.mu.RLock()
	if n.closed {
		n.mu.RUnlock()
		return
	}
	n.mu.RUnlock()

	// Non-blocking send to notification channel
	select {
	case n.notifyCh <- item:
		// Successfully sent
	default:
		// Channel full, drop notification
	}
}

// Close closes the notifier and all subscriber channels
func (n *Notifier[T]) Close() {
	n.closeOnce.Do(func() {
		n.mu.Lock()
		n.closed = true

		// Close all subscriber channels
		for _, ch := range n.subscribers {
			close(ch)
		}
		n.subscribers = nil

		// Close the notification channel
		close(n.notifyCh)

		n.mu.Unlock()
	})
}

// processNotifications handles distributing notifications to subscribers
func (n *Notifier[T]) processNotifications() {
	for item := range n.notifyCh {
		n.mu.RLock()
		// Copy the subscribers to avoid holding the lock while sending
		subscribers := make([]chan T, 0, len(n.subscribers))
		for _, ch := range n.subscribers {
			subscribers = append(subscribers, ch)
		}
		n.mu.RUnlock()

		// Send to each subscriber (non-blocking)
		for _, ch := range subscribers {
			select {
			case ch <- item:
				// Successfully sent
			default:
				// Subscriber channel is full, drop this notification for this subscriber
			}
		}
	}
}
