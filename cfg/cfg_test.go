package cfg

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const configContent = `
{
  "host": "localhost",
  "port": 43210,
  "timeout": "10s",
  "workers": 1,
  "user_agent": "SMerge/1.0",
  "retries": 3,
  "limiter": {
    "max_concurrent": 10,
    "rate": 1.0,
    "burst": 2.0,
    "exclude": ["127.0.0.1"]
  },
  "debug": true,
  "groups": [
    {
      "name": "group1",
      "endpoint": "/group1",
      "encoded": true,
      "period": "12h",
      "subscriptions": [
        {
          "name": "subscription1",
          "url": "http://localhost:43211/subscription1",
          "encoded": false,
          "timeout": "10s"
        },
        {
          "name": "subscription2",
          "url": "http://localhost:43212/subscription2",
          "encoded": true,
          "timeout": "10s"
        }
      ]
    }
  ]
}
`

func createConfigFile(name string) (string, error) {
	fullPath := filepath.Join(os.TempDir(), name)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return "", err
	}

	if _, err = f.Write([]byte(configContent)); err != nil {
		return "", err
	}

	if err = f.Close(); err != nil {
		return "", err
	}
	return fullPath, err
}

func TestNew(t *testing.T) {
	name, err := createConfigFile("smerge_test_new.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = New("/bad_file_path.json"); err == nil {
		t.Error("unexpected behavior")
	}

	cfg, err := New(name)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Addr() != "localhost:43210" {
		t.Error("unexpected address")
	}
}

func TestSubscriptionValidate(t *testing.T) {
	tmpDir := os.TempDir()
	localFile, fileErr := os.Create(filepath.Join(tmpDir, "local.txt"))
	if fileErr != nil {
		t.Fatal(fileErr)
	}

	if fileErr = localFile.Close(); fileErr != nil {
		t.Fatal(fileErr)
	}

	testCases := []struct {
		name      string
		sub       Subscription
		dockerDir string
		err       error  // if nil - no error expected
		errMsg    string // a part of error message if error expected
	}{
		{
			name:      "empty name",
			sub:       Subscription{Path: "http://localhost:43211/subscription1"},
			dockerDir: tmpDir,
			err:       ErrRequiredField,
			errMsg:    "subscription name is empty",
		},
		{
			name:      "empty SubPath",
			sub:       Subscription{Name: "subscription1"},
			dockerDir: tmpDir,
			err:       ErrRequiredField,
			errMsg:    "subscription path is empty",
		},
		{
			name: "too short timeout",
			sub: Subscription{
				Name:    "subscription1",
				Path:    "http://localhost:43211/subscription1",
				Timeout: Duration(1),
			},
			dockerDir: tmpDir,
			err:       ErrDenyInterval,
			errMsg:    "timeout is too short, should be at least",
		},
		{
			name: "invalid SubPath",
			sub: Subscription{
				Name:    "subscription1",
				Path:    "https://%.com",
				Timeout: Duration(time.Second),
			},
			dockerDir: tmpDir,
			err:       ErrParse,
			errMsg:    "URL is invalid",
		},
		{
			name: "invalid local SubPath",
			sub: Subscription{
				Name:    "subscription1",
				Path:    SubPath("/etc/passwd"),
				Timeout: Duration(time.Second),
				Local:   true,
			},
			dockerDir: tmpDir,
			err:       ErrParse,
			errMsg:    "file path is invalid",
		},
		{
			name: "local path without dockerDir",
			sub: Subscription{
				Name:    "subscription1",
				Path:    SubPath(localFile.Name()),
				Timeout: Duration(time.Second),
				Local:   true,
			},
			err:    ErrRequiredField,
			errMsg: "docker volume is empty for local subscription",
		},
		{
			name: "valid local SubPath",
			sub: Subscription{
				Name:    "subscription1",
				Path:    SubPath(localFile.Name()),
				Timeout: Duration(time.Second),
				Local:   true,
			},
			dockerDir: tmpDir,
		},
		{
			name: "valid",
			sub: Subscription{
				Name:    "subscription1",
				Path:    "http://localhost:43211/subscription1",
				Timeout: Duration(time.Second),
			},
			dockerDir: tmpDir,
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			err := tc.sub.Validate(tc.dockerDir)
			if tc.err == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("error expected: %v", tc.err)
				return
			}

			if !errors.Is(err, tc.err) {
				t.Errorf("unexpected error type: %v", err)
				return
			}

			if errMsg := err.Error(); !strings.Contains(errMsg, tc.errMsg) {
				t.Errorf("unexpected error message: %q", errMsg)
			}
		})
	}
}

