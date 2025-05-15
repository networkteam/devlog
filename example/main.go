package main

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/networkteam/go-sqllogger"
	slogmulti "github.com/samber/slog-multi"

	"github.com/networkteam/devlog"
	"github.com/networkteam/devlog/collector"
	sqlloggeradapter "github.com/networkteam/devlog/dbadapter/sqllogger"
)

type Todo struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	// 1. Set up slog with devlog middleware

	dlog := devlog.New()
	defer dlog.Close()

	logger := slog.New(
		slogmulti.Fanout(
			// Collect debug logs with devlog
			dlog.CollectSlogLogs(collector.CollectSlogLogsOptions{
				Level: slog.LevelDebug,
			}),
			// Log info to stderr
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}),
		),
	)
	slog.SetDefault(logger)

	// Initialize SQLite database
	connector := newSQLiteConnector(":memory:")
	loggingConnector := sqllogger.LoggingConnector(sqlloggeradapter.New(dlog.CollectDBQuery(), sqlloggeradapter.Options{
		Language: "sqlite",
	}), connector)

	db := sql.OpenDB(loggingConnector)
	defer db.Close()

	var err error

	// Create todos table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS todos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			completed BOOLEAN NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		logger.Error("Failed to create todos table", slog.Any("err", err))
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// 2. Create an HTTP client with devlog middleware (RoundTripper)

	httpClient := &http.Client{
		Transport: dlog.CollectHTTPClient(http.DefaultTransport),
		Timeout:   time.Second * 5,
	}
	type uselessfactResponse struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		Source    string `json:"source"`
		SourceURL string `json:"source_url"`
		Language  string `json:"language"`
		Permalink string `json:"permalink"`
	}
	mux.HandleFunc("/http-client", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/http-client")

		logger.DebugContext(r.Context(), "Requesting uselessfacts API")

		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://uselessfacts.jsph.pl/api/v2/facts/random", nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			logger.ErrorContext(r.Context(), "Failed to get uselessfacts API", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to get uselessfacts API"))
			return
		}
		defer resp.Body.Close()
		var fact uselessfactResponse
		if err := json.NewDecoder(resp.Body).Decode(&fact); err != nil {
			logger.ErrorContext(r.Context(), "Failed to decode uselessfacts API response", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to decode uselessfacts API response"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fact.Text))
	})

	// 3. Create a new HTTP server with a simple handler

	mux.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/log")

		logger.DebugContext(r.Context(), "Debug log from /log HTTP handler", slog.Group("request", slog.String("method", r.Method)))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Log a thing"))
	})

	// Add todo handlers
	mux.HandleFunc("GET /todos", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/todos")
		ctx := r.Context()

		// List todos
		rows, err := db.QueryContext(ctx, "SELECT id, title, completed, created_at FROM todos ORDER BY created_at DESC")
		if err != nil {
			logger.ErrorContext(ctx, "Failed to query todos", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		todos := []Todo{}
		for rows.Next() {
			var todo Todo
			if err := rows.Scan(&todo.ID, &todo.Title, &todo.Completed, &todo.CreatedAt); err != nil {
				logger.ErrorContext(ctx, "Failed to scan todo", slog.Any("err", err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			todos = append(todos, todo)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(todos)
	})

	mux.HandleFunc("POST /todos", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/todos")
		ctx := r.Context()

		// Create todo
		var todo Todo
		if err := json.NewDecoder(r.Body).Decode(&todo); err != nil {
			logger.ErrorContext(ctx, "Failed to decode todo", slog.Any("err", err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		result, err := db.ExecContext(ctx, "INSERT INTO todos (title, completed) VALUES (?, ?)", todo.Title, todo.Completed)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to insert todo", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		id, err := result.LastInsertId()
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get last insert ID", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		todo.ID = id
		todo.CreatedAt = time.Now()

		logger.InfoContext(ctx, "Created todo", slog.Group("todo", "id", todo.ID, "title", todo.Title))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(todo)
	})

	mux.HandleFunc("GET /todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/todos/{id}")
		id := r.PathValue("id")
		ctx := r.Context()

		var todo Todo
		err := db.QueryRowContext(ctx, "SELECT id, title, completed, created_at FROM todos WHERE id = ?", id).
			Scan(&todo.ID, &todo.Title, &todo.Completed, &todo.CreatedAt)
		if err == sql.ErrNoRows {
			logger.WarnContext(ctx, "Todo not found", "id", id)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			logger.ErrorContext(ctx, "Failed to query todo", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(todo)
	})

	mux.HandleFunc("PUT /todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/todos/{id}")
		id := r.PathValue("id")
		ctx := r.Context()

		var todo Todo
		if err := json.NewDecoder(r.Body).Decode(&todo); err != nil {
			logger.ErrorContext(ctx, "Failed to decode todo", slog.Any("err", err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		result, err := db.ExecContext(ctx, "UPDATE todos SET title = ?, completed = ? WHERE id = ?", todo.Title, todo.Completed, id)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to update todo", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		rows, err := result.RowsAffected()
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get rows affected", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if rows == 0 {
			logger.WarnContext(ctx, "Todo not found for update", "id", id)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("DELETE /todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/todos/{id}")
		id := r.PathValue("id")
		ctx := r.Context()

		result, err := db.ExecContext(ctx, "DELETE FROM todos WHERE id = ?", id)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to delete todo", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		rows, err := result.RowsAffected()
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get rows affected", slog.Any("err", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if rows == 0 {
			logger.WarnContext(ctx, "Todo not found for deletion", "id", id)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// 4. Wrap with devlog middleware to inspect requests and responses to the server

	outerMux := http.NewServeMux()
	outerMux.Handle("/", dlog.CollectHTTPServer(
		mux,
	))

	// 5. Mount devlog dashboard

	// Mount under path prefix /_devlog, so we handle the dashboard handler under this path, strip the prefix, so dashboard routes match and inform it about the path prefix to render correct URLs
	outerMux.Handle("/_devlog/", http.StripPrefix("/_devlog", dlog.DashboardHandler("/_devlog")))

	// Add a health check endpoint for refresh

	outerMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Run the server

	logger.Info("Starting server on :1095")
	if err := http.ListenAndServe(":1095", outerMux); err != nil {
		logger.Error("Failed to start server", slog.Group("error", slog.String("message", err.Error())))
		os.Exit(1)
	}
}
