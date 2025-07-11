package crawler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

const (
	userAgentDefault           = "test-agent"
	retriesDefault       uint8 = 3
	maxConcurrentDefault       = 10
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		groups []cfg.Group
		want   int
	}{
		{
			name:   "empty groups",
			groups: []cfg.Group{},
			want:   0,
		},
		{
			name: "single group",
			groups: []cfg.Group{
				{Name: "test1"},
			},
			want: 1,
		},
		{
			name: "multiple groups",
			groups: []cfg.Group{
				{Name: "test1"},
				{Name: "test2"},
				{Name: "test3"},
			},
			want: 3,
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.groups, userAgentDefault, retriesDefault, maxConcurrentDefault, "")

			if got := len(c.groups); got != tc.want {
				t.Errorf("New() got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCrawler_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("line1\nline2")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	tests := []struct {
		name        string
		group       cfg.Group
		force       bool
		expected    []byte
		decode      bool
		forceData   []byte // for error emulation
		errExpected bool
	}{
		{
			name: "basic get",
			group: cfg.Group{
				Name: "test1",
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(time.Second),
			},
			force:    true,
			expected: []byte("line1\nline2"),
		},
		{
			name: "empty group",
			group: cfg.Group{
				Name:          "test2",
				Subscriptions: []cfg.Subscription{},
				Period:        cfg.Duration(time.Second),
			},
			force: true,
		},
		{
			name: "decode group",
			group: cfg.Group{
				Name:    "test3",
				Encoded: true,
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(time.Second),
			},
			force:    true,
			expected: []byte("line1\nline2"),
			decode:   true,
		},
		{
			name: "get error",
			group: cfg.Group{
				Name:    "test4",
				Encoded: true,
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(time.Second),
			},
			decode:      true,
			forceData:   []byte("invalid base64!@#$"),
			errExpected: true,
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			c := New([]cfg.Group{tc.group}, userAgentDefault, retriesDefault, maxConcurrentDefault, "")

			if tc.forceData != nil {
				c.Lock()
				c.result[tc.group.Name] = tc.forceData
				c.Unlock()
			}

			got, err := c.Get(tc.group.Name, tc.force, tc.decode)
			if err != nil {
				if !tc.errExpected {
					t.Errorf("unexpected error: %v", err)
				} else {
					if !errors.Is(err, ErrGroupDecode) {
						t.Errorf("expected ErrGroupDecode, got: %v", err)
					}
				}
				return
			} else {
				if tc.errExpected {
					t.Error("expected error")
					return
				}
			}

			if !slices.Equal(got, tc.expected) {
				t.Errorf("got = %v, want %v", got, tc.expected)
			}
		})
	}
}

// compareResults compares two maps of strings to byte slices.
func compareResults(got, want map[string][]byte) error {
	if n, m := len(got), len(want); n != m {
		return fmt.Errorf("result length mismatch got = %v, want %v", n, m)
	}

	for k, v := range got {
		if !slices.Equal(v, want[k]) {
			return fmt.Errorf("got = %v, want %v", v, want[k])
		}
	}

	return nil
}

