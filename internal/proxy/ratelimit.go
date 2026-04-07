package proxy

import (
	"sync"
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
	rpm     int
	buckets map[string]*bucket
	mu      sync.Mutex
}

func NewRateLimiter(rpm int) *RateLimiter {
	return &RateLimiter{rpm: rpm, buckets: make(map[string]*bucket)}
}

func (r *RateLimiter) Allow(userKey string) bool {
	if r.rpm <= 0 {
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
	if len(b.times) >= r.rpm {
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
