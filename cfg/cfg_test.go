package cfg

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
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
	testCases := []struct {
		name   string
		sub    Subscription
		err    error  // if nil - no error expected
		errMsg string // a part of error message if error expected
	}{
		{
			name:   "empty name",
			sub:    Subscription{URL: "http://localhost:43211/subscription1"},
			err:    ErrRequiredField,
			errMsg: "subscription name is empty",
		},
		{
			name:   "empty URL",
			sub:    Subscription{Name: "subscription1"},
			err:    ErrRequiredField,
			errMsg: "subscription URL is empty",
		},
		{
			name: "too short timeout",
			sub: Subscription{
				Name:    "subscription1",
				URL:     "http://localhost:43211/subscription1",
				Timeout: Duration(1),
			},
			err:    ErrDenyInterval,
			errMsg: "timeout is too short, should be at least",
		},
		{
			name: "invalid URL",
			sub: Subscription{
				Name:    "subscription1",
				URL:     "https://%.com",
				Timeout: Duration(time.Second),
			},
			err:    ErrParse,
			errMsg: "URL is invalid",
		},
		{
			name: "valid",
			sub: Subscription{
				Name:    "subscription1",
				URL:     "http://localhost:43211/subscription1",
				Timeout: Duration(time.Second),
			},
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			err := tc.sub.Validate()
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
					{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: sec},
					{Name: "subscription1", URL: "http://localhost:43211/sub2", Timeout: sec},
					{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: sec},
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
					{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: sec},
					{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: sec},
				},
			},
		},
	}

	for i := range testCases {
		tc := testCases[i]

		t.Run(tc.name, func(t *testing.T) {
			err := tc.group.Validate()
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
			name:   "no groups",
			config: Config{Host: "localhost", Port: 43210},
			err:    ErrRequiredField,
			errMsg: "no groups defined",
		},
		{
			name: "invalid group",
			config: Config{
				Host: "localhost",
				Port: 43210,
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
				Host: "localhost",
				Port: 43210,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group1",
						Endpoint: "/group2",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
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
				Host: "localhost",
				Port: 43210,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group2",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
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
				Host: "localhost",
				Port: 43210,
				Groups: []Group{
					{
						Name:     "group1",
						Endpoint: "/group1",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
						},
					},
					{
						Name:     "group2",
						Endpoint: "/group2",
						Period:   Duration(time.Hour),
						Subscriptions: []Subscription{
							{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
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
				{Name: "subscription1", URL: "http://localhost:43211/sub1", Timeout: Duration(time.Second)},
			},
		},
		{
			Name:     "group2",
			Endpoint: "/group2",
			Period:   Duration(time.Hour),
			Subscriptions: []Subscription{
				{Name: "subscription2", URL: "http://localhost:43211/sub2", Timeout: Duration(time.Second)},
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
				t.Errorf("unexpected value, got=%s, but expected=%s", v, tc.expected)
			}
		})
	}
}
