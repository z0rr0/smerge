package server

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseWriter(t *testing.T) {
	tests := []struct {
		name         string
		writeStatus  int
		writeBody    string
		expectStatus int
		expectBody   string
	}{
		{
			name:         "explicit status",
			writeStatus:  http.StatusCreated,
			writeBody:    "test body",
			expectStatus: http.StatusCreated,
			expectBody:   "test body",
		},
		{
			name:         "default status",
			writeBody:    "test body",
			expectStatus: http.StatusOK,
			expectBody:   "test body",
		},
		{
			name:         "multiple writes",
			writeStatus:  http.StatusOK,
			writeBody:    "test body",
			expectStatus: http.StatusOK,
			expectBody:   "test body",
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			wrapped := wrapResponseWriter(rec)

			if tc.writeStatus != 0 {
				wrapped.WriteHeader(tc.writeStatus)
				// 2nd call should be ignored
				wrapped.WriteHeader(http.StatusTeapot)
			}

			if tc.writeBody != "" {
				_, _ = wrapped.Write([]byte(tc.writeBody))
			}

			result := rec.Result()
			defer func() {
				if err := result.Body.Close(); err != nil {
					t.Errorf("failed to close response body: %v", err)
				}
			}()

			if result.StatusCode != tc.expectStatus {
				t.Errorf("got status %d, want %d", result.StatusCode, tc.expectStatus)
			}

			body := rec.Body.String()
			if body != tc.expectBody {
				t.Errorf("got body %q, want %q", body, tc.expectBody)
			}

			// count written bytes
			if n := int64(len(tc.expectBody)); wrapped.written != n {
				t.Errorf("got written bytes %d, want %d", wrapped.written, n)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	tests := []struct {
		name         string
		method       string
		path         string
		query        string
		handler      http.HandlerFunc
		expectedCode int
		checkLogFunc func(logs string) error
	}{
		{
			name:   "success request",
			method: "GET",
			path:   "/test",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			expectedCode: http.StatusOK,
			checkLogFunc: func(logs string) error {
				if !strings.Contains(logs, "request completed") {
					return fmt.Errorf("logs don't contain 'request completed'")
				}
				if !strings.Contains(logs, "method=GET") {
					return fmt.Errorf("logs don't contain method info")
				}
				return nil
			},
		},
		{
			name:   "request with query",
			method: "GET",
			path:   "/test",
			query:  "param=value",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			expectedCode: http.StatusOK,
			checkLogFunc: func(logs string) error {
				if !strings.Contains(logs, "param=value") {
					return fmt.Errorf("logs don't contain query parameters")
				}
				return nil
			},
		},
		{
			name:   "error request",
			method: "GET",
			path:   "/error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "error", http.StatusInternalServerError)
			}),
			expectedCode: http.StatusInternalServerError,
			checkLogFunc: func(logs string) error {
				if !strings.Contains(logs, "server error") {
					return fmt.Errorf("logs don't contain error info")
				}
				return nil
			},
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			logBuf.Reset()

			url := tc.path
			if tc.query != "" {
				url += "?" + tc.query
			}
			req := httptest.NewRequest(tc.method, url, nil)
			rec := httptest.NewRecorder()

			handler := LoggingMiddleware(tc.handler)
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectedCode {
				t.Errorf("got status code %d, want %d", rec.Code, tc.expectedCode)
			}

			if reqID := rec.Header().Get("X-Request-ID"); reqID == "" {
				t.Error("X-Request-ID header is not set")
			}

			if err := tc.checkLogFunc(logBuf.String()); err != nil {
				t.Errorf("log check failed: %v", err)
			}
		})
	}
}

func TestErrorHandlingMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		handler      http.HandlerFunc
		expectedCode int
	}{
		{
			name: "no panic",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			expectedCode: http.StatusOK,
		},
		{
			name: "with panic",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("test panic")
			}),
			expectedCode: http.StatusInternalServerError,
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			handler := ErrorHandlingMiddleware(tc.handler)

			// don't let panic escape
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("handler let panic escape: %v", r)
					}
				}()
				handler.ServeHTTP(rec, req)
			}()

			if rec.Code != tc.expectedCode {
				t.Errorf("got status code %d, want %d", rec.Code, tc.expectedCode)
			}
		})
	}
}
