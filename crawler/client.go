package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ErrMaxRetries is an error for max retries reached.
var ErrMaxRetries = fmt.Errorf("max retries reached")

// RetryRoundTripper does HTTP request with retries support.
type RetryRoundTripper struct {
	next       http.RoundTripper
	maxRetries uint8
	backoff    func(attempt uint8) time.Duration
}

func (rrt *RetryRoundTripper) do(req *http.Request, i uint8) (*http.Response, error) {
	ctx := req.Context()
	delay := rrt.backoff(i)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
		// wait context or delay
		slog.Debug("attempt", "number", i, "delay", delay)
	}

	return rrt.next.RoundTrip(req)
}

// RoundTrip выполняет HTTP-запрос с поддержкой повторных попыток
func (rrt *RetryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp *http.Response
		stop bool
		err  error
	)

	// do retries from 0 to maxRetries-1
	for i := range rrt.maxRetries {
		reqCopy := cloneRequest(req)
		resp, err = rrt.do(reqCopy, i)

		if stop, err = stopRetry(resp, err); stop {
			return resp, err
		}
		slog.Warn("attempt", "number", i, "error", err)
	}

	if err != nil {
		return nil, errors.Join(ErrMaxRetries, err)
	}

	return nil, ErrMaxRetries
}

// NewRetryClient creates a new HTTP client with retries support.
func NewRetryClient(maxRetries uint8, roundTripper http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &RetryRoundTripper{
			next:       roundTripper,
			maxRetries: maxRetries,
			backoff:    calcDelay,
		},
		Timeout: timeout,
	}
}

// cloneRequest creates a copy of the request.
func cloneRequest(req *http.Request) *http.Request {
	r2 := new(http.Request)
	*r2 = *req
	return r2
}

// calcDelay returns delay for the next retry attempt.
func calcDelay(attempt uint8) time.Duration {
	const offset int64 = 20
	if attempt == 0 {
		return 0
	}

	var delay int64 = 1 << (attempt - 1)
	return time.Duration(offset*delay) * time.Millisecond
}

// stopRetry checks if we need to stop retries.
// If the first return value is true, then we need to stop retries.
func stopRetry(resp *http.Response, err error) (bool, error) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return true, err
		}

		return false, err
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		return false, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	return true, nil
}
