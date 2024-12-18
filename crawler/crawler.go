package crawler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/z0rr0/smerge/cfg"
)

const userAgent = "SMerge/1.0"

// Crawler is a main crawler structure.
type Crawler struct {
	sync.RWMutex
	groups map[string]*cfg.Group
	result map[string]string
	client *http.Client
	ctx    context.Context
	wg     sync.WaitGroup
}

type fetchResult struct {
	data  string
	error error
}

// New creates a new crawler instance.
func New(groups []cfg.Group) (*Crawler, context.CancelFunc) {
	n := len(groups)
	groupsMap := make(map[string]*cfg.Group, n)

	for i, group := range groups {
		groupsMap[group.Name] = &groups[i]
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Crawler{
		groups: groupsMap,
		result: make(map[string]string, n),
		client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		ctx:    ctx,
	}

	return c, cancel
}

// Run starts the crawler for all groups.
func (c *Crawler) Run() {
	for name := range c.groups {
		c.wg.Add(1)

		go func(group *cfg.Group) {
			period := group.Period.Timed()
			slog.Info("starting group handler", "name", name, "period", period)
			c.fetchGroup(group) // init fetch

			ticker := time.NewTicker(period)
			defer func() {
				ticker.Stop()
				c.wg.Done()
			}()

			for {
				select {
				case <-c.ctx.Done():
					slog.Info("group handler stopped", "name", group.Name)
					return
				case <-ticker.C:
					slog.Info("group handler tick", "name", group.Name, "period", period)
					c.fetchGroup(group)
				}
			}

		}(c.groups[name])
	}
}

// Stop stops the crawler.
func (c *Crawler) Stop() {
	<-c.ctx.Done()
	c.wg.Wait()
}

// Get returns the group data.
func (c *Crawler) Get(groupName string) string {
	c.RLock()
	defer c.RUnlock()
	return c.result[groupName]
}

// fetchGroup fetches all subscriptions for the group.
func (c *Crawler) fetchGroup(group *cfg.Group) {
	var (
		result  = make(chan fetchResult)
		subLen  = len(group.Subscriptions)
		builder strings.Builder
	)
	defer close(result)
	slog.Info("fetchGroup", "name", group.Name, "subscriptions", subLen)

	for i := range group.Subscriptions {
		go c.fetchSubscription(group.Name, &group.Subscriptions[i], result)
	}

	for i := 0; i < subLen; i++ {
		res := <-result
		if res.error != nil {
			slog.Error("fetchError", "group", group.Name, "error", res.error)
		} else {
			builder.WriteString(res.data)
			builder.WriteString("\n")
		}
	}

	groupSubs := strings.TrimRight(builder.String(), "\n")
	if group.Encoded {
		groupSubs = base64.StdEncoding.EncodeToString([]byte(groupSubs))
	}

	c.Lock()
	c.result[group.Name] = groupSubs
	c.Unlock()
}

// fetchSubscription fetches the subscription data.
func (c *Crawler) fetchSubscription(groupName string, sub *cfg.Subscription, result chan<- fetchResult) {
	ctx, cancel := context.WithTimeout(c.ctx, sub.Timeout.Timed())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		result <- fetchResult{error: fmt.Errorf("new request error: %w", err)}
		return
	}

	req.Header.Set("User-Agent", userAgent)
	slog.Info("fetchSubscription", "group", groupName, "name", sub.Name, "url", sub.URL)

	resp, err := c.client.Do(req)
	if err != nil {
		result <- fetchResult{error: fmt.Errorf("client do error: %w", err)}
		return
	}

	defer func() {
		if e := resp.Body.Close(); e != nil {
			slog.Error("response body close error", "group", groupName, "name", sub.Name, "error", e)
		}
	}()

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

	slog.Info("fetched", "group", groupName, "name", sub.Name, "size", n)
	result <- fetchResult{data: buf.String()}
}