func TestGroupValidate(t *testing.T) {
	const sec = Duration(time.Second)
	var tmpDir = os.TempDir()

	localFile, fileErr := os.Create(filepath.Join(tmpDir, "local.txt"))
	if fileErr != nil {
		t.Fatal(fileErr)
	}

	if fileErr = localFile.Close(); fileErr != nil {
		t.Fatal(fileErr)
	}

	testCases := []struct {
		name   string
		group  Group
		err    error  // if nil - no error expected
		errMsg string // a part of error message if error expected
	}{
		{
			name:   "empty name",
			group:  Group{Period: Duration(time.Hour)},
			err:    ErrRequiredField,
			errMsg: "group name is empty",
		},
		{
			name: "too short period",
			group: Group{
				Name:   "group1",
				Period: Duration(time.Millisecond),
			},
			err:    ErrDenyInterval,
			errMsg: "period is too short, should be at least",
		},
		{
			name: "no subscriptions",
			group: Group{
				Name:   "group1",
				Period: Duration(time.Hour),
			},
			err:    ErrRequiredField,
			errMsg: "group \"group1\" has no subscriptions",
		},
		{
			name: "invalid subscription",
			group: Group{
				Name:          "group1",
				Period:        Duration(time.Hour),
				Subscriptions: []Subscription{{}},
			},
			err:    ErrRequiredField,
			errMsg: "subscription name is empty",
		},
		{
			name: "duplicate subscription",
			group: Group{
				Name:   "group1",
				Period: Duration(time.Hour),
				Subscriptions: []Subscription{
					{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: sec},
					{Name: "subscription1", Path: "http://localhost:43211/sub2", Timeout: sec},
					{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: sec},
				},
			},
			err:    ErrDuplicate,
			errMsg: "subscription [1] \"subscription1\" is duplicated",
		},
		{
			name: "valid",
			group: Group{
				Name:   "group1",
				Period: Duration(time.Hour),
				Subscriptions: []Subscription{
					{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: sec},
					{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: sec},
					{
						Name:    "subscription3",
						Path:    SubPath(localFile.Name()),
						Timeout: sec,
						Local:   true,
					},
				},
			},
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			err := tc.group.Validate(tmpDir)
			if tc.err == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("error expected: %v", tc.err)
				return
			}

			if !errors.Is(err, tc.err) {
				t.Errorf("unexpected error type: %v", err)
				return
			}

			if errMsg := err.Error(); !strings.Contains(errMsg, tc.errMsg) {
				t.Errorf("unexpected error message: %q", errMsg)
			}
		})
	}
}

func TestGroupMaxSubscriptionTimeout(t *testing.T) {
	testCases := []struct {
		name     string
		group    Group
		expected time.Duration
	}{
		{
			name: "empty",
		},
		{
			name: "single subscription",
			group: Group{
				Subscriptions: []Subscription{
					{Timeout: Duration(time.Second)},
				},
			},
			expected: time.Second,
		},
		{
			name: "multiple subscriptions",
			group: Group{
				Subscriptions: []Subscription{
					{Timeout: Duration(time.Millisecond * 100)},
					{Timeout: Duration(time.Millisecond * 300)},
					{Timeout: Duration(time.Millisecond * 200)},
				},
			},
			expected: time.Millisecond * 300,
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			if timeout := tc.group.MaxSubscriptionTimeout(); timeout != tc.expected {
				t.Errorf("unexpected timeout, got=%v, but expected=%v", timeout, tc.expected)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	timeout := Duration(time.Second)
	userAgent := "test"
	limiter := LimitOptions{
		MaxConcurrent: 1,
		Rate:          1.0,
		Burst:         1.0,
	}

	testCases := []struct {
		name   string
		config Config
		err    error  // if nil - no error expected
		errMsg string // a part of error message if error expected
	}{
		{
			name:   "empty host",
			config: Config{},
			err:    ErrRequiredField,
			errMsg: "host is empty",
		},
		{
			name:   "empty port",
			config: Config{Host: "localhost"},
			err:    ErrRequiredField,
			errMsg: "port is empty",
		},
		{
			name:   "invalid timeout",
			config: Config{Host: "localhost", Port: 43210, UserAgent: userAgent, Retries: 3, Limiter: limiter},
			err:    ErrRequiredField,
			errMsg: "timeout is empty",
		},
		{
			name:   "invalid user agent",
			config: Config{Host: "localhost", Port: 43210, Timeout: timeout, Retries: 3, Limiter: limiter},
			err:    ErrRequiredField,
			errMsg: "user agent is empty",
		},
		{
			name:   "no retries",
			config: Config{Host: "localhost", Port: 43210, Timeout: timeout, UserAgent: userAgent, Limiter: limiter},
			err:    ErrRequiredField,
			errMsg: "retries is empty",
		},
		{
			name:   "invalid max concurrent",
			config: Config{Host: "localhost", Port: 43210, Timeout: timeout, UserAgent: userAgent, Retries: 3},
			err:    ErrRequiredField,
			errMsg: "max concurrent should be at least 1",
		},
		{
			name: "no groups",
			config: Config{
				Host:      "localhost",
				Port:      43210,
				Timeout:   timeout,
				UserAgent: userAgent,
				Retries:   3,
				Limiter:   limiter,
			},
			err:    ErrRequiredField,
			errMsg: "no groups defined",
		},
		{
			name: "invalid group",
			config: Config{
				Host:      "localhost",
				Port:      43210,
				Timeout:   timeout,
				UserAgent: userAgent,
				Retries:   3,
				Limiter:   limiter,
				Groups: []Group{
					{Period: Duration(time.Hour)},
				},
			},
			err:    ErrRequiredField,
			errMsg: "group name is empty",
		},
		{
			name: "duplicate group",
			config: Config{
				Host:      "localhost",
				Port:      43210,
				Timeout:   timeout,
				UserAgent: userAgent,
				Retries:   3,
				Limiter:   limiter,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group1",
						Endpoint: "/group2",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
						},
					},
				},
			},
			err:    ErrDuplicate,
			errMsg: "group name [1] \"group1\" is duplicated",
		},
		{
			name: "duplicate endpoint",
			config: Config{
				Host:      "localhost",
				Port:      43210,
				Timeout:   timeout,
				UserAgent: userAgent,
				Retries:   3,
				Limiter:   limiter,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group2",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
						},
					},
				},
			},
			err:    ErrDuplicate,
			errMsg: "endpoint [1] \"/group1\" is duplicated",
		},
		{
			name: "valid",
			config: Config{
				Host:      "localhost",
				Port:      43210,
				Timeout:   timeout,
				UserAgent: userAgent,
				Retries:   3,
				Limiter:   limiter,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group2",
						Endpoint: "/group2",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
						},
					},
				},
			},
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.err == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("error expected: %v", tc.err)
				return
			}

			if !errors.Is(err, tc.err) {
				t.Errorf("unexpected error type: %v", err)
				return
			}

			if errMsg := err.Error(); !strings.Contains(errMsg, tc.errMsg) {
				t.Errorf("unexpected error message: %q", errMsg)
			}
		})
	}
}

