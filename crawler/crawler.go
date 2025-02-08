package crawler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

// Getter is an interface for getting data by group name.
// If force is true, the data will be fetched from the source.
// If decode is true, the data will be decoded from base64 if request group has Encoded flag.
type Getter interface {
	Get(groupName string, force bool, decode bool) []byte
}

// Crawler is a main crawler structure.
type Crawler struct {
	sync.RWMutex
	groups     map[string]*cfg.Group
	result     map[string][]byte
	userAgent  string
	client     *http.Client
	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

type fetchResult struct {
	subscription string
	urls         []string
	error        error
}

// New creates a new crawler instance.
func New(groups []cfg.Group, userAgent string) *Crawler {
	const (
		maxConnectionsPerHost = 100
		maxIdleConnections    = 1000
		minHandshakeTimeout   = 500 * time.Millisecond
	)
	var (
		timeout   time.Duration
		groupLen  = len(groups)
		groupsMap = make(map[string]*cfg.Group, groupLen)
	)

	for i, group := range groups {
		groupsMap[group.Name] = &groups[i]
		timeout = max(timeout, group.MaxSubscriptionTimeout())
	}

	handshakeTimeout := max(timeout/2, minHandshakeTimeout)
	slog.Info("timeouts", "timeout", timeout, "handshake", handshakeTimeout)

	ctx, cancel := context.WithCancel(context.Background())

	return &Crawler{
		groups:    groupsMap,
		result:    make(map[string][]byte, groupLen),
		userAgent: userAgent,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				MaxIdleConns:      maxIdleConnections,
				MaxConnsPerHost:   maxConnectionsPerHost,
				IdleConnTimeout:   timeout * 10,
				ForceAttemptHTTP2: true,
				DialContext: (&net.Dialer{
					Timeout:   handshakeTimeout,
					KeepAlive: timeout * 5,
				}).DialContext,
				TLSHandshakeTimeout:   handshakeTimeout,
				ResponseHeaderTimeout: timeout,
			},
			Timeout: timeout * 2,
		},
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

// Run starts the crawler for all groups.
func (c *Crawler) Run() {
	for name := range c.groups {
		c.wg.Add(1)

		go func(group *cfg.Group) {
			period := group.Period.Timed()
			slog.Info("starting group handler", "group", name, "period", period)
			c.fetchGroup(group) // 1st init fetch after start

			ticker := time.NewTicker(period)
			defer func() {
				ticker.Stop()
				c.wg.Done()
			}()

			for {
				select {
				case <-c.ctx.Done():
					slog.Info("group handler stopped", "group", group.Name)
					return
				case <-ticker.C:
					slog.Info("group handler tick", "group", group.Name, "period", period)
					c.fetchGroup(group)
				}
			}

		}(c.groups[name])
	}
}

// Shutdown stops the crawler and waits for all goroutines to finish.
func (c *Crawler) Shutdown() {
	c.cancelFunc()
	c.wg.Wait()
}

// needDecode checks if the group data needs to be decoded.
// A caller should hold the read lock.
func (c *Crawler) needDecode(groupName string, decode bool, resultSize int) bool {
	if !(decode && resultSize != 0) {
		return false
	}

	group, ok := c.groups[groupName]
	return ok && group.Encoded
}

// Get returns the group data.
func (c *Crawler) Get(groupName string, force bool, decode bool) []byte {
	if force {
		c.fetchGroup(c.groups[groupName])
	}

	c.RLock()
	groupResult := c.result[groupName]
	resultSize := len(groupResult)
	decode = c.needDecode(groupName, decode, resultSize)
	c.RUnlock()

	if decode {
		dst := make([]byte, base64.StdEncoding.DecodedLen(resultSize))

		if n, err := base64.StdEncoding.Decode(dst, groupResult); err != nil {
			slog.Error("decode error", "group", groupName, "error", err)
		} else {
			slog.Debug("decoded", "group", groupName, "size", n)
			groupResult = dst[:n]
		}
	}

	return groupResult
}

// fetchGroup fetches all subscriptions for the group.
func (c *Crawler) fetchGroup(group *cfg.Group) {
	const avgSubURLs = 10
	var (
		start            = time.Now()
		result           = make(chan fetchResult, 1)
		subscriptionsLen = len(group.Subscriptions)
		avgURLsLen       = subscriptionsLen * avgSubURLs
	)
	defer close(result)
	slog.Info("fetchGroup", "group", group.Name, "subscriptions", subscriptionsLen)

	for i := range group.Subscriptions {
		go c.fetchSubscription(group.Name, &group.Subscriptions[i], result)
	}

	urls := make([]string, 0, avgURLsLen)
	for i := 0; i < subscriptionsLen; i++ {
		if res := <-result; res.error != nil {
			slog.Error("fetchError", "group", group.Name, "subscription", res.subscription, "error", res.error)
		} else {
			urls = append(urls, res.urls...)
		}
	}
	groupResult := prepareGroupResult(urls, group.Encoded)

	c.Lock()
	c.result[group.Name] = groupResult
	c.Unlock()

	slog.Info(
		"fetched",
		"group", group.Name,
		"urls", len(urls),
		"bytes", len(groupResult),
		"duration", time.Since(start),
	)
}

func (c *Crawler) fetchURLSubscription(ctx context.Context, sub *cfg.Subscription) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.Path.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request error: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("client do error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// close body because a caller skips this step due to an error
		if err = resp.Body.Close(); err != nil {
			slog.Error("response body close error", "subscription", sub.Name, "error", err)
		}
		return nil, fmt.Errorf("response status error: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// fetchLocalSubscription fetches the subscription if sub.Path is a local file.
func (c *Crawler) fetchLocalSubscription(_ context.Context, sub *cfg.Subscription) (io.ReadCloser, error) {
	var fileName = sub.Path.String()

	f, err := os.Open(fileName) // #nosec G304, file name is already validated during configuration parsing
	if err != nil {
		return nil, fmt.Errorf("open file=%q error: %w", fileName, err)
	}

	return f, nil
}

// fetchSubscription fetches the subscription urls.
func (c *Crawler) fetchSubscription(groupName string, sub *cfg.Subscription, result chan<- fetchResult) {
	var (
		fetchRes    = fetchResult{subscription: sub.Name}
		ctx, cancel = context.WithTimeout(c.ctx, sub.Timeout.Timed())
		reader      io.ReadCloser
		err         error
	)
	defer func() {
		result <- fetchRes
		cancel()
	}()

	slog.Debug(
		"fetchSubscription",
		"group", groupName,
		"subscription", sub.Name,
		"local", sub.Local,
		"has_prefixes", sub.HasPrefixes,
		"url", sub.Path,
	)
	start := time.Now()

	if sub.Local {
		reader, err = c.fetchLocalSubscription(ctx, sub)
	} else {
		reader, err = c.fetchURLSubscription(ctx, sub)
	}

	if err != nil {
		fetchRes.error = fmt.Errorf("fetch error: %w", err)
		return
	}

	defer func() {
		if e := reader.Close(); e != nil {
			slog.Error("response body close error", "group", groupName, "subscription", sub.Name, "error", e)
		}
	}()

	urls, n, err := readSubscription(reader, sub.Encoded)
	if err != nil {
		fetchRes.error = fmt.Errorf("read subscription error: %w", err)
		return
	}

	fetchRes.urls = sub.Filter(urls)
	slog.Info("fetched",
		"group", groupName,
		"subscription", sub.Name,
		"encoded", sub.Encoded,
		"size", len(urls),
		"filtered", len(fetchRes.urls),
		"prefixes", len(sub.HasPrefixes),
		"bytes", n,
		"duration", time.Since(start),
	)
}

// readSubscription reads the subscription data from the reader (HTTP response body).
func readSubscription(r io.Reader, encoded bool) ([]string, int64, error) {
	var (
		buf = new(bytes.Buffer)
		n   int64
		err error
	)

	if encoded {
		decoder := base64.NewDecoder(base64.StdEncoding, r)
		if n, err = buf.ReadFrom(decoder); err != nil {
			return nil, 0, fmt.Errorf("read encoded response error: %w", err)
		}
	} else {
		if n, err = io.Copy(buf, r); err != nil {
			return nil, 0, fmt.Errorf("read response error: %w", err)
		}
	}

	// split result ignoring characters https://pkg.go.dev/unicode#IsSpace
	return strings.Fields(buf.String()), n, nil
}

// prepareGroupResult prepares the group result for storing.
func prepareGroupResult(urls []string, encoded bool) []byte {
	const lineSep = "\n"

	sort.Strings(urls)
	groupResult := []byte(strings.Join(urls, lineSep))

	if encoded {
		dst := make([]byte, base64.StdEncoding.EncodedLen(len(groupResult)))
		base64.StdEncoding.Encode(dst, groupResult)
		groupResult = dst
	}

	return groupResult
}
