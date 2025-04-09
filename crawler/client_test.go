package crawler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockRoundTripper is used to mock HTTP responses for testing
type mockRoundTripper struct {
	responses []*http.Response
	errors    []error
	calls     int
}

type mockReadCloser struct {
	err error
}

func (m *mockReadCloser) Read(_ []byte) (n int, err error) {
	return 0, nil
}

func (m *mockReadCloser) Close() error {
	return m.err
}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	if m.calls >= len(m.responses) {
		return nil, errors.New("no more responses")
	}
	resp := m.responses[m.calls]
	err := m.errors[m.calls]
	m.calls++
	return resp, err
}

func TestRetryRoundTripper_RoundTrip(t *testing.T) {
	var connErr = errors.New("connection error")
	tests := []struct {
		name         string
		maxRetries   uint8
		responses    []*http.Response
		errors       []error
		expectedCode int
		expectError  bool
	}{
		{
			name:         "successful request first try",
			maxRetries:   3,
			responses:    []*http.Response{{StatusCode: http.StatusOK}},
			errors:       []error{nil},
			expectedCode: http.StatusOK,
		},
		{
			name:         "successful after one retry",
			maxRetries:   3,
			responses:    []*http.Response{nil, {StatusCode: http.StatusOK}},
			errors:       []error{connErr, nil},
			expectedCode: http.StatusOK,
		},
		{
			name:       "retry for 5xx status code",
			maxRetries: 3,
			responses: []*http.Response{
				{StatusCode: http.StatusInternalServerError, Body: &mockReadCloser{}},
				{StatusCode: http.StatusOK},
			},
			errors:       []error{nil, nil},
			expectedCode: http.StatusOK,
		},
		{
			name:        "max retries reached",
			maxRetries:  2,
			responses:   []*http.Response{nil, nil},
			errors:      []error{connErr, connErr},
			expectError: true,
		},
		{
			name:       "all retries give 5xx",
			maxRetries: 2,
			responses: []*http.Response{
				{StatusCode: http.StatusInternalServerError, Body: &mockReadCloser{errors.New("test error")}},
				{StatusCode: http.StatusInternalServerError, Body: &mockReadCloser{}},
			},
			errors:      []error{nil, nil},
			expectError: true,
		},
		{
			name:        "no retries",
			expectError: true,
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			mock := &mockRoundTripper{
				responses: tc.responses,
				errors:    tc.errors,
			}

			rrt := &RetryRoundTripper{
				next:          mock,
				maxRetries:    tc.maxRetries,
				delayStrategy: func(attempt uint8) time.Duration { return time.Millisecond },
				retryCheck:    retryInternalServerError,
			}

			req, _ := http.NewRequest("GET", "https://example.com", nil)
			resp, err := rrt.RoundTrip(req)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("expected no error, got %v", err)
				return
			}

			if resp.StatusCode != tc.expectedCode {
				t.Errorf("expected status code %d, got %d", tc.expectedCode, resp.StatusCode)
			}

			expectedCalls := 0
			for i, e := range tc.errors {
				expectedCalls++
				if e == nil && tc.responses[i].StatusCode < http.StatusInternalServerError {
					break
				}
			}

			if mock.calls != expectedCalls {
				t.Errorf("expected %d calls, got %d", expectedCalls, mock.calls)
			}
		})
	}
}

