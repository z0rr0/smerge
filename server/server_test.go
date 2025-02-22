package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

func TestRun(t *testing.T) {
	subsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("line1\nline2")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer subsServer.Close()

	config := &cfg.Config{
		Host:      "localhost",
		Port:      43210,
		Timeout:   cfg.Duration(time.Second),
		UserAgent: "TestUserAgent",
		Retries:   3,
		Groups: []cfg.Group{
			{
				Name:     "test1",
				Endpoint: "/test1",
				Period:   cfg.Duration(time.Hour),
				Subscriptions: []cfg.Subscription{
					{Name: "sub1", Path: cfg.SubPath(subsServer.URL), Timeout: cfg.Duration(time.Second)},
				},
			},
			{Name: "test2", Endpoint: "/test2", Period: cfg.Duration(time.Second)},
		},
	}

	serverDone := make(chan struct{})
	go func() {
		Run(config, "test version")
		close(serverDone)
	}()

	time.Sleep(100 * time.Millisecond)
	client := &http.Client{
		Timeout: time.Second,
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectBody     bool
	}{
		{
			name:           "valid endpoint",
			path:           "/test1",
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
		{
			name:           "invalid endpoint",
			path:           "/nonexistent",
			expectedStatus: http.StatusNotFound,
			expectBody:     true,
		},
		{
			name:           "not found",
			path:           "/",
			expectedStatus: http.StatusNotFound,
			expectBody:     true,
		},
		{
			name:           "health check",
			path:           "/ok",
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
	}

	baseURL := fmt.Sprintf("http://%s", config.Addr())

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			url := baseURL + tc.path

			resp, err := client.Get(url)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}

			defer func() {
				if err2 := resp.Body.Close(); err2 != nil {
					t.Errorf("failed to close response body: %v", err2)
				}
			}()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tc.expectedStatus)
			}

			if tc.expectBody {
				body, err3 := io.ReadAll(resp.Body)
				if err3 != nil {
					t.Fatalf("failed to read response body: %v", err3)
				}

				if len(body) == 0 {
					t.Error("expected non-empty response body")
				}
			}

			if reqID := resp.Header.Get("X-Request-ID"); reqID == "" {
				t.Error("X-Request-ID header not set")
			}
		})
	}

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("failed to find current process: %v", err)
	}

	if err4 := proc.Signal(syscall.SIGTERM); err4 != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err4)
	}

	select {
	case <-serverDone:
		// server stopped successfully
	case <-time.After(5 * time.Second):
		t.Error("server didn't stop within timeout")
	}
}

func TestRunWithBadAddress(t *testing.T) {
	config := &cfg.Config{
		Host:    "invalid-host-name-that-should-not-resolve",
		Port:    12345,
		Timeout: cfg.Duration(time.Second),
		Groups:  []cfg.Group{{Name: "test", Period: cfg.Duration(time.Second)}},
	}

	done := make(chan struct{})
	go func() {
		Run(config, "test version")
		close(done)
	}()

	select {
	case <-done:
		// Server should fail to start and return
	case <-time.After(5 * time.Second):
		t.Error("server didn't handle bad address properly")
	}
}
