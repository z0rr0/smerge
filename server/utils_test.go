package server

import (
	"context"
	"testing"
)

func TestGetRequestID(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
		wantOK bool
	}{
		{
			name: "empty context",
			ctx:  context.Background(),
		},
		{
			name:   "context with request ID",
			ctx:    context.WithValue(context.Background(), requestIDKey, "test-id"),
			wantID: "test-id",
			wantOK: true,
		},
		{
			name: "context with wrong type",
			ctx:  context.WithValue(context.Background(), requestIDKey, 123),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := GetRequestID(tt.ctx)

			if gotID != tt.wantID {
				t.Errorf("go = %v, want %v", gotID, tt.wantID)
			}

			if gotOK != tt.wantOK {
				t.Errorf("got = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := requestID()
		if ids[id] {
			t.Errorf("generated duplicate ID: %v", id)
		}
		ids[id] = true

		// hex or timestamp
		if len(id) != 32 && len(id) <= 10 {
			t.Errorf("generated ID with unexpected length: %v", id)
		}
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty string", "", false},
		{"true", "true", true},
		{"True", "True", true},
		{"TRUE", "TRUE", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"YES", "YES", true},
		{"on", "on", true},
		{"ON", "ON", true},
		{"false", "false", false},
		{"False", "False", false},
		{"0", "0", false},
		{"no", "no", false},
		{"off", "off", false},
		{"random string", "random", false},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			if got := parseBool(tc.value); got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}
