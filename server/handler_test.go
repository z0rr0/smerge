package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/z0rr0/smerge/cfg"
)

type mockCrawler struct {
	data string
}

func (m *mockCrawler) Get(_ string, _ bool) string {
	return m.data
}

func TestHandleGroup(t *testing.T) {
	mockData := "test data"
	cr := &mockCrawler{data: mockData}

	groups := map[string]*cfg.Group{
		"test":  {Name: "test"},
		"other": {Name: "other"},
	}

	tests := []struct {
		name         string
		method       string
		path         string
		force        string
		expectedCode int
		expectedBody string
	}{
		{
			name:         "valid request",
			method:       "GET",
			path:         "/test",
			expectedCode: http.StatusOK,
			expectedBody: mockData,
		},
		{
			name:         "valid request with force",
			method:       "GET",
			path:         "/test",
			force:        "true",
			expectedCode: http.StatusOK,
			expectedBody: mockData,
		},
		{
			name:         "not found",
			method:       "GET",
			path:         "/unknown",
			expectedCode: http.StatusNotFound,
			expectedBody: "Not Found\n",
		},
		{
			name:         "wrong method",
			method:       "POST",
			path:         "/test",
			expectedCode: http.StatusMethodNotAllowed,
			expectedBody: "Method Not Allowed\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.path
			if tt.force != "" {
				url += "?force=" + tt.force
			}

			req := httptest.NewRequest(tt.method, url, nil)
			rec := httptest.NewRecorder()

			handler := handleGroup(groups, cr)
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("got status code %d, want %d", rec.Code, tt.expectedCode)
			}

			if body := rec.Body.String(); body != tt.expectedBody {
				t.Errorf("got body %q, want %q", body, tt.expectedBody)
			}

			if tt.expectedCode == http.StatusOK {
				contentType := rec.Header().Get("Content-Type")
				if contentType != "text/plain" {
					t.Errorf("got Content-Type %q, want %q", contentType, "text/plain")
				}
			}
		})
	}
}