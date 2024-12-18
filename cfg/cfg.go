package cfg

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Duration is a wrapper around time.Duration that supports unmarshalling from a JSON string.
type Duration time.Duration

var (
	// minPeriod is a minimal period value of subscriptions' group refresh.
	minPeriod = Duration(time.Minute)

	// minTimeout is a minimal timeout value of subscription refresh.
	minTimeout = Duration(10 * time.Millisecond)
)

// UnmarshalJSON parses a JSON string into a Duration type.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration should be a string, got %s", b)
	}

	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration %s: %v", s, err)
	}

	*d = Duration(duration)
	return nil
}

// Timed returns a time.Duration value of the Duration type.
func (d *Duration) Timed() time.Duration {
	return time.Duration(*d)
}

// Subscription represents a subscription data.
type Subscription struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Encoded bool     `json:"encoded"`
	Timeout Duration `json:"timeout"`
}

// validate checks the subscription for correctness.
func (s *Subscription) validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is empty")
	}

	if s.URL == "" {
		return fmt.Errorf("URL is empty")
	}

	if s.Timeout < minTimeout {
		return fmt.Errorf("timeout is too short, should be at least %v", minTimeout)
	}

	_, err := url.Parse(s.URL)
	if err != nil {
		return fmt.Errorf("URL is invalid: %w", err)
	}

	return nil
}

// Group is a collection of subscriptions.
type Group struct {
	Name          string         `json:"name"`
	Endpoint      string         `json:"endpoint"`
	Encoded       bool           `json:"encoded"`
	Period        Duration       `json:"period"`
	Subscriptions []Subscription `json:"subscriptions"`
}

// validate checks the group for correctness.
func (g *Group) validate() error {
	if g.Period < minPeriod {
		return fmt.Errorf("period is too short, should be at least %v", minPeriod)
	}

	var subscriptions = make(map[string]struct{}, len(g.Subscriptions))

	for i, sub := range g.Subscriptions {
		if err := sub.validate(); err != nil {
			return fmt.Errorf("subscription [%d] %q: %w", i, sub.Name, err)
		}

		if _, ok := subscriptions[sub.Name]; ok {
			return fmt.Errorf("subscription [%d] %q is duplicated", i, sub.Name)
		}
		subscriptions[sub.Name] = struct{}{}
	}

	return nil
}

// Config is a main configuration structure.
type Config struct {
	Host    string   `json:"host"`
	Port    uint16   `json:"port"`
	Timeout Duration `json:"timeout"`
	Debug   bool     `json:"debug"`
	Groups  []Group  `json:"groups"`
}

// validate checks the configuration for correctness.
func (c *Config) validate() error {
	var endpoints = make(map[string]struct{}, len(c.Groups))

	for i, group := range c.Groups {
		if err := group.validate(); err != nil {
			return fmt.Errorf("group [%d] %q: %w", i, group.Name, err)
		}

		if _, ok := endpoints[group.Endpoint]; ok {
			return fmt.Errorf("endpoint %q is duplicated", group.Endpoint)
		}
		endpoints[group.Endpoint] = struct{}{}
	}

	return nil
}

// Addr returns service's net address.
func (c *Config) Addr() string {
	return net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
}

// GroupsMap returns a map of groups by their endpoints.
func (c *Config) GroupsMap() map[string]*Group {
	var groups = make(map[string]*Group, len(c.Groups))

	for i := range c.Groups {
		endpoint := url.QueryEscape(strings.Trim(c.Groups[i].Endpoint, "/ "))
		groups[endpoint] = &c.Groups[i]
	}

	return groups
}

// readConfig reads a configuration file from the filesystem.
func readConfig(filename string) ([]byte, error) {
	const dockerDir = "/data/conf"
	var testConfig = filepath.Join(os.TempDir(), "smerge.json")

	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get current dir: %w", err)
	}

	cleanPath := filepath.Clean(strings.Trim(filename, " "))
	isDocker := strings.HasPrefix(cleanPath, dockerDir)
	isCurrent := strings.HasPrefix(cleanPath, currentDir)

	if filepath.IsAbs(cleanPath) {
		if !(cleanPath == testConfig || isDocker || isCurrent) {
			return nil, fmt.Errorf("file %q has relative path and not in the allowed directories", cleanPath)
		}
	} else {
		cleanPath = filepath.Join(currentDir, cleanPath)
	}

	return os.ReadFile(cleanPath)
}

// New creates a new configuration from a file.
func New(filename string) (*Config, error) {
	jsonData, err := readConfig(filename)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config = new(Config)
	if err = json.Unmarshal(jsonData, config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err = config.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return config, nil
}
