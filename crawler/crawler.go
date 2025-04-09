package crawler

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
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

// bufferSize is a size of buffer for reading subscription data.
const bufferSize = 3072

var (
	// bufferPool is a pool for bytes.Buffer for subscription data reading.
	bufferPool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, bufferSize))
		},
	}

	// ErrGroupDecode is a public error for decode error.
	ErrGroupDecode = fmt.Errorf("decode error")

	// ErrNotFoundGroup is a public error for group not found.
	ErrNotFoundGroup = fmt.Errorf("group not found")
)

// Getter is an interface for getting data by group name.
// If force is true, the data will be fetched from the source.
// If decode is true, the data will be decoded from base64 if request group has Encoded flag.
type Getter interface {
	Get(groupName string, force bool, decode bool) ([]byte, error)
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
	semaphore  chan struct{} // to limit the number of concurrent goroutines for fetchSubscription
}

type fetchResult struct {
	subscription string
	urls         []string
	error        error
}

// New creates a new crawler instance.
func New(groups []cfg.Group, userAgent string, retries uint8, maxConcurrent int) *Crawler {
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
	transport := &http.Transport{
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
	}
	client := NewRetryClient(retries, transport, timeout*2, retryInternalServerError, calcDelay)

	return &Crawler{
		groups:     groupsMap,
		result:     make(map[string][]byte, groupLen),
		userAgent:  userAgent,
		client:     client,
		ctx:        ctx,
		cancelFunc: cancel,
		semaphore:  make(chan struct{}, maxConcurrent),
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
	close(c.semaphore)
}

// needDecode checks if the group data needs to be decoded.
// A caller should hold the read lock.
func (c *Crawler) needDecode(groupName string, decode bool, resultSize int) bool {
	if !decode || resultSize == 0 {
		return false
	}

	group, ok := c.groups[groupName]
	return ok && group.Encoded
}

// Get returns the group data.
func (c *Crawler) Get(groupName string, force bool, decode bool) ([]byte, error) {
	group, ok := c.groups[groupName]
	if !ok {
		return nil, errors.Join(ErrNotFoundGroup, fmt.Errorf("group name %q", groupName))
	}

	if force {
		c.fetchGroup(group)
	}

	c.RLock()
	groupResult, ok := c.result[groupName]
	c.RUnlock()

	if !ok {
		return nil, errors.Join(ErrNotFoundGroup, errors.New("no group result"))
	}

	resultSize := len(groupResult)

	if c.needDecode(groupName, decode, resultSize) {
		return decodeGroup(groupResult, resultSize, groupName)
	}

	return groupResult, nil
}

// fetchGroup fetches all subscriptions for the group.
func (c *Crawler) fetchGroup(group *cfg.Group) {
	const avgSubURLs = 10
	var (
		start            = time.Now()
		subResult        = make(chan fetchResult, 1) // to collect results from subscriptions
		ready            = make(chan struct{})       // to signal that all subscriptions are fetched
		subscriptionsLen = len(group.Subscriptions)
		avgURLsLen       = subscriptionsLen * avgSubURLs
	)
	defer close(subResult)
	slog.Info("fetchGroup", "group", group.Name, "subscriptions", subscriptionsLen)

	urls := make([]string, 0, avgURLsLen)
	go func() {
		for range subscriptionsLen {
			if res := <-subResult; res.error != nil {
				slog.Error("fetchError", "group", group.Name, "subscription", res.subscription, "error", res.error)
			} else {
				urls = append(urls, res.urls...)
			}
		}
		close(ready) // all subscriptions are fetched
	}()

	for i := range group.Subscriptions {
		c.semaphore <- struct{}{} // to limit total number of goroutines

		go func(name string, sub *cfg.Subscription) {
			defer func() {
				<-c.semaphore
				if r := recover(); r != nil {
					slog.Error("fetch subscription panic", "group", name, "subscription", sub.Name, "recover", r)
					subResult <- fetchResult{subscription: sub.Name, error: fmt.Errorf("fetch sub panic: %v", r)}
				}
			}()
			c.fetchSubscription(name, sub, subResult)
		}(group.Name, &group.Subscriptions[i])
	}

	<-ready
	result := prepareGroupResult(urls, group.Encoded)

	c.Lock()
	c.result[group.Name] = result
	c.Unlock()

	slog.Info("fetched", "group", group.Name, "urls", len(urls), "bytes", len(result), "duration", time.Since(start))
}

func (c *Crawler) fetchURLSubscription(ctx context.Context, sub *cfg.Subscription) (io.ReadCloser, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.Path.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("new request error: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)

	if err != nil {
		return nil, 0, fmt.Errorf("client do error: %w", err)
	}

	return resp.Body, resp.StatusCode, nil
}

// fetchLocalSubscription fetches the subscription if sub.Path is a local file.
func (c *Crawler) fetchLocalSubscription(ctx context.Context, sub *cfg.Subscription) (io.ReadCloser, int, error) {
	var (
		fd       *os.File
		err      error
		done     = make(chan struct{})
		fileName = sub.Path.String()
	)

	go func() {
		fd, err = os.Open(fileName) // #nosec G304, file name is already validated during configuration parsing
		close(done)
	}()

	select {
	case <-done:
		if err != nil {
			return nil, 0, fmt.Errorf("open file=%q error: %w", fileName, err)
		}
		// if a subscription's file was opened successfully, return a fake http.StatusOK status
		// to maintain a consistent signature with fetchURLSubscription.
		return fd, http.StatusOK, nil
	case <-ctx.Done():
		err = c.ctx.Err()

		// if we return error, the file will not be closed, do it here obviously
		if closeErr := fd.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}

		return nil, 0, fmt.Errorf("open file=%q context error: %w", fileName, err)
	}
}

// fetchSubscription fetches the subscription urls.
func (c *Crawler) fetchSubscription(groupName string, sub *cfg.Subscription, result chan<- fetchResult) {
	var (
		fetchRes    = fetchResult{subscription: sub.Name}
		ctx, cancel = context.WithTimeout(c.ctx, sub.Timeout.Timed())
		statusCode  int
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
		reader, statusCode, err = c.fetchLocalSubscription(ctx, sub)
	} else {
		reader, statusCode, err = c.fetchURLSubscription(ctx, sub)
	}

	if err != nil {
		fetchRes.error = fmt.Errorf("fetch error: %w", err)
		return
	}

	defer func() {
		if e := reader.Close(); e != nil {
			slog.Error("reader close error", "group", groupName, "subscription", sub.Name, "error", e)
		}
	}()

	if statusCode != http.StatusOK {
		fetchRes.error = fmt.Errorf("response status error: %d", statusCode)
		return
	}

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
		n   int64
		err error
	)

	buf := bufferPool.Get().(*bytes.Buffer) // get a buffer from common pool
	buf.Reset()
	defer bufferPool.Put(buf)

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

	if len(urls) == 0 {
		return nil
	}

	sort.Strings(urls)
	groupResult := []byte(strings.Join(urls, lineSep))

	if encoded {
		dst := make([]byte, base64.StdEncoding.EncodedLen(len(groupResult)))
		base64.StdEncoding.Encode(dst, groupResult)
		groupResult = dst
	}

	return groupResult
}

func decodeGroup(groupResult []byte, resultSize int, groupName string) ([]byte, error) {
	dst := make([]byte, base64.StdEncoding.DecodedLen(resultSize))
	n, err := base64.StdEncoding.Decode(dst, groupResult)

	if err != nil {
		slog.Error("decode error", "group", groupName, "error", err)
		return nil, ErrGroupDecode
	}

	slog.Debug("decoded", "group", groupName, "size", n)
	return dst[:n], nil
}
