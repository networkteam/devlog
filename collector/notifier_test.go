package collector_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestNotifier_BasicFunctionality(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to notifications
	ch := notifier.Subscribe(ctx)
	require.NotNil(t, ch)

	// Send a notification
	testMessage := "Hello, World!"
	notifier.Notify(testMessage)

	// Wait for the notification to be received
	select {
	case msg := <-ch:
		assert.Equal(t, testMessage, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for notification")
	}
}

func TestNotifier_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Number of subscribers to test
	numSubscribers := 5

	// Create multiple subscribers
	subscribers := make([]<-chan string, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subscribers[i] = notifier.Subscribe(ctx)
	}

	// Send a notification
	testMessage := "Broadcast Message"
	notifier.Notify(testMessage)

	// Verify all subscribers received the message
	for i, ch := range subscribers {
		select {
		case msg := <-ch:
			assert.Equal(t, testMessage, msg, "Subscriber %d received incorrect message", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Subscriber %d timed out waiting for notification", i)
		}
	}
}

func TestNotifier_Unsubscribe(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to notifications
	ch := notifier.Subscribe(ctx)
	require.NotNil(t, ch)

	// Send a notification that should be received
	notifier.Notify("Before Unsubscribe")

	// Wait for the notification to be received
	select {
	case msg := <-ch:
		assert.Equal(t, "Before Unsubscribe", msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for first notification")
	}

	// Manually unsubscribe
	notifier.Unsubscribe(ch)

	// Send another notification after unsubscribing
	notifier.Notify("After Unsubscribe")

	// Verify that no more messages are received (channel should be closed)
	select {
	case msg, ok := <-ch:
		if ok {
			t.Fatalf("Received unexpected message after unsubscribe: %s", msg)
		}
		// Channel is closed, which is correct behavior
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel was not closed after unsubscribe")
	}
}

func TestNotifier_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())

	// Subscribe to notifications
	ch := notifier.Subscribe(ctx)
	require.NotNil(t, ch)

	// Send a notification that should be received
	notifier.Notify("Before Context Cancel")

	// Wait for the notification to be received
	select {
	case msg := <-ch:
		assert.Equal(t, "Before Context Cancel", msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for first notification")
	}

	// Cancel the context, which should trigger unsubscribe
	cancel()

	// Give some time for the unsubscribe to take effect
	time.Sleep(50 * time.Millisecond)

	// Send another notification after context cancellation
	notifier.Notify("After Context Cancel")

	// Verify that no more messages are received (channel should be closed)
	select {
	case msg, ok := <-ch:
		if ok {
			t.Fatalf("Received unexpected message after context cancel: %s", msg)
		}
		// Channel is closed, which is correct behavior
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel was not closed after context cancellation")
	}
}

func TestNotifier_BufferOverflow(t *testing.T) {
	t.Parallel()

	// Create a notifier with small buffer sizes for testing overflow
	options := collector.NotifierOptions{
		SubscriberBufferSize:   2,
		NotificationBufferSize: 3,
	}
	notifier := collector.NewNotifierWithOptions[int](options)
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe but don't read from the channel initially
	ch := notifier.Subscribe(ctx)
	require.NotNil(t, ch)

	// Create a barrier to synchronize the test
	var wg sync.WaitGroup
	wg.Add(1)

	// Send more notifications than the buffers can hold
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			notifier.Notify(i)
		}
	}()

	// Wait for the goroutine to complete
	wg.Wait()

	// Now read from the channel and verify we get at least some values
	// We can't guarantee exactly which values due to the async nature and buffer limits
	receivedCount := 0
	receivedValues := make(map[int]bool)

	// Read all available messages (should be at least the subscriber buffer size)
	timeout := time.After(200 * time.Millisecond)
readLoop:
	for {
		select {
		case val, ok := <-ch:
			if !ok {
				break readLoop
			}
			receivedCount++
			receivedValues[val] = true
		case <-timeout:
			break readLoop
		}
	}

	// We should have received at least some values
	assert.Greater(t, receivedCount, 0, "Should have received at least one notification")
	assert.LessOrEqual(t, receivedCount, options.SubscriberBufferSize+options.NotificationBufferSize,
		"Should not receive more notifications than the combined buffer sizes")
	t.Logf("Received %d notifications with buffer sizes: notifier=%d, subscriber=%d",
		receivedCount, options.NotificationBufferSize, options.SubscriberBufferSize)
}

func TestNotifier_ConcurrentSubscribers(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()
	defer notifier.Close()

	// Number of concurrent subscribers
	numSubscribers := 50

	// Create a channel to collect all received messages
	receivedMessages := make(chan string, numSubscribers*2)

	// Create multiple subscribers concurrently
	var wg sync.WaitGroup
	wg.Add(numSubscribers)

	for i := 0; i < numSubscribers; i++ {
		go func(id int) {
			defer wg.Done()

			// Create a context with timeout for this subscriber
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			// Subscribe to notifications
			ch := notifier.Subscribe(ctx)

			// Wait for a notification or context timeout
			select {
			case msg, ok := <-ch:
				if ok {
					receivedMessages <- msg
				}
			case <-ctx.Done():
				// Context timed out, which is fine
			}
		}(i)
	}

	// Give some time for subscriptions to be established
	time.Sleep(50 * time.Millisecond)

	// Send a notification
	testMessage := "Concurrent Broadcast"
	notifier.Notify(testMessage)

	// Wait for all subscriber goroutines to complete
	wg.Wait()
	close(receivedMessages)

	// Count how many subscribers received the message
	receivedCount := 0
	for msg := range receivedMessages {
		assert.Equal(t, testMessage, msg)
		receivedCount++
	}

	// Some subscribers may not receive the message due to race conditions
	// but a significant number should
	t.Logf("Message received by %d out of %d subscribers", receivedCount, numSubscribers)
	assert.Greater(t, receivedCount, 0, "At least some subscribers should receive the message")
}