func TestConfigGroupsEndpointsMap(t *testing.T) {
	groups := []Group{
		{
			Name:     "group1",
			Endpoint: "/group1",
			Period:   Duration(time.Hour),
			Subscriptions: []Subscription{
				{Name: "subscription1", Path: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
			},
		},
		{
			Name:     "group2",
			Endpoint: "/group2",
			Period:   Duration(time.Hour),
			Subscriptions: []Subscription{
				{Name: "subscription2", Path: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
			},
		},
	}

	config := Config{
		Host:   "localhost",
		Port:   43210,
		Groups: groups,
	}
	expected := map[string]*Group{
		"group1": &groups[0],
		"group2": &groups[1],
	}

	if configGroups := config.GroupsEndpoints(); !maps.Equal(configGroups, expected) {
		t.Errorf("unexpected groups map, got=%v, but expected=%v", configGroups, expected)
	}
}

func TestDurationUnmarshalJSON(t *testing.T) {
	testCases := []struct {
		name     string
		str      string
		expected Duration
		err      error
	}{
		{
			name:     "1 hour",
			str:      `"1h"`,
			expected: Duration(time.Hour),
		},
		{
			name:     "1 second",
			str:      `"1s"`,
			expected: Duration(time.Second),
		},
		{
			name: "invalid duration",
			str:  `"1x"`,
			err:  ErrParse,
		},
		{
			name: "empty duration",
			str:  `""`,
			err:  ErrParse,
		},
		{
			name:     "1 hour and 10 second",
			str:      `"1h10s"`,
			expected: Duration(time.Hour + 10*time.Second),
		},
		{
			name:     "1 hour 2 minutes 3 seconds",
			str:      `"1h2m3s"`,
			expected: Duration(time.Hour + 2*time.Minute + 3*time.Second),
		},
		{
			name: "bad json",
			str:  `1h10s"`,
			err:  ErrParse,
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			var d = new(Duration)
			err := d.UnmarshalJSON([]byte(tc.str))

			if tc.err == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if *d != tc.expected {
					t.Errorf("unexpected duration, got=%v, but expected=%v", *d, tc.expected)
				}
				return
			}

			if err == nil {
				t.Errorf("error expected: %v", tc.err)
				return
			}

			if !errors.Is(err, tc.err) {
				t.Errorf("unexpected error type: %v", err)
				return
			}
		})
	}
}

