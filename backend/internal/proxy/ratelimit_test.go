package proxy

import (
	"testing"
	"time"
)

func TestIPRateLimiter(t *testing.T) {
	// 10 RPS, burst of 3
	limiter := NewIPRateLimiter(10, 3)

	ip := "192.168.1.100"

	// First 3 requests should be permitted immediately (burst)
	for i := 0; i < 3; i++ {
		if !limiter.Allow(ip) {
			t.Fatalf("request %d should have been allowed within burst capacity", i+1)
		}
	}

	// 4th request without delay should be rejected
	if limiter.Allow(ip) {
		t.Fatal("request 4 should have been rejected after exhausting burst capacity")
	}

	stats := limiter.Stats()
	if stats.TotalRejected != 1 {
		t.Errorf("expected 1 rejected request in stats, got %d", stats.TotalRejected)
	}
	if stats.ActiveIPs != 1 {
		t.Errorf("expected 1 active IP in stats, got %d", stats.ActiveIPs)
	}

	// Wait 250ms -> should replenish ~2.5 tokens
	time.Sleep(250 * time.Millisecond)
	if !limiter.Allow(ip) {
		t.Fatal("request should be allowed after token replenishment")
	}
}

func TestIPRateLimiterDifferentIPs(t *testing.T) {
	limiter := NewIPRateLimiter(5, 2)

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"

	// Exhaust ip1
	limiter.Allow(ip1)
	limiter.Allow(ip1)
	if limiter.Allow(ip1) {
		t.Error("expected ip1 to be exhausted")
	}

	// ip2 should still have full quota
	if !limiter.Allow(ip2) {
		t.Error("ip2 should be allowed independently of ip1")
	}
}

func TestIPRateLimiterCleanup(t *testing.T) {
	limiter := NewIPRateLimiter(10, 10)
	limiter.Allow("1.1.1.1")
	limiter.Allow("2.2.2.2")

	if limiter.Stats().ActiveIPs != 2 {
		t.Fatalf("expected 2 active IPs, got %d", limiter.Stats().ActiveIPs)
	}

	time.Sleep(20 * time.Millisecond)
	removed := limiter.Cleanup(10 * time.Millisecond)
	if removed != 2 {
		t.Errorf("expected 2 idle IPs removed, got %d", removed)
	}
	if limiter.Stats().ActiveIPs != 0 {
		t.Errorf("expected 0 active IPs after cleanup, got %d", limiter.Stats().ActiveIPs)
	}
}

