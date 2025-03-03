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

const (
	// minPeriod is a minimal period value of subscriptions' group refresh.
	minPeriod = Duration(time.Second)
	// minTimeout is a minimal timeout value of subscription refresh.
	minTimeout = Duration(10 * time.Millisecond)
)

var (
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

	for i := range count - 1 {
		b.WriteString(fmt.Sprintf("'%s'", prefixes[i]))
		b.WriteString(", ")
	}

	b.WriteString(fmt.Sprintf("'%s'", prefixes[count-1]))
	b.WriteString("]")

	return slog.StringValue(b.String())
}

// Match checks if the subscription proxy SubPath has prefixes.
func (prefixes Prefixes) Match(subURL string) bool {
	for prefix := range slices.Values(prefixes) {
		if strings.HasPrefix(subURL, prefix) {
			return true
		}
	}

	return false
}

// SubPath is a subscription Path or file path string type.
type SubPath string

// String returns a string representation of the SubPath type.
func (su SubPath) String() string {
	return string(su)
}

// LogValue returns a slog.Value to implement slog.LogValuer interface.
func (su SubPath) LogValue() slog.Value {
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
	Path        SubPath  `json:"url"`
	Encoded     bool     `json:"encoded"`
	Timeout     Duration `json:"timeout"`
	HasPrefixes Prefixes `json:"has_prefixes"`
	Local       bool     `json:"local"`
}

// Validate checks the subscription for correctness.
func (s *Subscription) Validate(dockerVolume string) error {
	if s.Name == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("subscription name is empty"))
	}

	if s.Path == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("subscription path is empty"))
	}

	if s.Timeout < minTimeout {
		return errors.Join(ErrDenyInterval, fmt.Errorf("timeout is too short, should be at least %v", minTimeout))
	}

	if s.Local && dockerVolume == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("docker volume is empty for local subscription %q", s.Name))
	}

	if s.Local {
		fileName, err := validateFilePath(dockerVolume, string(s.Path))
		if err != nil {
			return errors.Join(ErrParse, fmt.Errorf("file path is invalid: %w", err))
		}
		s.Path = SubPath(fileName)
	} else {
		_, err := url.Parse(s.Path.String())
		if err != nil {
			return errors.Join(ErrParse, fmt.Errorf("URL is invalid: %w", err))
		}
	}

	return nil
}

// filterIter returns an iterator for filtering subscription URLs by prefixes.
func (s *Subscription) filterIter(subURLs iter.Seq[string]) iter.Seq[string] {
	return func(yield func(string) bool) {
		for subURL := range subURLs {
			if s.HasPrefixes.Match(subURL) && !yield(subURL) {
				return
			}
		}
	}
}

// Filter returns a list of URLs filtered by prefixes.
func (s *Subscription) Filter(subURLs []string) []string {
	if len(subURLs) == 0 || len(s.HasPrefixes) == 0 {
		return subURLs
	}

	return slices.Collect(s.filterIter(slices.Values(subURLs)))
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
func (g *Group) Validate(dockerVolume string) error {
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
		if err := sub.Validate(dockerVolume); err != nil {
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
	Host         string   `json:"host"`
	Port         uint16   `json:"port"`
	UserAgent    string   `json:"user_agent"`
	Timeout      Duration `json:"timeout"`
	DockerVolume string   `json:"docker_volume"`
	Retries      uint8    `json:"retries"`
	Debug        bool     `json:"debug"`
	Groups       []Group  `json:"groups"`
}

// Validate checks the configuration for correctness.
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("host is empty"))
	}

	if c.Port == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("port is empty"))
	}

	if c.Timeout == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("timeout is empty"))
	}

	if c.UserAgent == "" {
		return errors.Join(ErrRequiredField, fmt.Errorf("user agent is empty"))
	}

	if c.Retries == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("retries is empty"))
	}

	n := len(c.Groups)
	if n == 0 {
		return errors.Join(ErrRequiredField, fmt.Errorf("no groups defined"))
	}

	endpoints := make(map[string]struct{}, n)
	names := make(map[string]struct{}, n)

	for i, group := range c.Groups {
		if err := group.Validate(c.DockerVolume); err != nil {
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
	const dockerConfigDir = "/data"
	currentDir, err := os.Getwd()

	if err != nil {
		return nil, fmt.Errorf("get current dir: %w", err)
	}

	cleanPath := filepath.Clean(strings.Trim(filename, " "))
	isDocker := strings.HasPrefix(cleanPath, dockerConfigDir)
	isCurrent := strings.HasPrefix(cleanPath, currentDir)
	isTemp := strings.HasPrefix(cleanPath, os.TempDir())

	if filepath.IsAbs(cleanPath) {
		if !(isTemp || isDocker || isCurrent) {
			return nil, fmt.Errorf("file %q has an absolute path and is not in the allowed directories", cleanPath)
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

	config := new(Config)
	if err = json.Unmarshal(jsonData, config); err != nil {
		return nil, errors.Join(ErrParse, fmt.Errorf("unmarshal config: %w", err))
	}

	if err = config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// validateFilePath checks if the file path is valid and safe.
// It returns the cleaned file path or an error.
func validateFilePath(dockerVolume, fileName string) (string, error) {
	if fileName == "" {
		return "", errors.New("file name is empty")
	}

	cleanPath := filepath.Clean(strings.Trim(fileName, " "))

	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("file %q has relative path", cleanPath)
	}

	// check cleanPath exists and it's a regular file
	fileInfo, err := os.Stat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("get file %q info: %w", cleanPath, err)
	}

	fileMode := fileInfo.Mode()
	if !fileMode.IsRegular() {
		return "", fmt.Errorf("file %q is not a regular file, mode=%v", cleanPath, fileMode)
	}

	tmpDir := os.TempDir()
	if !(strings.HasPrefix(cleanPath, dockerVolume) || strings.HasPrefix(cleanPath, tmpDir)) {
		return "", fmt.Errorf("file %q has invalid path", cleanPath)
	}

	return cleanPath, nil
}
