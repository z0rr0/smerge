package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// GetRequestID returns request ID from context.
func GetRequestID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}

// requestID generates a new request ID. It uses 16 bytes of random data or current nanoseconds timestamp as a fallback.
func requestID() string {
	var bytes = make([]byte, 16)

	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		slog.Warn("failed to generate request ID", "error", err)
		return strconv.Itoa(time.Now().Nanosecond())
	}
	return hex.EncodeToString(bytes)
}

// parseBool converts string value to boolean.
// Accepted values: true, 1, yes, on.
func parseBool(value string) bool {
	if v := strings.ToLower(value); v == "true" || v == "1" || v == "yes" || v == "on" {
		return true
	}

	return false
}
