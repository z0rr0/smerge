package crawler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

// lineSep is a line separator.
const lineSep = "\n"

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

	timeout *= 2
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
				IdleConnTimeout:   timeout * 3,
				ForceAttemptHTTP2: true,
			},
			Timeout: timeout,
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
		result           = make(chan fetchResult)
		start            = time.Now()
		subscriptionsLen = len(group.Subscriptions)
	)
	defer close(result)
	slog.Info("fetchGroup", "group", group.Name, "subscriptions", subscriptionsLen)

	for i := range group.Subscriptions {
		go c.fetchSubscription(group.Name, &group.Subscriptions[i], result)
	}

	urls := make([]string, 0, subscriptionsLen*avgSubURLs)
	for i := 0; i < subscriptionsLen; i++ {
		res := <-result
		if res.error != nil {
			slog.Error("fetchError", "group", group.Name, "subscription", res.subscription, "error", res.error)
		} else {
			urls = append(urls, res.urls...)
		}
	}
	groupResult := prepareGroupResult(urls, group.Encoded)

	c.Lock()
	c.result[group.Name] = groupResult
	c.Unlock()

	slog.Info("fetched", "group", group.Name, "urls", len(urls), "bytes", len(groupResult), "duration", time.Since(start))
}

// fetchSubscription fetches the subscription urls.
func (c *Crawler) fetchSubscription(groupName string, sub *cfg.Subscription, result chan<- fetchResult) {
	ctx, cancel := context.WithTimeout(c.ctx, sub.Timeout.Timed())
	defer cancel()

	fetchRes := fetchResult{subscription: sub.Name}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		fetchRes.error = fmt.Errorf("new request error: %w", err)
		result <- fetchRes
		return
	}

	req.Header.Set("User-Agent", c.userAgent)
	slog.Debug("fetchSubscription", "group", groupName, "subscription", sub.Name, "url", sub.URL)

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		fetchRes.error = fmt.Errorf("client do error: %w", err)
		result <- fetchRes
		return
	}

	defer func() {
		if e := resp.Body.Close(); e != nil {
			slog.Error("response body close error", "group", groupName, "subscription", sub.Name, "error", e)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		fetchRes.error = fmt.Errorf("response status error: %d", resp.StatusCode)
		result <- fetchRes
		return
	}

	urls, n, err := readSubscription(resp.Body, sub.Encoded)
	if err != nil {
		fetchRes.error = err
		result <- fetchRes
	}

	slog.Info("fetched", "group", groupName, "subscription", sub.Name, "size", len(urls), "bytes", n, "duration", time.Since(start))
	fetchRes.urls = urls
	result <- fetchRes
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

	urls := strings.Split(strings.ReplaceAll(strings.Trim(buf.String(), lineSep), "\r\n", lineSep), lineSep)
	return urls, n, nil
}

// prepareGroupResult prepares the group result for storing.
func prepareGroupResult(urls []string, encoded bool) []byte {
	sort.Strings(urls)
	groupResult := []byte(strings.Join(urls, lineSep))

	if encoded {
		dst := make([]byte, base64.StdEncoding.EncodedLen(len(groupResult)))
		base64.StdEncoding.Encode(dst, groupResult)
		groupResult = dst
	}

	return groupResult
}
