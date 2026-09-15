package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A per-address limiter that believes X-Forwarded-For from anyone is not a
// limiter: a fresh fabricated address on every attempt keeps the count at one.
// The header is only evidence when a proxy we run put it there.
func TestClientIPIgnoresForwardedHeaderFromAPublicPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.RemoteAddr = "198.51.100.9:44321"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	if got := ClientIP(r); got != "198.51.100.9" {
		t.Fatalf("ClientIP = %q, want the peer address", got)
	}
}

func TestClientIPBelievesAProxyOnAPrivateNetwork(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.RemoteAddr = "172.18.0.4:59000"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.2")
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want the first public hop", got)
	}
}

func TestLockoutAndRelease(t *testing.T) {
	limiter := New(2, 50*time.Millisecond)
	if !limiter.Allow("1.2.3.4") {
		t.Fatal("a fresh address was refused")
	}
	limiter.Fail("1.2.3.4")
	limiter.Fail("1.2.3.4")
	if limiter.Allow("1.2.3.4") {
		t.Fatal("the address was not locked out")
	}
	time.Sleep(60 * time.Millisecond)
	if !limiter.Allow("1.2.3.4") {
		t.Fatal("the lockout did not expire")
	}
}

// A spray across fabricated addresses must not be able to grow the table
// without limit, even while every entry is still inside its lockout window.
func TestTableStaysBounded(t *testing.T) {
	limiter := New(5, time.Hour)
	limiter.capacity = 100
	for i := 0; i < 5000; i++ {
		limiter.Fail(string(rune(i)) + "x")
	}
	limiter.mu.Lock()
	size := len(limiter.entries)
	limiter.mu.Unlock()
	if size > 101 {
		t.Fatalf("table grew to %d entries", size)
	}
}
