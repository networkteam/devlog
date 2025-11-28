package devlog_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"

	"github.com/networkteam/devlog"
	"github.com/networkteam/devlog/collector"
)

func TestE2E(t *testing.T) {
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	dlog := devlog.NewWithOptions(devlog.Options{
		LogCapacity:        50,
		LogOptions:         nil,
		HTTPClientCapacity: 10,
		HTTPClientOptions: &collector.HTTPClientOptions{
			MaxBodySize:         1024 * 1024,
			CaptureRequestBody:  true,
			CaptureResponseBody: true,
		},
		HTTPServerCapacity: 10,
		HTTPServerOptions: &collector.HTTPServerOptions{
			MaxBodySize:         1024 * 1024,
			CaptureRequestBody:  true,
			CaptureResponseBody: true,
		},
		DBQueryCapacity: 10,
	})

	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Copy the request body to response to simulate processing
		io.Copy(w, r.Body)
	}))

	handler := dlog.CollectHTTPServer(mux)

	server := httptest.NewServer(handler)

	client := server.Client()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				resp, err := client.Post(server.URL, "application/octet-stream", bytes.NewReader(make([]byte, 1024*1024))) // Send 1MB of data
				if err != nil {
					t.Errorf("Failed to make request: %v", err)
					continue
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
				}
			}
		}()
	}

	wg.Wait()

	server.Close()

	client = nil
	server = nil

	dlog.Close()

	dlog = nil
	mux = nil

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	memGrowth := memAfter.HeapAlloc - memBefore.HeapAlloc
	if memGrowth > 2*1024*1024 {
		t.Errorf("Memory leak detected: grew by %d bytes", memGrowth)
	}
}
