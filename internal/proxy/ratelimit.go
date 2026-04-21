package proxy

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	rateLimitWindow = time.Minute
	bucketEvictAge  = 5 * time.Minute
)

type bucket struct {
	times    []time.Time
	lastSeen time.Time
}

type RateLimiter struct {
	rpm     atomic.Int64
	buckets map[string]*bucket
	mu      sync.Mutex
}

func NewRateLimiter(rpm int) *RateLimiter {
	r := &RateLimiter{buckets: make(map[string]*bucket)}
	r.rpm.Store(int64(rpm))
	return r
}

// SetRPM updates the per-user rate limit without discarding existing buckets.
// A value <= 0 disables rate limiting.
func (r *RateLimiter) SetRPM(rpm int) {
	r.rpm.Store(int64(rpm))
}

func (r *RateLimiter) Allow(userKey string) bool {
	rpm := int(r.rpm.Load())
	if rpm <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)
	b, ok := r.buckets[userKey]
	if !ok {
		b = &bucket{}
		r.buckets[userKey] = b
	}
	b.lastSeen = now
	valid := b.times[:0]
	for _, t := range b.times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	b.times = valid
	if len(b.times) >= rpm {
		return false
	}
	b.times = append(b.times, now)

	// Lazy eviction of stale buckets
	if len(r.buckets) > 100 {
		for k, bk := range r.buckets {
			if now.Sub(bk.lastSeen) > bucketEvictAge {
				delete(r.buckets, k)
			}
		}
	}
	return true
}
