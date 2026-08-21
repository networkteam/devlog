package collector_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestHTTPClientCollector_UnreadResponseBody(t *testing.T) {
	// Create a smaller response to make debugging easier
	largeResponse := strings.Repeat("a", 100)

	// Create a server that tracks whether the body was read
	bodyReadTracker := &BodyReadTracker{data: largeResponse}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(largeResponse)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeResponse))
		bodyReadTracker.serverWroteBody = true
	}))
	defer server.Close()

	// Create a collector with specific options for testing
	options := collector.DefaultHTTPClientOptions()
	options.MaxBodySize = 1024 // Ensure it's large enough for our test data
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	// Start collecting before making request
	collect := Collect(t, httpCollector.Subscribe)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request but intentionally don't read the body
	resp, err := client.Get(server.URL)
	require.NoError(t, err)

	// Verify we got a response
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, strconv.Itoa(len(largeResponse)), resp.Header.Get("Content-Length"))

	// Don't read the body, just close it immediately to simulate an application
	// that doesn't consume the response body
	resp.Body.Close()

	requests := collect.Stop()

	// Verify the captured request details
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// Verify the body was captured even though it wasn't read by the client
	captured := req.ResponseBody.String()
	assert.Equal(t, largeResponse, captured)
	assert.True(t, req.ResponseBody.IsFullyCaptured())
}

func TestHTTPClientCollector_Collect_NoCapture_NoEventNoNotify(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	collect := Collect(t, httpCollector.Subscribe)

	// No storage registered, so ShouldCapture is false for any context
	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	// Give the notifier a chance to deliver, if it were going to
	time.Sleep(20 * time.Millisecond)

	assert.Empty(t, collect.Stop())
}

func TestHTTPClientCollector_Collect_DispatchesToStorage(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	req, ok := events[0].Data.(collector.HTTPClientRequest)
	require.True(t, ok)
	assert.Equal(t, "https://example.com", req.URL)
}

func TestHTTPClientCollector_Collect_GroupsUnderParentEvent(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	parentCtx := aggregator.StartEvent(context.Background())

	httpCollector.Collect(parentCtx, collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com/child",
	})

	aggregator.EndEvent(parentCtx, "parent event")

	events := storage.GetEvents(10)
	require.Len(t, events, 1, "the child request must not be dispatched as a top-level event")

	parent := events[0]
	require.Len(t, parent.Children, 1)

	req, ok := parent.Children[0].Data.(collector.HTTPClientRequest)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/child", req.URL)
}

func TestHTTPClientCollector_Collect_NotifiesSubscribers(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	collect := Collect(t, httpCollector.Subscribe)

	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	received := collect.Wait(1)
	require.Len(t, received, 1)
	assert.Equal(t, "https://example.com", received[0].URL)
}

func TestHTTPClientCollector_Collect_AppliesTransformers(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	options.Transformers = []collector.HTTPClientRequestTransformer{
		func(req collector.HTTPClientRequest) collector.HTTPClientRequest {
			if req.Tags == nil {
				req.Tags = map[string]string{}
			}
			req.Tags["transformed"] = "true"
			return req
		},
	}
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	req, ok := events[0].Data.(collector.HTTPClientRequest)
	require.True(t, ok)
	assert.Equal(t, "true", req.Tags["transformed"])
}

func TestHTTPClientCollector_Collect_FillsZeroValues(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	before := time.Now()

	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	req, ok := events[0].Data.(collector.HTTPClientRequest)
	require.True(t, ok)

	assert.NotEqual(t, uuid.Nil, req.ID)
	assert.False(t, req.RequestTime.Before(before))
	assert.Equal(t, req.RequestTime, req.ResponseTime)
}

func TestHTTPClientCollector_Collect_PreservesNonZeroValues(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	id := uuid.Must(uuid.NewV7())
	requestTime := time.Now().Add(-time.Minute)
	responseTime := time.Now().Add(-30 * time.Second)

	httpCollector.Collect(context.Background(), collector.HTTPClientRequest{
		ID:           id,
		Method:       http.MethodGet,
		URL:          "https://example.com",
		RequestTime:  requestTime,
		ResponseTime: responseTime,
	})

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	req, ok := events[0].Data.(collector.HTTPClientRequest)
	require.True(t, ok)

	assert.Equal(t, id, req.ID)
	assert.True(t, requestTime.Equal(req.RequestTime))
	assert.True(t, responseTime.Equal(req.ResponseTime))
}

func TestHTTPClientCollector_Add_NotifiesButNoEvent(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	options := collector.DefaultHTTPClientOptions()
	options.EventAggregator = aggregator
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	collect := Collect(t, httpCollector.Subscribe)

	httpCollector.Add(collector.HTTPClientRequest{
		Method: http.MethodGet,
		URL:    "https://example.com",
	})

	received := collect.Wait(1)
	require.Len(t, received, 1)
	assert.Equal(t, "https://example.com", received[0].URL)

	// Add never reaches the event aggregator
	assert.Empty(t, storage.GetEvents(10))
}

func TestHTTPClientCollector_MaxBodySize(t *testing.T) {
	options := collector.DefaultHTTPClientOptions()
	options.MaxBodySize = 4096
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	assert.Equal(t, 4096, httpCollector.MaxBodySize())
}

// BodyReadTracker tracks if a response body was read
type BodyReadTracker struct {
	data            string
	serverWroteBody bool
	clientReadBody  bool
}

func (b *BodyReadTracker) Read(p []byte) (int, error) {
	if len(p) > len(b.data) {
		copy(p, b.data)
		b.clientReadBody = true
		return len(b.data), io.EOF
	}

	copy(p, b.data[:len(p)])
	b.data = b.data[len(p):]
	b.clientReadBody = true
	return len(p), nil
}

func (b *BodyReadTracker) Close() error {
	return nil
}
