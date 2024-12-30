package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
)

// responseWriter is a wrapper around http.ResponseWriter that captures the status code.
type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
	written     int64
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

// LoggingMiddleware creates a middleware that logs incoming requests and their duration
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := requestID()

		ctx := r.Context()
		ctx = context.WithValue(ctx, requestIDKey, reqID)
		r = r.WithContext(ctx)

		slog.Info("request started",
			"id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		wrappedWriter := wrapResponseWriter(w)
		wrappedWriter.Header().Set("X-Request-ID", reqID)

		next.ServeHTTP(wrappedWriter, r)
		duration := time.Since(start)
		attrs := []any{
			slog.String("id", reqID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", remoteAddress(r)),
			slog.Int("status", wrappedWriter.status),
			slog.Duration("duration", duration),
		}

		if len(r.URL.RawQuery) > 0 {
			attrs = append(attrs, slog.String("query", r.URL.RawQuery))
		}

		// select log level
		if wrappedWriter.status >= http.StatusInternalServerError {
			slog.ErrorContext(ctx, "request completed with server error", attrs...)
		} else if wrappedWriter.status >= http.StatusBadRequest {
			slog.WarnContext(ctx, "request completed with client error", attrs...)
		} else {
			slog.InfoContext(ctx, "request completed", attrs...)
		}
	})
}

// ErrorHandlingMiddleware handles panics and logs the error
func ErrorHandlingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				reqID, _ := GetRequestID(r.Context())
				slog.Error("panic recovered",
					"error", err,
					"id", reqID,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// HealthCheckMiddleware is a middleware that handles health check requests.
func HealthCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const healthCheckPath = "/ok"

		if strings.TrimRight(r.URL.Path, "/") == healthCheckPath {
			w.Header().Set("Content-Type", "text/plain")

			if _, err := w.Write([]byte("OK")); err != nil {
				reqID, _ := GetRequestID(r.Context())
				slog.Error("response write error", "id", reqID, "error", err)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleGroup is a main logic for handling group requests.
func handleGroup(groups map[string]*cfg.Group, cr crawler.Getter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := strings.Trim(r.URL.Path, "/ ")

		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		group, ok := groups[url]
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		force := parseBool(r.FormValue("force"))
		decode := parseBool(r.FormValue("decode"))
		groupData := cr.Get(group.Name, force, decode)

		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write(groupData); err != nil {
			reqID, exists := GetRequestID(r.Context())
			if !exists {
				reqID = "unknown"
				slog.Warn("request id not found", "method", r.Method, "path", r.URL.Path)
			}

			slog.Error("response write error", "id", reqID, "error", err)
		}
	}
}
