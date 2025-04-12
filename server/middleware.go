package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
	"github.com/z0rr0/smerge/limiter"
)

// healthPaths is a map of health check paths.
var healthPaths = map[string]struct{}{"/ok": {}, "/health": {}, "/ping": {}}

// responseWriter is a wrapper around http.ResponseWriter that captures the status code.
type responseWriter struct {
	http.ResponseWriter
	wroteHeader atomic.Bool
	written     atomic.Int64
	status      int
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if swapped := rw.wroteHeader.CompareAndSwap(false, true); !swapped {
		return // already written
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.WriteHeader(http.StatusOK) // will be ignored if any other status code was set before
	n, err := rw.ResponseWriter.Write(b)

	if err != nil {
		return n, err
	}

	rw.written.Add(int64(n))
	return n, nil
}

// LoggingMiddleware creates a middleware that logs incoming requests and their duration
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			start      = time.Now()
			reqID      = generateRequestID()
			remoteAddr = remoteAddress(r)
		)

		ctx := r.Context()
		ctx = context.WithValue(ctx, requestID, reqID)
		r = r.WithContext(ctx)

		slog.InfoContext(ctx, "request started",
			"id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", remoteAddr,
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
			slog.String("remote_addr", remoteAddr),
			slog.Int("status", wrappedWriter.status),
			slog.Duration("duration", duration),
		}

		if r.URL.RawQuery != "" {
			attrs = append(attrs, slog.String("query", r.URL.RawQuery))
		}

		switch {
		case wrappedWriter.status >= http.StatusInternalServerError:
			slog.ErrorContext(ctx, "request completed with server error", attrs...)
		case wrappedWriter.status >= http.StatusBadRequest:
			slog.WarnContext(ctx, "request completed with client error", attrs...)
		default:
			slog.InfoContext(ctx, "request completed", attrs...)
		}
	})
}

// ErrorHandlingMiddleware handles panics and logs the error.
func ErrorHandlingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				ctx := r.Context()
				reqID, _ := GetRequestID(ctx)
				slog.ErrorContext(ctx, "panic recovered", "error", err, "id", reqID, "stack", string(debug.Stack()))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RateLimiterMiddleware is a middleware that limits the rate of incoming requests.
func RateLimiterMiddleware(next http.Handler, ipLimiter *limiter.IPRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ipLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		remoteAddr := remoteAddress(r)

		if bucket := ipLimiter.GetBucket(remoteAddr); !bucket.Allow() {
			slog.WarnContext(ctx, "rate limit exceeded", "remote_addr", remoteAddr)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidationMiddleware is a middleware that handles validation of HTTP methods.
func ValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HealthCheckMiddleware is a middleware that handles health check requests.
func HealthCheckMiddleware(next http.Handler, versionInfo string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var okResponse = []byte("OK " + versionInfo)

		if _, ok := healthPaths[strings.TrimRight(r.URL.Path, "/")]; ok {
			w.Header().Set("Content-Type", "text/plain")

			if _, err := w.Write(okResponse); err != nil {
				ctx := r.Context()
				reqID, _ := GetRequestID(ctx)
				slog.ErrorContext(ctx, "response write error", "id", reqID, "error", err)
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
		group, ok := groups[url]
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		force := parseBool(r.FormValue("force"))
		decode := parseBool(r.FormValue("decode"))
		groupData, err := cr.Get(group.Name, force, decode)

		if err != nil {
			slog.ErrorContext(r.Context(), "handle group", "name", group.Name, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		if _, writeErr := w.Write(groupData); writeErr != nil {
			ctx := r.Context()
			reqID, exists := GetRequestID(ctx)

			if !exists {
				reqID = "unknown"
				slog.WarnContext(ctx, "request id not found", "method", r.Method, "path", r.URL.Path)
			}

			slog.ErrorContext(ctx, "response write error", "id", reqID, "error", writeErr)
		}
	}
}
