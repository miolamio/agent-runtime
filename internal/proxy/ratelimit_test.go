package proxy

import (
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(5)
	for i := 0; i < 5; i++ {
		if !rl.Allow("user1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow("user1") {
		t.Error("6th request should be denied")
	}
	if !rl.Allow("user2") {
		t.Error("user2 should be allowed")
	}
}

func TestRateLimiterZeroDisabled(t *testing.T) {
	rl := NewRateLimiter(0)
	for i := 0; i < 1000; i++ {
		if !rl.Allow("user1") {
			t.Fatalf("request %d should be allowed with rpm=0", i+1)
		}
	}
}

func TestRateLimiterSetRPM(t *testing.T) {
	rl := NewRateLimiter(0)
	// rpm=0 → unlimited
	for i := 0; i < 10; i++ {
		if !rl.Allow("u") {
			t.Fatalf("unlimited: req %d denied", i+1)
		}
	}
	// Tighten to rpm=3 — existing unlimited burst should not persist.
	rl.SetRPM(3)
	for i := 0; i < 3; i++ {
		if !rl.Allow("u") {
			t.Fatalf("rpm=3: req %d should pass", i+1)
		}
	}
	if rl.Allow("u") {
		t.Error("rpm=3: 4th request must be denied")
	}
	// Loosen to unlimited — previously denied user gets through.
	rl.SetRPM(0)
	if !rl.Allow("u") {
		t.Error("rpm=0 after tighten: request must pass")
	}
	// Tighten again to rpm=5; bucket carried over 3 entries (unlimited mode
	// short-circuits without touching the bucket), so 2 more pass then the 3rd
	// hits the cap.
	rl.SetRPM(5)
	if !rl.Allow("u") {
		t.Error("rpm=5 req1 (cum 4) must pass")
	}
	if !rl.Allow("u") {
		t.Error("rpm=5 req2 (cum 5) must pass")
	}
	if rl.Allow("u") {
		t.Error("rpm=5 req3 (cum 6) must be denied")
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
