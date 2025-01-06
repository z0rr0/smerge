package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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
			ctx:    context.WithValue(context.Background(), requestID, "test-id"),
			wantID: "test-id",
			wantOK: true,
		},
		{
			name: "context with wrong type",
			ctx:  context.WithValue(context.Background(), requestID, 123),
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
		id := generateRequestID()
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
		{name: "empty string"},
		{name: "true", value: "true", want: true},
		{name: "True", value: "True", want: true},
		{name: "t", value: "t", want: true},
		{name: "TRUE", value: "TRUE", want: true},
		{name: "1", value: "1", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "y", value: "y", want: true},
		{name: "YES", value: "YES", want: true},
		{name: "on", value: "on", want: true},
		{name: "ON", value: "ON", want: true},
		{name: "false", value: "false"},
		{name: "False", value: "False"},
		{name: "0", value: "0"},
		{name: "no", value: "no"},
		{name: "off", value: "off"},
		{name: "random string", value: "random"},
		{name: "random number", value: "123"},
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

func TestRemoteAddress(t *testing.T) {
	tests := []struct {
		name       string
		request    *http.Request
		withHeader bool
		header     string
		remoteAddr string
		expected   string
	}{
		{
			name:       "nil request",
			header:     "192.168.1.1",
			remoteAddr: "192.168.1.2:1234",
			expected:   "",
		},
		{
			name:       "with header",
			request:    httptest.NewRequest("GET", "/", nil),
			withHeader: true,
			header:     "192.168.1.1",
			remoteAddr: "192.168.1.2:1234",
			expected:   "192.168.1.1",
		},
		{
			name:       "no header",
			request:    httptest.NewRequest("GET", "/", nil),
			remoteAddr: "192.168.1.2:1234",
			expected:   "192.168.1.2:1234",
		},
		{
			name:       "empty header",
			request:    httptest.NewRequest("GET", "/", nil),
			withHeader: true,
			header:     "",
			remoteAddr: "192.168.1.2:1234",
			expected:   "192.168.1.2:1234",
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			if tc.request != nil {
				if tc.withHeader {
					tc.request.Header.Set("X-Real-IP", tc.header)
				}
				tc.request.RemoteAddr = tc.remoteAddr
			}

			if got := remoteAddress(tc.request); got != tc.expected {
				t.Errorf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}