func TestCrawler_Run(t *testing.T) {
	const serverTime = 10 * time.Millisecond
	var wg sync.WaitGroup

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(serverTime)
		if _, err := w.Write([]byte("line1\nline2")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
		wg.Done()
	}))
	defer server.Close()

	tests := []struct {
		name           string
		group          cfg.Group
		maxConcurrent  int
		expectedCalls  int
		expectedResult map[string][]byte
	}{
		{
			name: "single call",
			group: cfg.Group{
				Name: "group1",
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(25 * time.Millisecond),
			},
			maxConcurrent:  10,
			expectedCalls:  2, // `1 * 2` due to 1st init
			expectedResult: map[string][]byte{"group1": []byte("line1\nline2")},
		},
		{
			name: "multiple subscriptions",
			group: cfg.Group{
				Name: "group2",
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
					{
						Name:    "sub2",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(25 * time.Millisecond),
			},
			maxConcurrent:  10,
			expectedCalls:  4,
			expectedResult: map[string][]byte{"group2": []byte("line1\nline1\nline2\nline2")},
		},
		{
			name: "limited concurrency",
			group: cfg.Group{
				Name: "group3",
				Subscriptions: []cfg.Subscription{
					{
						Name:    "sub1",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
					{
						Name:    "sub2",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
					{
						Name:    "sub3",
						Path:    cfg.SubPath(server.URL),
						Timeout: cfg.Duration(time.Second),
					},
				},
				Period: cfg.Duration(25 * time.Millisecond),
			},
			maxConcurrent:  1,
			expectedCalls:  6,
			expectedResult: map[string][]byte{"group3": []byte("line1\nline1\nline1\nline2\nline2\nline2")},
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			dataReceived := make(chan struct{})
			wg.Add(tc.expectedCalls)
			c := New([]cfg.Group{tc.group}, userAgentDefault, retriesDefault, tc.maxConcurrent, "")

			go func() {
				wg.Wait()
				close(dataReceived)
			}()

			c.Run()

			select {
			case <-dataReceived:
				t.Logf("received all data, test: %s", tc.name)
			case <-time.After(5 * time.Second):
				t.Error("timeout waiting for crawler to fetch data")
			}

			c.RLock()
			result := maps.Clone(c.result)
			c.RUnlock()

			if err := compareResults(result, tc.expectedResult); err != nil {
				t.Error(err)
			}

			c.Shutdown()
		})
	}
}

func TestCrawler_fetchSubscription(t *testing.T) {
	const fileName = "test.txt"
	var tmpDir = t.TempDir()

	localFile, fileErr := os.Create(filepath.Join(tmpDir, fileName))
	if fileErr != nil {
		t.Fatalf("failed to create test file: %v", fileErr)
	}

	if _, fileErr = localFile.Write([]byte("line10\nline20")); fileErr != nil {
		t.Fatalf("failed to write to test file: %v", fileErr)
	}

	if fileErr = localFile.Close(); fileErr != nil {
		t.Fatalf("failed to close test file: %v", fileErr)
	}

	tests := []struct {
		name         string
		handler      http.HandlerFunc
		subscription cfg.Subscription
		expected     []string
		wantErr      bool
		encoded      bool
	}{
		{
			name: "successful fetch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte("line1\nline2")); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			},
			subscription: cfg.Subscription{
				Name:    "test",
				Timeout: cfg.Duration(time.Second),
			},
			expected: []string{"line1", "line2"},
		},
		{
			name: "local file",
			subscription: cfg.Subscription{
				Name:    "localFile",
				Path:    cfg.SubPath(fileName),
				Timeout: cfg.Duration(time.Second),
				Local:   true,
			},
			expected: []string{"line10", "line20"},
		},
		{
			name: "encoded response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				data := base64.StdEncoding.EncodeToString([]byte("line1\nline2"))
				if _, err := w.Write([]byte(data)); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			},
			subscription: cfg.Subscription{
				Name:    "test",
				Timeout: cfg.Duration(time.Second),
				Encoded: true,
			},
			expected: []string{"line1", "line2"},
			encoded:  true,
		},
		{
			name: "timeout error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(25 * time.Millisecond)
				if _, err := w.Write([]byte("line1\nline2")); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			},
			subscription: cfg.Subscription{
				Name:    "test",
				Timeout: cfg.Duration(10 * time.Millisecond),
			},
			wantErr: true,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			subscription: cfg.Subscription{
				Name:    "test",
				Timeout: cfg.Duration(time.Second),
			},
			wantErr: true,
		},
		{
			name: "skip empty lines",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte("\n\nline1\n\nline2\r\nline3\n\t")); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			},
			subscription: cfg.Subscription{
				Name:    "test",
				Timeout: cfg.Duration(time.Second),
			},
			expected: []string{"line1", "line2", "line3"},
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			if tc.handler != nil {
				server := httptest.NewServer(tc.handler)
				defer server.Close()

				tc.subscription.Path = cfg.SubPath(server.URL)
			}

			c := New([]cfg.Group{}, userAgentDefault, retriesDefault, maxConcurrentDefault, tmpDir)
			result := make(chan fetchResult)

			go c.fetchSubscription("test-group", &tc.subscription, result)

			select {
			case res := <-result:
				if (res.error != nil) != tc.wantErr {
					t.Errorf("fetchSubscription() error = %v, wantErr %v", res.error, tc.wantErr)
				}
				if !tc.wantErr && len(res.urls) == 0 {
					t.Error("fetchSubscription() returned empty urls")
				}
				if !slices.Equal(res.urls, tc.expected) {
					t.Errorf("fetchSubscription() = %q, want %q", res.urls, tc.expected)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("timeout waiting for fetchSubscription")
			}
		})
	}
}

// TestCrawler_Shutdown tests the shutdown functionality
func TestCrawler_Shutdown(t *testing.T) {
	serverResponded := make(chan struct{})
	onceFunc := sync.OnceFunc(func() {
		close(serverResponded)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		if _, err := w.Write([]byte("line1\nline2")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
		onceFunc()
	}))
	defer server.Close()

	group := cfg.Group{
		Name: "test",
		Subscriptions: []cfg.Subscription{
			{
				Name:    "sub1",
				Path:    cfg.SubPath(server.URL),
				Timeout: cfg.Duration(time.Second),
			},
		},
		Period: cfg.Duration(50 * time.Millisecond),
	}

	c := New([]cfg.Group{group}, userAgentDefault, retriesDefault, maxConcurrentDefault, "")
	c.Run()

	<-serverResponded
	time.Sleep(70 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		c.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		t.Log("shutdown completed")
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete in time")
	}
}

