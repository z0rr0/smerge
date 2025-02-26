package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

var (
	// ErrMaxRetries is an error for max retries reached.
	ErrMaxRetries = fmt.Errorf("max retries reached")

	// ErrRequest is an error if request failed.
	ErrRequest = fmt.Errorf("request error")
)

// retryCheckFunc is a function that checks if we need to retry the request.
type retryCheckFunc func(resp *http.Response) error

// delayFunc is a function that returns delay for the next retry attempt.
type delayFunc func(attempt uint8) time.Duration

// RetryRoundTripper does HTTP request with retries support.
type RetryRoundTripper struct {
	next          http.RoundTripper
	maxRetries    uint8
	delayStrategy delayFunc
	retryCheck    retryCheckFunc
}

func (rrt *RetryRoundTripper) do(req *http.Request, i uint8) (*http.Response, error) {
	ctx := req.Context()
	delay := rrt.delayStrategy(i)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
		// wait context or delay
		slog.Debug("attempt", "number", i, "delay", delay)
	}

	resp, err := rrt.next.RoundTrip(req)
	if err != nil {
		return nil, errors.Join(ErrRequest, err)
	}

	return resp, nil
}

// RoundTrip does HTTP request with retries support.
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

		if stop, err = stopRetry(err, resp, rrt.retryCheck); stop {
			return resp, err
		}
		slog.Warn("attempt", "number", i, "error", err)
	}

	if err != nil {
		return nil, errors.Join(ErrMaxRetries, err)
	}

	// only if rrt.MaxRetries == 0
	return nil, ErrMaxRetries
}

// NewRetryClient creates a new HTTP client with retries support.
func NewRetryClient(maxRetries uint8, rt http.RoundTripper, timeout time.Duration, rc retryCheckFunc, ds delayFunc) *http.Client {
	return &http.Client{
		Transport: &RetryRoundTripper{
			next:          rt,
			maxRetries:    maxRetries,
			delayStrategy: ds,
			retryCheck:    rc,
		},
		Timeout: timeout,
	}
}

// cloneRequest creates a copy of the request.
func cloneRequest(req *http.Request) *http.Request {
	return req.Clone(req.Context())
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
func stopRetry(err error, resp *http.Response, retryCheck retryCheckFunc) (bool, error) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return true, err
		}

		return false, err
	}

	if retryErr := retryCheck(resp); retryErr != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			retryErr = errors.Join(retryErr, closeErr)
		}
		return false, retryErr
	}

	return true, nil
}

// retryInternalServerError checks if we need to retry on internal server error.
// It returns nil then we need to stop retries.
// It is a custom variant of retryCheckFunc.
func retryInternalServerError(resp *http.Response) error {
	if resp.StatusCode < http.StatusInternalServerError {
		return nil
	}

	return fmt.Errorf("status code: %d", resp.StatusCode)
}