func TestRetryRoundTripper_CancelContext(t *testing.T) {
	var mockErr = errors.New("test error")
	mock := &mockRoundTripper{
		responses: []*http.Response{nil, nil, nil},
		errors:    []error{mockErr, mockErr, mockErr},
	}

	rrt := &RetryRoundTripper{
		next:          mock,
		maxRetries:    3,
		delayStrategy: func(attempt uint8) time.Duration { return 40 * time.Millisecond },
		retryCheck:    retryInternalServerError,
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://example.com", nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := rrt.RoundTrip(req)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestCalcDelay(t *testing.T) {
	tests := []struct {
		attempt uint8
		want    time.Duration
	}{
		{0, 0},
		{1, 20 * time.Millisecond},  // 20 * 2^0
		{2, 40 * time.Millisecond},  // 20 * 2^1
		{3, 80 * time.Millisecond},  // 20 * 2^2
		{4, 160 * time.Millisecond}, // 20 * 2^3
	}

	for i := range tests {
		tc := tests[i]

		t.Run("attempt_"+string(tc.attempt), func(t *testing.T) {
			got := calcDelay(tc.attempt)
			if got != tc.want {
				t.Errorf("calcDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
			}
		})
	}
}

func TestStopRetry(t *testing.T) {
	tests := []struct {
		name string
		resp *http.Response
		rc   retryCheckFunc
		err  error
		stop bool
	}{
		{
			name: "no error 200 response",
			resp: &http.Response{StatusCode: http.StatusOK},
			rc:   retryInternalServerError,
			stop: true,
		},
		{
			name: "network error",
			err:  errors.New("network error"),
		},
		{
			name: "500 server error",
			resp: &http.Response{StatusCode: http.StatusInternalServerError, Body: &mockReadCloser{}},
			rc:   retryInternalServerError,
		},
		{
			name: "404 not found",
			resp: &http.Response{StatusCode: http.StatusNotFound},
			rc:   retryInternalServerError,
			stop: true,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			stop: true,
		},
		{
			name: "custom retry check",
			resp: &http.Response{StatusCode: http.StatusBadRequest},
			rc:   func(resp *http.Response) error { return nil },
			stop: true,
		},
		{
			name: "custom retry check with error",
			resp: &http.Response{StatusCode: http.StatusBadRequest, Body: &mockReadCloser{}},
			rc:   func(resp *http.Response) error { return errors.New("test error") },
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			stop, _ := stopRetry(tc.err, tc.resp, tc.rc)

			if stop != tc.stop {
				t.Errorf("stopRetry() = %v, want %v", stop, tc.stop)
			}
		})
	}
}

func TestNewRetryClient(t *testing.T) {
	tests := []struct {
		name       string
		maxRetries uint8
		transport  *http.Transport
		timeout    time.Duration
	}{
		{
			name:       "default settings",
			maxRetries: 3,
			transport:  nil,
			timeout:    5 * time.Second,
		},
		{
			name:       "custom settings",
			maxRetries: 5,
			transport:  &http.Transport{MaxIdleConns: 10},
			timeout:    10 * time.Second,
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			client := NewRetryClient(tc.maxRetries, tc.transport, tc.timeout, retryInternalServerError, calcDelay)

			if client == nil {
				t.Fatal("NewRetryClient() returned nil")
				return // only for staticcheck
			}

			if client.Timeout != tc.timeout {
				t.Errorf("client.Timeout = %v, want %v", client.Timeout, tc.timeout)
			}

			rrt, ok := client.Transport.(*RetryRoundTripper)
			if !ok {
				t.Fatal("client.Transport is not a *RetryRoundTripper")
				return // only for staticcheck
			}

			if rrt.maxRetries != tc.maxRetries {
				t.Errorf("rrt.maxRetries = %v, want %v", rrt.maxRetries, tc.maxRetries)
			}

			if rrt.delayStrategy == nil {
				t.Error("rrt.delayStrategy is nil")
			}
		})
	}
}

func TestRetryRoundTripper_Integration(t *testing.T) {
	const responseText = "success"
	var serverCallCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCallCount++
		if serverCallCount <= 2 { // default maxRetries is 3
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write([]byte(responseText)); writeErr != nil {
			t.Errorf("error writing response: %v", writeErr)
		}
	}))
	defer server.Close()

	client := NewRetryClient(3, http.DefaultTransport, 5*time.Second, retryInternalServerError, calcDelay)
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("error closing response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading response body: %v", err)
	}

	if bodyText := string(body); bodyText != responseText {
		t.Errorf("expected body %q, got %q", responseText, bodyText)
	}

	if expectedCalls := 3; serverCallCount != expectedCalls {
		t.Errorf("expected %d server calls, got %d", expectedCalls, serverCallCount)
	}
}