func TestReadSubscription(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		encoded     bool
		wantUrls    []string
		wantBytes   int64
		wantErr     bool
		errContains string
	}{
		{
			name:      "one",
			input:     "https://example.com",
			wantUrls:  []string{"https://example.com"},
			wantBytes: 19,
		},
		{
			name:      "multiple urls",
			input:     "https://example1.com\nhttps://example2.com\nhttps://example3.com\n",
			wantUrls:  []string{"https://example1.com", "https://example2.com", "https://example3.com"},
			wantBytes: 63,
		},
		{
			name:      "multiple urls with windows line endings",
			input:     "https://example1.com\r\nhttps://example2.com\r\nhttps://example3.com",
			wantUrls:  []string{"https://example1.com", "https://example2.com", "https://example3.com"},
			wantBytes: 64,
		},
		{
			name:      "simple encoded",
			input:     base64.StdEncoding.EncodeToString([]byte("https://example.com")),
			encoded:   true,
			wantUrls:  []string{"https://example.com"},
			wantBytes: 19,
		},
		{
			name: "multiple urls encoded",
			input: base64.StdEncoding.EncodeToString([]byte("https://example1.com\n" +
				"https://example2.com\n" +
				"https://example3.com")),
			encoded:   true,
			wantUrls:  []string{"https://example1.com", "https://example2.com", "https://example3.com"},
			wantBytes: 62,
		},

		{
			name:        "invalid base64 input",
			input:       "invalid base64!@#$",
			encoded:     true,
			wantErr:     true,
			errContains: "read encoded response error",
		},
		{
			name:  "empty input",
			input: "",
		},
		{
			name:    "empty encoded input",
			input:   base64.StdEncoding.EncodeToString([]byte("")),
			encoded: true,
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.input)
			gotUrls, gotBytes, err := readSubscription(reader, tc.encoded)

			if err != nil {
				if !tc.wantErr {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if e := err.Error(); tc.errContains != "" && !strings.Contains(e, tc.errContains) {
					t.Errorf("error = %v, want error containing %v", e, tc.errContains)
				}
				return
			}

			if !slices.Equal(gotUrls, tc.wantUrls) {
				t.Errorf("gotUrls = %q, want %q", gotUrls, tc.wantUrls)
			}

			if gotBytes != tc.wantBytes {
				t.Errorf("gotBytes = %v, want %v", gotBytes, tc.wantBytes)
			}
		})
	}
}

// Benchmarks
func BenchmarkCrawler_fetchGroup(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("line1\nline2\nline3")); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	group := cfg.Group{
		Name: "bench",
		Subscriptions: []cfg.Subscription{
			{
				Name:    "sub1",
				Path:    cfg.SubPath(server.URL),
				Timeout: cfg.Duration(time.Second),
			},
			{
				Name:    "sub2",
				Path:    cfg.SubPath(server.URL),
				Timeout: cfg.Duration(time.Second),
			},
		},
		Period: cfg.Duration(time.Second),
	}

	c := New([]cfg.Group{group}, userAgentDefault, retriesDefault, maxConcurrentDefault, "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.fetchGroup(&group)
	}
}

func TestDecodeGroup(t *testing.T) {
	tests := []struct {
		name        string
		groupResult []byte
		resultSize  int
		groupName   string
		want        []byte
		wantErr     bool
		expectedErr error
	}{
		{
			name:        "valid base64 decode",
			groupResult: []byte(base64.StdEncoding.EncodeToString([]byte("line1\nline2"))),
			resultSize:  len(base64.StdEncoding.EncodeToString([]byte("line1\nline2"))),
			groupName:   "test-group",
			want:        []byte("line1\nline2"),
		},
		{
			name:        "invalid base64 decode",
			groupResult: []byte("invalid-base64!@#$"),
			resultSize:  len("invalid-base64!@#$"),
			groupName:   "test-group",
			want:        nil,
			wantErr:     true,
			expectedErr: ErrGroupDecode,
		},
		{
			name:        "empty input",
			groupResult: []byte{},
			resultSize:  0,
			groupName:   "test-group",
			want:        []byte{},
		},
		{
			name:        "valid multi-line decode",
			groupResult: []byte(base64.StdEncoding.EncodeToString([]byte("https://example1.com\nhttps://example2.com\nhttps://example3.com"))),
			resultSize:  len(base64.StdEncoding.EncodeToString([]byte("https://example1.com\nhttps://example2.com\nhttps://example3.com"))),
			groupName:   "multi-group",
			want:        []byte("https://example1.com\nhttps://example2.com\nhttps://example3.com"),
		},
	}

	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeGroup(tc.groupResult, tc.resultSize, tc.groupName)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error but got none")
					return
				}
				if tc.expectedErr != nil && !errors.Is(err, tc.expectedErr) {
					t.Errorf("expected error %v, got %v", tc.expectedErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("decodeGroup() = %q, want %q", got, tc.want)
			}
		})
	}
}
