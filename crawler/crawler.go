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

// Getter is an interface for getting data by group name.
type Getter interface {
	Get(string, bool) string
}

// Crawler is a main crawler structure.
type Crawler struct {
	sync.RWMutex
	groups        map[string]*cfg.Group
	result        map[string]string
	userAgent     string
	client        *http.Client
	ctxWithCancel context.Context
	cancelFunc    context.CancelFunc
	wg            sync.WaitGroup
}

type fetchResult struct {
	data  []string
	error error
}

// New creates a new crawler instance.
func New(groups []cfg.Group, userAgent string) *Crawler {
	n := len(groups)
	groupsMap := make(map[string]*cfg.Group, n)

	for i, group := range groups {
		groupsMap[group.Name] = &groups[i]
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Crawler{
		groups:    groupsMap,
		result:    make(map[string]string, n),
		userAgent: userAgent,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment, MaxIdleConns: 100,
				MaxConnsPerHost: 20,
				IdleConnTimeout: 90 * time.Second,
			},
			Timeout: 30 * time.Second,
		},
		ctxWithCancel: ctx,
		cancelFunc:    cancel,
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
				case <-c.ctxWithCancel.Done():
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

// Get returns the group data.
func (c *Crawler) Get(groupName string, force bool) string {
	if force {
		c.fetchGroup(c.groups[groupName])
	}

	c.RLock()
	defer c.RUnlock()
	return c.result[groupName]
}

// fetchGroup fetches all subscriptions for the group.
func (c *Crawler) fetchGroup(group *cfg.Group) {
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

	subscriptionsItems := make([]string, 0, subscriptionsLen)
	for i := 0; i < subscriptionsLen; i++ {
		res := <-result
		if res.error != nil {
			slog.Error("fetchError", "group", group.Name, "error", res.error)
		} else {
			subscriptionsItems = append(subscriptionsItems, res.data...)
		}
	}

	sort.Strings(subscriptionsItems)
	groupSubs := strings.Join(subscriptionsItems, "\n")

	if group.Encoded {
		groupSubs = base64.StdEncoding.EncodeToString([]byte(groupSubs))
	}

	c.Lock()
	c.result[group.Name] = groupSubs
	c.Unlock()

	slog.Info("fetched", "group", group.Name, "size", len(subscriptionsItems), "len", len(groupSubs), "duration", time.Since(start))
}

// fetchSubscription fetches the subscription data.
func (c *Crawler) fetchSubscription(groupName string, sub *cfg.Subscription, result chan<- fetchResult) {
	ctx, cancel := context.WithTimeout(c.ctxWithCancel, sub.Timeout.Timed())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		result <- fetchResult{error: fmt.Errorf("new request error: %w", err)}
		return
	}

	req.Header.Set("User-Agent", c.userAgent)
	slog.Debug("fetchSubscription", "group", groupName, "subscription", sub.Name, "url", sub.URL)

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		result <- fetchResult{error: fmt.Errorf("client do error: %w", err)}
		return
	}

	defer func() {
		if e := resp.Body.Close(); e != nil {
			slog.Error("response body close error", "group", groupName, "subscription", sub.Name, "error", e)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		result <- fetchResult{error: fmt.Errorf("response status error: %d", resp.StatusCode)}
		return
	}

	var (
		buf = new(bytes.Buffer)
		n   int64
	)

	if sub.Encoded {
		decoder := base64.NewDecoder(base64.StdEncoding, resp.Body)
		if n, err = buf.ReadFrom(decoder); err != nil {
			result <- fetchResult{error: fmt.Errorf("read encoded response error: %w", err)}
			return
		}
	} else {
		if n, err = io.Copy(buf, resp.Body); err != nil {
			result <- fetchResult{error: fmt.Errorf("read response error: %w", err)}
			return
		}
	}

	data := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")
	slog.Info("fetched", "group", groupName, "subscription", sub.Name, "size", len(data), "bytes", n, "duration", time.Since(start))
	result <- fetchResult{data: data}
}
