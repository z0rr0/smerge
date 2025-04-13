package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

const (
	timeout   = cfg.Duration(time.Millisecond * 10)
	startTime = time.Millisecond * 250
)

var testSignal = os.Signal(syscall.SIGUSR1)

func waitForServerReady(address string, timeout time.Duration) error {
	const step = 50 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, step)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(step)
	}
	return fmt.Errorf("server did not start within %s", timeout)
}

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
		Timeout:   timeout,
		UserAgent: "TestUserAgent",
		Retries:   3,
		Limiter: cfg.LimitOptions{
			MaxConcurrent: 10,
			Rate:          10.0,
			Burst:         20.0,
			Interval:      cfg.Duration(time.Second),
			CleanInterval: cfg.Duration(time.Minute),
		},
		Groups: []cfg.Group{
			{
				Name:     "test1",
				Endpoint: "/test1",
				Period:   cfg.Duration(time.Hour),
				Subscriptions: []cfg.Subscription{
					{Name: "sub1", Path: cfg.SubPath(subsServer.URL), Timeout: timeout},
				},
			},
			{Name: "test2", Endpoint: "/test2", Period: cfg.Duration(time.Second)},
		},
	}

	serverDone := make(chan struct{})
	go func() {
		Run(config, "test version", testSignal)
		close(serverDone)
	}()
	if err := waitForServerReady(config.Addr(), startTime); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

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

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			url := baseURL + tc.path

			resp, err := client.Get(url)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}

			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("failed to close response body: %v", closeErr)
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

	if err4 := proc.Signal(testSignal); err4 != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err4)
	}

	select {
	case <-serverDone:
		// server stopped successfully
	case <-time.After(5 * time.Second):
		t.Error("server didn't stop within timeout")
	}
}

func TestRunWithoutRateLimit(t *testing.T) {
	subsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("line1\nline2")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer subsServer.Close()

	config := &cfg.Config{
		Host:      "localhost",
		Port:      43210,
		Timeout:   timeout,
		UserAgent: "TestUserAgent",
		Retries:   3,
		Limiter:   cfg.LimitOptions{MaxConcurrent: 2},
		Groups: []cfg.Group{
			{
				Name:     "test1",
				Endpoint: "/test1",
				Period:   cfg.Duration(time.Hour),
				Subscriptions: []cfg.Subscription{
					{Name: "sub1", Path: cfg.SubPath(subsServer.URL), Timeout: timeout},
				},
			},
			{Name: "test2", Endpoint: "/test2", Period: cfg.Duration(time.Second)},
		},
	}

	serverDone := make(chan struct{})
	go func() {
		Run(config, "test version", testSignal)
		close(serverDone)
	}()
	if err := waitForServerReady(config.Addr(), startTime); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	client := &http.Client{Timeout: time.Second}
	clientURL := fmt.Sprintf("http://%s/test1/", config.Addr())

	resp, err := client.Get(clientURL)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("failed to find current process: %v", err)
	}

	if err = proc.Signal(testSignal); err != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err)
	}

	select {
	case <-serverDone:
		// server stopped successfully
	case <-time.After(5 * time.Second):
		t.Error("server didn't stop within timeout")
	}
}
