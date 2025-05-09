package dashboard

import (
	"html"
	"net/http"
	"time"

	"github.com/networkteam/devlog/collector"
)

type Handler struct {
	logCollector        *collector.LogCollector
	httpClientCollector *collector.HTTPClientCollector

	mux http.Handler
}

type HandlerOptions struct {
	LogCollector        *collector.LogCollector
	HTTPClientCollector *collector.HTTPClientCollector
}

func NewHandler(options HandlerOptions) *Handler {
	mux := http.NewServeMux()
	handler := &Handler{
		logCollector:        options.LogCollector,
		httpClientCollector: options.HTTPClientCollector,
		mux:                 mux,
	}

	mux.HandleFunc("/logs", handler.getLogs)
	mux.HandleFunc("/http-client-requests", handler.getHTTPClientRequests)

	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) getLogs(w http.ResponseWriter, r *http.Request) {
	recentLogs := h.logCollector.Tail(10)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent Logs</h1><ul>"))
	for _, log := range recentLogs {
		_, _ = w.Write([]byte("<li>" + log.Time.Format(time.RFC3339) + " " + html.EscapeString(log.Message) + "</li>"))
	}
	_, _ = w.Write([]byte("</ul></body></html>"))

	// templ.Handler(views.RootPage()).ServeHTTP(w, r)
}

func (h *Handler) getHTTPClientRequests(w http.ResponseWriter, r *http.Request) {
	requests := h.httpClientCollector.GetRequests(10)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent HTTP Client Requests</h1><ul>"))
	for _, req := range requests {
		_, _ = w.Write([]byte("<li>" + req.RequestTime.Format(time.RFC3339) + " " + html.EscapeString(req.Method) + " " + html.EscapeString(req.URL) + " " + html.EscapeString(req.ResponseBody.String()) + "</li>"))

	}
	_, _ = w.Write([]byte("</ul></body></html>"))
}
