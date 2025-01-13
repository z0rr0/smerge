package cfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Duration is a wrapper around time.Duration that supports unmarshalling from a JSON string.
type Duration time.Duration

var (
	// minPeriod is a minimal period value of subscriptions' group refresh.
	minPeriod = Duration(time.Second)

	// minTimeout is a minimal timeout value of subscription refresh.
	minTimeout = Duration(10 * time.Millisecond)

	// ErrRequiredField is an error for required field.
	ErrRequiredField = errors.New("required field is empty")
	// ErrDenyInterval is an error for deny interval, too short or too long.
	ErrDenyInterval = errors.New("deny interval")
	// ErrDuplicate is an error for duplicated value.
	ErrDuplicate = errors.New("duplicate error")
	// ErrParse is an error for parsing error.
	ErrParse = errors.New("parse error")
)

// UnmarshalJSON parses a JSON string into a Duration type.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Join(ErrParse, fmt.Errorf("failed to unmarshal duration [%s]: %w", b, err))
	}

	duration, err := time.ParseDuration(s)
	if err != nil {
		return errors.Join(ErrParse, fmt.Errorf("failed to parse duration [%s]: %w", s, err))
	}

	*d = Duration(duration)
	return nil
}

// MarshalJSON returns a JSON representation of the Duration type.
func (d *Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// Timed returns a time.Duration value of the Duration type.
func (d *Duration) Timed() time.Duration {
	return time.Duration(*d)
}

// String returns a string representation of the Duration type.
func (d *Duration) String() string {
	return d.Timed().String()
}

// Prefixes is a list of prefixes for filtering subscription's values.
type Prefixes []string

// LogValue returns a slog.Value to implement slog.LogValuer interface.
func (prefixes Prefixes) LogValue() slog.Value {
	var count = len(prefixes)

	if count == 0 {
		return slog.StringValue("[]")
	}

	var b strings.Builder
	b.WriteString("[")

	for i := 0; i < count-1; i++ {
		b.WriteString(fmt.Sprintf("'%s'", prefixes[i]))
		b.WriteString(", ")
	}

	b.WriteString(fmt.Sprintf("'%s'", prefixes[count-1]))
	b.WriteString("]")

	return slog.StringValue(b.String())
}

// Match checks if the subscription proxy URL has prefixes.
func (prefixes Prefixes) Match(subURL string) bool {
	for prefix := range slices.Values(prefixes) {
		if strings.HasPrefix(subURL, prefix) {
			return true
		}
	}

	return false
}

// URL is a subscription URL string type.
type URL string

// String returns a string representation of the URL type.
func (su URL) String() string {
	return string(su)
}

// LogValue returns a slog.Value to implement slog.LogValuer interface.
func (su URL) LogValue() slog.Value {
	const maxLen = 32
	var (
		value = string(su)
		n     = len(value)
	)

	if n > maxLen {
		value = value[:maxLen] + "..."
	} else {
		n = max(n-3, 0)
		value = value[:n] + "..."
	}

	return slog.StringValue(value)
}

// Subscription represents a subscription data.
type Subscription struct {
	Name        string   `json:"name"`
	URL         URL      `json:"url"`
	Encoded     bool     `json:"encoded"`
	Timeout     Duration `json:"timeout"`
	HasPrefixes Prefixes `json:"has_prefixes"`
}

// Validate checks the subscription for correctness.
func (s *Subscription) Validate() error {
	if s.Name == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("subscription name is empty"))
	}

	if s.URL == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("subscription URL is empty"))
	}

	if s.Timeout < minTimeout {
		return errors.Join(ErrDenyInterval, fmt.Errorf("timeout is too short, should be at least %v", minTimeout))
	}

	_, err := url.Parse(s.URL.String())
	if err != nil {
		return errors.Join(ErrParse, fmt.Errorf("URL is invalid: %w", err))
	}

	return nil
}

// filterIter returns an iterator for filtering subscription URLs by prefixes.
func (s *Subscription) filterIter(subURLs []string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for subURL := range slices.Values(subURLs) {
			if s.HasPrefixes.Match(subURL) && !yield(subURL) {
				return
			}
		}
	}
}

