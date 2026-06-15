package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const relayVersion = "0.1.0"

// newMCPServer builds the MCP server and registers the read tools against reg.
func newMCPServer(reg *registry) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "devlog-agent", Version: relayVersion}, nil)
	registerTools(server, reg)
	return server
}

// runRelayDirect attaches to an existing devlog capture session and serves the
// MCP endpoint on loopback, forwarding to the local devlog instance at baseURL.
// It blocks until ctx is canceled. sid may be empty to select interactively.
func runRelayDirect(ctx context.Context, baseURL, sid string, mcpPort int) error {
	sid, err := selectSession(ctx, baseURL, sid)
	if err != nil {
		return err
	}

	reg := newRegistry()
	reg.add("local", newDirectClient(baseURL, sid))

	server := newMCPServer(reg)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	guarded := loopbackHostGuard(mcpHandler)

	mux := http.NewServeMux()
	mux.Handle("/mcp", guarded)
	mux.Handle("/mcp/", guarded)

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mcpPort)))
	if err != nil {
		return err
	}

	fmt.Printf("devlog relay (direct) → %s (session %s)\n", baseURL, sid)
	fmt.Printf("MCP endpoint: http://%s/mcp\n", ln.Addr())

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// loopbackHostGuard rejects requests whose Host header is not a loopback literal.
// This blocks DNS-rebinding attacks from a browser against the local MCP endpoint.
func loopbackHostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden: non-loopback host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host // no port present
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
