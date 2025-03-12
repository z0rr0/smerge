package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
)

type mockCrawler struct {
	data string
}

func (m *mockCrawler) Get(_ string, _ bool, _ bool) ([]byte, error) {
	return []byte(m.data), nil
}

type mockCrawlerError struct{}

func (m *mockCrawlerError) Get(_ string, _ bool, _ bool) ([]byte, error) {
	return nil, crawler.ErrGroupDecode
}

type writerError struct {
	Code      int
	HeaderMap http.Header
	Body      *bytes.Buffer
}

func newWriterError() *writerError {
	return &writerError{
		Code:      http.StatusOK,
		HeaderMap: make(http.Header),
		Body:      new(bytes.Buffer),
	}
}

func (r *writerError) Header() http.Header {
	return r.HeaderMap
}

func (r *writerError) Write([]byte) (int, error) {
	return 0, http.ErrAbortHandler
}

func (r *writerError) WriteHeader(code int) {
	r.Code = code
}

func TestHandleGroup(t *testing.T) {
	mockData := "test data"
	cr := &mockCrawler{data: mockData}
	crWithErr := &mockCrawlerError{}

	groups := map[string]*cfg.Group{
		"test":  {Name: "test"},
		"other": {Name: "other"},
	}

	tests := []struct {
		name         string
		getter       crawler.Getter
		writer       http.ResponseWriter
		method       string
		path         string
		force        string
		decode       string
		expectedCode int
		expectedBody string
	}{
		{
			name:         "valid request",
			getter:       cr,
			method:       "GET",
			path:         "/test",
			expectedCode: http.StatusOK,
			expectedBody: mockData,
		},
		{
			name:         "valid request with force",
			getter:       cr,
			method:       "GET",
			path:         "/test",
			force:        "true",
			expectedCode: http.StatusOK,
			expectedBody: mockData,
		},
		{
			name:         "not found",
			getter:       cr,
			method:       "GET",
			path:         "/unknown",
			expectedCode: http.StatusNotFound,
			expectedBody: "Not Found\n",
		},
		{
			name:         "error from crawler",
			getter:       crWithErr,
			method:       "GET",
			path:         "/test",
			expectedCode: http.StatusInternalServerError,
			expectedBody: "Internal Server Error\n",
		},
		{
			name:         "write error",
			getter:       cr,
			writer:       newWriterError(),
			method:       "GET",
			path:         "/test",
			expectedCode: http.StatusOK,
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			var (
				u, err   = url.Parse(tc.path)
				recorder = tc.writer
			)

			if err != nil {
				t.Fatal(err)
			}

			q := u.Query()
			if tc.force != "" {
				q.Set("force", tc.force)
			}
			if tc.decode != "" {
				q.Set("decode", tc.decode)
			}

			u.RawQuery = q.Encode()
			req := httptest.NewRequest(tc.method, u.String(), nil)
			handler := handleGroup(groups, tc.getter)

			if recorder == nil {
				recorder = httptest.NewRecorder()
			}

			handler.ServeHTTP(recorder, req)

			rec, ok := recorder.(*httptest.ResponseRecorder)
			if !ok {
				// used writerError
				if we, isOk := recorder.(*writerError); !isOk {
					t.Fatal("invalid writer")
				} else {
					if we.Code != tc.expectedCode {
						t.Errorf("got status code %d, want %d", we.Code, tc.expectedCode)
					}
				}
				return
			}

			if rec.Code != tc.expectedCode {
				t.Errorf("got status code %d, want %d", rec.Code, tc.expectedCode)
			}

			if body := rec.Body.String(); body != tc.expectedBody {
				t.Errorf("got body %q, want %q", body, tc.expectedBody)
			}

			if tc.expectedCode == http.StatusOK {
				contentType := recorder.Header().Get("Content-Type")
				if contentType != "text/plain" {
					t.Errorf("got Content-Type %q, want %q", contentType, "text/plain")
				}
			}
		})
	}
}
