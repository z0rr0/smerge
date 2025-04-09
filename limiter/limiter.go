package limiter

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// TokenBucket is a rate limiter that uses the token bucket algorithm.
type TokenBucket struct {
	sync.RWMutex
	tokens         float64
	maxTokens      float64
	refillRate     float64 // per interval
	interval       time.Duration
	lastRefillTime time.Time
}

// NewTokenBucket creates a new TokenBucket with the specified max tokens and refill rate.
func NewTokenBucket(maxTokens, refillRate float64, interval time.Duration) *TokenBucket {
	return &TokenBucket{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		interval:       interval,
		lastRefillTime: time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	const step float64 = 1.0

	tb.Lock()
	defer tb.Unlock()

	now := time.Now()
	elapsed := float64(now.Sub(tb.lastRefillTime)) / float64(tb.interval)
	tb.lastRefillTime = now

	// add tokens by rate from last refill time
	tb.tokens = min(tb.tokens+elapsed*tb.refillRate, tb.maxTokens)

	if tb.tokens < step {
		return false
	}

	tb.tokens -= step
	return true
}

// IPRateLimiter is a rate limiter that limits requests based on the IP address.
type IPRateLimiter struct {
	sync.RWMutex
	buckets  map[string]*TokenBucket
	rate     float64
	burst    float64
	interval time.Duration
}

// NewIPRateLimiter creates a new IPRateLimiter with the specified rate and burst.
func NewIPRateLimiter(rate, burst float64, interval time.Duration) *IPRateLimiter {
	return &IPRateLimiter{
		buckets:  make(map[string]*TokenBucket),
		rate:     rate,
		burst:    burst,
		interval: interval,
	}
}

// getOrCreateBucket returns the TokenBucket for the given IP address.
// It uses privileged mode to check if the limiter was created before.
func (irl *IPRateLimiter) getOrCreateBucket(ip string) *TokenBucket {
	irl.Lock()
	bucket, ok := irl.buckets[ip]

	if !ok {
		bucket = NewTokenBucket(irl.burst, irl.rate, irl.interval)
		irl.buckets[ip] = bucket
	}

	irl.Unlock()
	return bucket
}

// GetBucket returns the TokenBucket for the given IP address.
func (irl *IPRateLimiter) GetBucket(ip string) *TokenBucket {
	irl.RLock()
	bucket, ok := irl.buckets[ip]
	irl.RUnlock()

	if !ok {
		return irl.getOrCreateBucket(ip)
	}

	return bucket
}

// CleanupBuckets removes buckets that have not been used for a specified duration.
func (irl *IPRateLimiter) CleanupBuckets(cleanupInterval time.Duration) int {
	irl.Lock()
	defer irl.Unlock()

	now := time.Now()
	removedCount := 0

	for ip, bucket := range irl.buckets {
		bucket.RLock()
		lastUsed := bucket.lastRefillTime
		bucket.RUnlock()

		if now.Sub(lastUsed) > cleanupInterval {
			delete(irl.buckets, ip)
			removedCount++
		}
	}

	return removedCount
}

// Cleanup starts a goroutine that periodically cleans up buckets that have not been used for a specified duration.
// It returns a channel that can be used to wait the cleanup process and stop it using the context.
func (irl *IPRateLimiter) Cleanup(ctx context.Context, cleanupInterval, maxIdleTime time.Duration) chan struct{} {
	var (
		ticker = time.NewTicker(maxIdleTime)
		done   = make(chan struct{})
		count  int
	)

	go func() {
		defer func() {
			ticker.Stop()
			close(done)
		}()

		for {
			select {
			case <-ticker.C:
				count = irl.CleanupBuckets(cleanupInterval)
				slog.Debug("cleanup rate limit buckets", "count", count)
			case <-ctx.Done():
				slog.Info("stopping cleanup of rate limit buckets")
				return
			}
		}
	}()

	return done
}
