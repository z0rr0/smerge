package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// acceptedTrue is a map of accepted true values.
	acceptedTrue = map[string]struct{}{"true": {}, "t": {}, "yes": {}, "y": {}, "on": {}, "enabled": {}, "1": {}}
)

// ctxKey is a type for context key.
type ctxKey string

// requestID is a key for request ID in context.
const requestID ctxKey = "requestID"

// GetRequestID returns request ID from context.
func GetRequestID(ctx context.Context) (string, bool) {
	if reqID, ok := ctx.Value(requestID).(string); ok {
		return reqID, ok
	}

	return "", false
}

// generateRequestID generates a new request ID. It uses 16 bytes of random data or current nanoseconds timestamp as a fallback.
func generateRequestID() string {
	var bytes = make([]byte, 16)

	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		slog.Warn("failed to generate request ID", "error", err)
		return strconv.Itoa(time.Now().Nanosecond())
	}
	return hex.EncodeToString(bytes)
}

// parseBool converts string value to boolean.
// Accepted values: true, 1, yes, on, enabled, t, y.
func parseBool(value string) bool {
	_, ok := acceptedTrue[strings.ToLower(value)]
	return ok
}

// remoteAddress returns remote address from request.
func remoteAddress(r *http.Request) string {
	const httpIPHeader = "X-Real-IP"

	if r == nil {
		return ""
	}

	if ra := r.Header.Get(httpIPHeader); ra != "" {
		return ra
	}

	return r.RemoteAddr
}
