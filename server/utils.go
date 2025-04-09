package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// HTTP headers for IP address.
	httpIPHeader       = "X-Real-IP"
	httpIPForwardedFor = "X-Forwarded-For"

	// requestIDLen is a length of generated request ID in bytes.
	requestIDLen = 16
)

var (
	// acceptedTrue is a map of accepted true values.
	acceptedTrue = map[string]struct{}{"true": {}, "t": {}, "yes": {}, "y": {}, "on": {}, "enabled": {}, "1": {}}
)

// ctxKey is a type for context key.
type ctxKey string

// requestID is a key for request ID in a request context.
const requestID ctxKey = "requestID"

// GetRequestID returns request ID from context.
// The second return value indicates if the request ID was found.
func GetRequestID(ctx context.Context) (string, bool) {
	if reqID, ok := ctx.Value(requestID).(string); ok {
		return reqID, ok
	}

	return "", false
}

// generateRequestID generates a new request ID.
// It uses requestIDLen bytes of random data or current nanoseconds timestamp as a fallback.
func generateRequestID() string {
	bytes := make([]byte, requestIDLen)

	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		slog.Warn("failed to generate request ID", "error", err)
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(bytes)
}

// parseBool converts string value to boolean.
// Accepted values: true, 1, yes, on, enabled, t, y.
func parseBool(value string) bool {
	value = strings.TrimSpace(value)
	_, ok := acceptedTrue[strings.ToLower(value)]
	return ok
}

// remoteAddress returns remote address from request.
func remoteAddress(r *http.Request) string {
	if r == nil {
		return ""
	}

	if ra := r.Header.Get(httpIPForwardedFor); ra != "" {
		return strings.SplitN(ra, ",", 2)[0]
	}

	if ra := r.Header.Get(httpIPHeader); ra != "" {
		return ra
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		slog.Error("failed to parse remote address", "error", err)
	}

	return host
}
