package limiter

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	tests := []struct {
		name           string
		maxTokens      float64
		refillRate     float64
		interval       time.Duration
		requests       int
		sleepIntervals []time.Duration
		wantResults    []bool
	}{
		{
			name:           "Single request with filled bucket",
			maxTokens:      5,
			refillRate:     1,
			interval:       time.Second,
			requests:       1,
			sleepIntervals: []time.Duration{0},
			wantResults:    []bool{true},
		},
		{
			name:           "Multiple requests with filled bucket",
			maxTokens:      5,
			refillRate:     1,
			interval:       time.Second,
			requests:       6,
			sleepIntervals: []time.Duration{0, 0, 0, 0, 0, 0},
			wantResults:    []bool{true, true, true, true, true, false},
		},
		{
			name:           "Refill after depletion",
			maxTokens:      2,
			refillRate:     1,
			interval:       time.Millisecond * 50,
			requests:       4,
			sleepIntervals: []time.Duration{0, 0, time.Millisecond * 100, 0},
			wantResults:    []bool{true, true, true, true},
		},
		{
			name:           "Partial refill after depletion",
			maxTokens:      3,
			refillRate:     1,
			interval:       time.Millisecond * 50,
			requests:       5,
			sleepIntervals: []time.Duration{0, 0, 0, time.Millisecond * 10, time.Millisecond * 50},
			wantResults:    []bool{true, true, true, false, true},
		},
		{
			name:           "Refill up to max capacity",
			maxTokens:      2,
			refillRate:     1,
			interval:       time.Millisecond * 50,
			requests:       3,
			sleepIntervals: []time.Duration{0, 0, time.Millisecond * 50},
			wantResults:    []bool{true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := NewTokenBucket(tt.maxTokens, tt.refillRate, tt.interval)

			for i := 0; i < tt.requests; i++ {
				if i > 0 && tt.sleepIntervals[i] > 0 {
					time.Sleep(tt.sleepIntervals[i])
				}

				got := tb.Allow()
				if want := tt.wantResults[i]; got != want {
					t.Errorf("request %d: got = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestIPRateLimiter_GetBucket(t *testing.T) {
	tests := []struct {
		name     string
		rate     float64
		burst    float64
		interval time.Duration
		ips      []string
		wantSame []bool // whether the same bucket should be returned for consecutive calls with the same IP
		excluded map[string]struct{}
	}{
		{
			name:     "Single IP",
			rate:     1,
			burst:    5,
			interval: time.Second,
			ips:      []string{"192.168.1.1", "192.168.1.1"},
			wantSame: []bool{true},
		},
		{
			name:     "Multiple IPs",
			rate:     1,
			burst:    5,
			interval: time.Second,
			ips:      []string{"192.168.1.1", "192.168.1.2", "192.168.1.1", "192.168.1.1"},
			wantSame: []bool{false, false, true},
		},
		{
			name:     "Multiple IPs with exclusions",
			rate:     1,
			burst:    5,
			interval: time.Second,
			ips:      []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
			wantSame: []bool{true, false},
			excluded: map[string]struct{}{"192.168.1.1": {}, "192.168.1.2": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			irl := NewIPRateLimiter(tt.rate, tt.burst, tt.interval, tt.excluded)
			size := len(tt.ips)
			buckets := make([]Bucket, 0, size)

			for _, ip := range tt.ips {
				bucket := irl.GetBucket(ip)

				if _, ok := tt.excluded[ip]; ok {
					if _, ok = bucket.(*IgnoreLimitBucket); !ok {
						t.Errorf("expected IgnoreLimitBucket for excluded IP %s, got %T", ip, bucket)
					}
				}

				buckets = append(buckets, irl.GetBucket(ip))
			}

			for i := 1; i < size; i++ {
				sameBucket := buckets[i] == buckets[i-1]

				if sameBucket != tt.wantSame[i-1] {
					t.Errorf(
						"fequest %d and %d: got same bucket = %v, want same = %v for IPs %s and %s",
						i, i-1, sameBucket, tt.wantSame[i-1], tt.ips[i-1], tt.ips[i],
					)
				}
			}
		})
	}
}

func TestIPRateLimiter_RateLimiting(t *testing.T) {
	tests := []struct {
		name           string
		rate           float64
		burst          float64
		interval       time.Duration
		ips            []string
		sleepIntervals []time.Duration
		wantResults    []bool
		excluded       map[string]struct{}
	}{
		{
			name:           "Single IP within limit",
			rate:           1,
			burst:          2,
			interval:       time.Second,
			ips:            []string{"192.168.1.1", "192.168.1.1"},
			sleepIntervals: []time.Duration{0, 0},
			wantResults:    []bool{true, true},
		},
		{
			name:           "Single IP exceeding limit",
			rate:           1,
			burst:          2,
			interval:       time.Second,
			ips:            []string{"192.168.1.1", "192.168.1.1", "192.168.1.1"},
			sleepIntervals: []time.Duration{0, 0, 0},
			wantResults:    []bool{true, true, false},
		},
		{
			name:           "Multiple IPs separate limits",
			rate:           1,
			burst:          1,
			interval:       time.Second,
			ips:            []string{"192.168.1.1", "192.168.1.2", "192.168.1.1", "192.168.1.2"},
			sleepIntervals: []time.Duration{0, 0, 0, 0},
			wantResults:    []bool{true, true, false, false},
		},
		{
			name:           "IP refill after time",
			rate:           1,
			burst:          1,
			interval:       time.Millisecond * 50,
			ips:            []string{"192.168.1.1", "192.168.1.1", "192.168.1.1"},
			sleepIntervals: []time.Duration{0, 0, time.Millisecond * 60},
			wantResults:    []bool{true, false, true},
		},
		{
			name:           "Excluded IP",
			rate:           1,
			burst:          1,
			interval:       time.Second,
			ips:            []string{"192.168.1.1", "192.168.1.2", "192.168.1.1", "192.168.1.2"},
			sleepIntervals: []time.Duration{0, 0, 0, 0},
			wantResults:    []bool{true, true, true, true},
			excluded:       map[string]struct{}{"192.168.1.1": {}, "192.168.1.2": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			irl := NewIPRateLimiter(tt.rate, tt.burst, tt.interval, tt.excluded)

			for i, ip := range tt.ips {
				if i > 0 && tt.sleepIntervals[i] > 0 {
					time.Sleep(tt.sleepIntervals[i])
				}

				bucket := irl.GetBucket(ip)
				got := bucket.Allow()

				if want := tt.wantResults[i]; got != want {
					t.Errorf("request %d for IP %s: got = %v, want %v", i+1, ip, got, want)
				}
			}
		})
	}
}

func TestIPRateLimiter_CleanupBuckets(t *testing.T) {
	tests := []struct {
		name             string
		ips              []string
		ipSleepDurations []time.Duration
		interval         time.Duration
		cleanupInterval  time.Duration
		wantRemoved      uint64
		wantRemaining    uint64
	}{
		{
			name:            "No buckets to clean",
			ips:             []string{},
			interval:        time.Millisecond * 100,
			cleanupInterval: time.Millisecond,
		},
		{
			name:             "No idle buckets",
			ips:              []string{"192.168.1.1", "192.168.1.2"},
			ipSleepDurations: []time.Duration{time.Millisecond, time.Millisecond},
			interval:         time.Millisecond * 100,
			cleanupInterval:  time.Millisecond * 50,
			wantRemoved:      0,
			wantRemaining:    2,
		},
		{
			name:             "Some idle buckets",
			ips:              []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
			ipSleepDurations: []time.Duration{time.Millisecond * 25, time.Millisecond, time.Millisecond},
			interval:         time.Millisecond * 100,
			cleanupInterval:  time.Millisecond * 20,
			wantRemoved:      1,
			wantRemaining:    2,
		},
		{
			name:             "All buckets idle",
			ips:              []string{"192.168.1.1", "192.168.1.2"},
			ipSleepDurations: []time.Duration{time.Millisecond * 10, time.Millisecond * 20},
			interval:         time.Millisecond * 100,
			cleanupInterval:  time.Millisecond * 10,
			wantRemoved:      2,
		},
		{
			name:             "Multiple identical IPs",
			ips:              []string{"192.168.1.1", "192.168.1.2", "192.168.1.1"},
			ipSleepDurations: []time.Duration{time.Millisecond * 5, time.Millisecond * 25, time.Millisecond},
			interval:         time.Millisecond * 100,
			cleanupInterval:  time.Millisecond * 20,
			wantRemoved:      1,
			wantRemaining:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			irl := NewIPRateLimiter(10.0, 20.0, tt.interval, nil)

			for i, ip := range tt.ips {
				bucket := irl.GetBucket(ip)
				_ = bucket.Allow()

				time.Sleep(tt.ipSleepDurations[i])
			}

			got := irl.cleanupBuckets(tt.cleanupInterval)
			if got != tt.wantRemoved {
				t.Errorf("cleanupBuckets() = %v, want %v", got, tt.wantRemoved)
			}

			irl.RLock()
			got = uint64(len(irl.buckets))
			irl.RUnlock()

			if got != tt.wantRemaining {
				t.Errorf("remaining buckets = %v, want %v", got, tt.wantRemaining)
			}
		})
	}
}

func TestIPRateLimiter_Cleanup(t *testing.T) {
	interval := time.Millisecond * 500
	irl := NewIPRateLimiter(10.0, 20.0, interval, nil)

	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for _, ip := range ips {
		bucket := irl.GetBucket(ip)
		_ = bucket.Allow()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*150)
	defer cancel()

	cleanupInterval := 120 * time.Millisecond
	maxIdleTime := 50 * time.Millisecond

	done := irl.Cleanup(ctx, cleanupInterval, maxIdleTime)

	time.Sleep(80 * time.Millisecond)
	bucket := irl.GetBucket(ips[2]) // remaining bucket
	_ = bucket.Allow()

	// wait for cleanup to finish
	<-done

	irl.RLock()
	remainingCount := len(irl.buckets)
	irl.RUnlock()

	if remainingCount != 1 {
		t.Errorf("after cleanup goroutine: bucket count = %v, want %v", remainingCount, 1)
	}
}