func TestDurationMarshalJSON(t *testing.T) {
	testCases := []struct {
		name     string
		duration Duration
		expected string
	}{
		{
			name:     "1 hour",
			duration: Duration(time.Hour),
			expected: `"1h0m0s"`,
		},
		{
			name:     "1 second",
			duration: Duration(time.Second),
			expected: `"1s"`,
		},
		{
			name:     "1 hour and 10 second",
			duration: Duration(time.Hour + 10*time.Second),
			expected: `"1h0m10s"`,
		},
		{
			name:     "2 hours 3 minutes 4 seconds",
			duration: Duration(2*time.Hour + 3*time.Minute + 4*time.Second),
			expected: `"2h3m4s"`,
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.duration.MarshalJSON()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if s := string(b); s != tc.expected {
				t.Errorf("unexpected JSON, got=%s, but expected=%s", s, tc.expected)
			}
		})
	}
}

func TestPrefixes_LogValue(t *testing.T) {
	testCases := []struct {
		name     string
		prefixes Prefixes
		expected string
	}{
		{
			name:     "empty",
			prefixes: nil,
			expected: "[]",
		},
		{
			name:     "single",
			prefixes: Prefixes{"prefix1"},
			expected: "['prefix1']",
		},
		{
			name:     "multiple",
			prefixes: Prefixes{"prefix1", "prefix2", "prefix3"},
			expected: "['prefix1', 'prefix2', 'prefix3']",
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			s := tc.prefixes.LogValue()

			if v := s.String(); v != tc.expected {
				t.Errorf("unexpected value, got=%q, but expected=%q", v, tc.expected)
			}
		})
	}
}

func TestPrefixes_Match(t *testing.T) {
	testCases := []struct {
		name     string
		prefixes Prefixes
		value    string
		expected bool
	}{
		{
			name: "empty",
		},
		{
			name:     "no match",
			prefixes: Prefixes{"ss://", "vless://"},
			value:    "trojan://url1",
		},
		{
			name:  "no prefixes",
			value: "ss://url1",
		},
		{
			name:     "empty value",
			prefixes: Prefixes{"ss://", "vless://"},
		},
		{
			name:     "single match",
			prefixes: Prefixes{"ss://", "vless://"},
			value:    "ss://url1",
			expected: true,
		},
		{
			name:     "multiple matches",
			prefixes: Prefixes{"v", "vl", "vle", "vless"},
			value:    "vless://url1",
			expected: true,
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			if result := tc.prefixes.Match(tc.value); result != tc.expected {
				t.Errorf("unexpected value, got=%v, but expected=%v", result, tc.expected)
			}
		})
	}
}

func TestSubscription_Filter(t *testing.T) {
	testCases := []struct {
		name     string
		sub      Subscription
		values   []string
		expected []string
	}{
		{
			name: "empty",
			sub:  Subscription{},
		},
		{
			name:     "no prefixes",
			sub:      Subscription{},
			values:   []string{"ss://url1", "ss://url2", "vless://url3"},
			expected: []string{"ss://url1", "ss://url2", "vless://url3"},
		},
		{
			name: "no values",
			sub:  Subscription{HasPrefixes: Prefixes{"ss://", "vless://"}},
		},
		{
			name:   "no match",
			sub:    Subscription{HasPrefixes: Prefixes{"ss://", "vless://"}},
			values: []string{"trojan://url1", "vmess://url2", "https://url3"},
		},
		{
			name:     "single match",
			sub:      Subscription{HasPrefixes: Prefixes{"ss://", "vless://"}},
			values:   []string{"ss://url1", "vmess://url2", "https://url3"},
			expected: []string{"ss://url1"},
		},
		{
			name:     "multiple matches",
			sub:      Subscription{HasPrefixes: Prefixes{"ss://", "vless://"}},
			values:   []string{"ss://url1", "vless://url2", "https://url3"},
			expected: []string{"ss://url1", "vless://url2"},
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			if result := tc.sub.Filter(tc.values); !slices.Equal(result, tc.expected) {
				t.Errorf("unexpected value, got=%v, but expected=%v", result, tc.expected)
			}
		})
	}
}

func TestURL_LogValue(t *testing.T) {
	testCases := []struct {
		name     string
		url      SubPath
		expected string
	}{
		{
			name:     "empty",
			expected: "...",
		},
		{
			name:     "short",
			url:      "https://localhost:43211/sub1",
			expected: "https://localhost:43211/s...",
		},
		{
			name:     "long",
			url:      "https://localhost:43211/subscription1",
			expected: "https://localhost:43211/subscrip...",
		},
		{
			name:     "local",
			url:      "/data/subscriptions/subscription1",
			expected: "/data/subscriptions/subscription...",
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			s := tc.url.LogValue()

			if v := s.String(); v != tc.expected {
				t.Errorf("unexpected value, got=%q, but expected=%q", v, tc.expected)
			}

			if v := tc.url.String(); v != string(tc.url) {
				t.Errorf("unexpected value, got=%q, but expected=%q", v, tc.url)
			}
		})
	}
}
