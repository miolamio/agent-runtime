package proxy

import (
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(5)
	for i := 0; i < 5; i++ {
		if !rl.Allow("student1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow("student1") {
		t.Error("6th request should be denied")
	}
	if !rl.Allow("student2") {
		t.Error("student2 should be allowed")
	}
}

func TestRateLimiterZeroDisabled(t *testing.T) {
	rl := NewRateLimiter(0)
	for i := 0; i < 1000; i++ {
		if !rl.Allow("student1") {
			t.Fatalf("request %d should be allowed with rpm=0", i+1)
		}
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2)
	rl.Allow("s1")
	rl.Allow("s1")
	if rl.Allow("s1") {
		t.Error("should be denied after 2")
	}
	rl.mu.Lock()
	bucket := rl.buckets["s1"]
	for i := range bucket.times {
		bucket.times[i] = time.Now().Add(-2 * time.Minute)
	}
	rl.mu.Unlock()
	if !rl.Allow("s1") {
		t.Error("should be allowed after window expiry")
	}
}