// Filter returns a list of URLs filtered by prefixes.
func (s *Subscription) Filter(subURLs []string) []string {
	var valuesLen = len(subURLs)

	if valuesLen == 0 || len(s.HasPrefixes) == 0 {
		return subURLs
	}

	urls := make([]string, 0, valuesLen)
	for subURL := range s.filterIter(subURLs) {
		urls = append(urls, subURL)
	}

	return urls
}

// Group is a collection of subscriptions.
type Group struct {
	Name          string         `json:"name"`
	Endpoint      string         `json:"endpoint"`
	Encoded       bool           `json:"encoded"`
	Period        Duration       `json:"period"`
	Subscriptions []Subscription `json:"subscriptions"`
}

// Validate checks the group for correctness.
func (g *Group) Validate() error {
	if g.Name == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("group name is empty"))
	}

	if g.Period < minPeriod {
		return errors.Join(ErrDenyInterval, fmt.Errorf("period is too short, should be at least %v", minPeriod))
	}

	n := len(g.Subscriptions)
	if n == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("group %q has no subscriptions", g.Name))
	}

	subscriptions := make(map[string]struct{}, n)

	for i, sub := range g.Subscriptions {
		if err := sub.Validate(); err != nil {
			return err
		}

		if _, ok := subscriptions[sub.Name]; ok {
			return errors.Join(ErrDuplicate, fmt.Errorf("subscription [%d] %q is duplicated", i, sub.Name))
		}
		subscriptions[sub.Name] = struct{}{}
	}

	return nil
}

// MaxSubscriptionTimeout returns the maximum timeout of all subscriptions in the group.
func (g *Group) MaxSubscriptionTimeout() time.Duration {
	var maxTimeout time.Duration

	for _, sub := range g.Subscriptions {
		maxTimeout = max(maxTimeout, sub.Timeout.Timed())
	}

	return maxTimeout
}

// Config is a main configuration structure.
type Config struct {
	Host      string   `json:"host"`
	Port      uint16   `json:"port"`
	UserAgent string   `json:"user_agent"`
	Timeout   Duration `json:"timeout"`
	Debug     bool     `json:"debug"`
	Groups    []Group  `json:"groups"`
}

// Validate checks the configuration for correctness.
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("host is empty"))
	}

	if c.Port == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("port is empty"))
	}

	n := len(c.Groups)
	if n == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("no groups defined"))
	}

	endpoints := make(map[string]struct{}, n)
	names := make(map[string]struct{}, n)

	for i, group := range c.Groups {
		if err := group.Validate(); err != nil {
			return err
		}

		if _, ok := endpoints[group.Endpoint]; ok {
			return errors.Join(ErrDuplicate, fmt.Errorf("endpoint [%d] %q is duplicated", i, group.Endpoint))
		}

		if _, ok := names[group.Name]; ok {
			return errors.Join(ErrDuplicate, fmt.Errorf("group name [%d] %q is duplicated", i, group.Name))
		}

		endpoints[group.Endpoint] = struct{}{}
		names[group.Name] = struct{}{}
	}

	return nil
}

// Addr returns service's net address.
func (c *Config) Addr() string {
	return net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
}

// GroupsEndpoints returns a map of groups by their endpoints.
func (c *Config) GroupsEndpoints() map[string]*Group {
	var groups = make(map[string]*Group, len(c.Groups))

	for i := range c.Groups {
		endpoint := url.QueryEscape(strings.Trim(c.Groups[i].Endpoint, "/ "))
		groups[endpoint] = &c.Groups[i]
	}

	return groups
}

// readConfig reads a configuration file from the filesystem.
func readConfig(filename string) ([]byte, error) {
	const dockerDir = "/data"
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get current dir: %w", err)
	}

	cleanPath := filepath.Clean(strings.Trim(filename, " "))
	isDocker := strings.HasPrefix(cleanPath, dockerDir)
	isCurrent := strings.HasPrefix(cleanPath, currentDir)
	isTemp := strings.HasPrefix(cleanPath, os.TempDir())

	if filepath.IsAbs(cleanPath) {
		if !(isTemp || isDocker || isCurrent) {
			return nil, fmt.Errorf("file %q has abusolute path and not in the allowed directories", cleanPath)
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
		return nil, errors.Join(ErrParse, fmt.Errorf("unmarshal config: %w", err))
	}

	if err = config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}