func TestNotifier_Close(t *testing.T) {
	t.Parallel()

	// Create a notifier for string messages
	notifier := collector.NewNotifier[string]()

	// Create multiple subscribers
	ctx := context.Background()
	subscribers := make([]<-chan string, 5)
	for i := range subscribers {
		subscribers[i] = notifier.Subscribe(ctx)
	}

	// Close the notifier
	notifier.Close()

	// Verify all subscriber channels are closed
	for i, ch := range subscribers {
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "Channel %d should be closed", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Channel %d was not closed after notifier closed", i)
		}
	}

	// Verify that new subscriptions after closing return a closed channel
	newCh := notifier.Subscribe(ctx)
	select {
	case _, ok := <-newCh:
		assert.False(t, ok, "New subscription after close should return a closed channel")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("New subscription after close did not return a closed channel")
	}

	// Verify that notifications after closing are ignored (should not panic)
	notifier.Notify("After Close")
}

func TestNotifier_HighVolume(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping high-volume test in short mode")
	}

	// Create a notifier with larger buffers for high volume testing
	options := collector.NotifierOptions{
		SubscriberBufferSize:   500,
		NotificationBufferSize: 1000,
	}
	notifier := collector.NewNotifierWithOptions[int](options)
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Number of subscribers and notifications
	numSubscribers := 5
	numNotifications := 1000

	// Create channels to track received messages
	received := make([]chan int, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		received[i] = make(chan int, numNotifications)
	}

	// Create multiple subscribers
	var wg sync.WaitGroup
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := notifier.Subscribe(ctx)

			// Read notifications until the channel is closed or we've read enough
			count := 0
			for val := range ch {
				received[id] <- val
				count++
				if count >= numNotifications {
					break
				}
			}
		}(i)
	}

	// Give subscribers time to start
	time.Sleep(50 * time.Millisecond)

	// Send notifications in a separate goroutine
	go func() {
		for i := 0; i < numNotifications; i++ {
			notifier.Notify(i)

			// Occasionally sleep to simulate varying notification rates
			if i%100 == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Wait for a reasonable time for processing to complete
	time.Sleep(2 * time.Second)
	cancel() // Cancel context to signal subscribers to stop

	// Wait for all subscribers to finish
	wg.Wait()

	// Count total received notifications
	totalReceived := 0
	for i := 0; i < numSubscribers; i++ {
		close(received[i])
		count := 0
		for range received[i] {
			count++
		}
		totalReceived += count
		t.Logf("Subscriber %d received %d notifications", i, count)
	}

	// We should have received a significant number of notifications
	// Not all will be received due to buffer limits
	expectedMinimum := numSubscribers * options.SubscriberBufferSize / 2
	t.Logf("Total notifications received: %d out of maximum possible %d",
		totalReceived, numSubscribers*numNotifications)
	assert.Greater(t, totalReceived, expectedMinimum,
		"Should have received at least half the buffer capacity across all subscribers")
}

func TestNotifier_SlowConsumer(t *testing.T) {
	t.Parallel()

	// Create a notifier with custom buffer sizes
	options := collector.NotifierOptions{
		SubscriberBufferSize:   5,
		NotificationBufferSize: 10,
	}
	notifier := collector.NewNotifierWithOptions[int](options)
	defer notifier.Close()

	// Create a context with cancel for clean subscription management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a slow consumer that processes messages with a delay
	ch := notifier.Subscribe(ctx)

	// Start a goroutine to consume messages slowly
	var wg sync.WaitGroup
	wg.Add(1)

	var receivedValues []int
	var receivedMutex sync.Mutex

	go func() {
		defer wg.Done()
		for val := range ch {
			// Simulate slow processing
			time.Sleep(10 * time.Millisecond)

			receivedMutex.Lock()
			receivedValues = append(receivedValues, val)
			receivedMutex.Unlock()
		}
	}()

	// Send notifications faster than they can be processed
	for i := 0; i < 20; i++ {
		notifier.Notify(i)
		// Send rapidly
		time.Sleep(1 * time.Millisecond)
	}

	// Give some time for processing
	time.Sleep(300 * time.Millisecond)
	cancel() // Stop the consumer

	// Wait for the consumer to finish
	wg.Wait()

	// Verify behavior
	receivedMutex.Lock()
	defer receivedMutex.Unlock()

	// We should have received some values, but not all due to buffer overflow
	t.Logf("Slow consumer received %d out of 20 notifications", len(receivedValues))
	assert.Greater(t, len(receivedValues), 0, "Should have received some notifications")
	assert.Less(t, len(receivedValues), 20, "Should not have received all notifications due to slowness")

	// Values should be in order (no out-of-order delivery)
	for i := 1; i < len(receivedValues); i++ {
		assert.Greater(t, receivedValues[i], receivedValues[i-1],
			"Values should be received in order even with a slow consumer")
	}
}
