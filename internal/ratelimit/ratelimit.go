// Package ratelimit throttles credential guessing, and works out which address
// to throttle.
//
// It is shared by the MCP endpoint and the panel login because both are
// credential checks reachable from the internet, and a limiter that protects
// only one of them protects neither: the panel password is the weaker secret of
// the two, since a person chose it.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClientIP returns the address to attribute a request to.
//
// X-Forwarded-For is only believed when the request actually arrived from a
// private or loopback address, which is where a reverse proxy sits. Trusting it
// unconditionally would hand any caller a free bypass: a fresh fabricated
// address on every attempt makes a per-address limiter count to one forever.
// When the peer is a public address, the peer is the client and the header is
// somebody's claim about themselves.
func ClientIP(r *http.Request) string {
	peer := peerIP(r)
	if !isInternal(peer) {
		return peer.String()
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return peer.String()
	}
	// Right to left: the rightmost entries were appended by proxies we just
	// established are ours, so the first address that is not one of them is the
	// closest thing to a real client on record.
	hops := strings.Split(forwarded, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(hops[i]))
		if candidate == nil {
			continue
		}
		if !isInternal(candidate) {
			return candidate.String()
		}
	}
	return peer.String()
}

func peerIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	// A non-address RemoteAddr means a test server or a unix socket. Neither is
	// reachable from outside, so it is treated as internal.
	return net.IPv4(127, 0, 0, 1)
}

func isInternal(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// Attempts throttles failed credential checks per source address.
//
// A high-entropy API key cannot realistically be guessed, but the panel
// password can be, and an unthrottled endpoint invites the attempt either way.
type Attempts struct {
	mu          sync.Mutex
	entries     map[string]*entry
	maxFailures int
	lockout     time.Duration
	capacity    int
}

type entry struct {
	failures int
	until    time.Time
}

// New builds a limiter that locks an address out for the given window once it
// has failed maxFailures times.
func New(maxFailures int, lockout time.Duration) *Attempts {
	return &Attempts{entries: map[string]*entry{}, maxFailures: maxFailures, lockout: lockout, capacity: 10000}
}

// Allow reports whether an address may attempt a credential right now.
func (a *Attempts) Allow(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.entries[ip]
	if !ok {
		return true
	}
	if time.Now().After(record.until) {
		delete(a.entries, ip)
		return true
	}
	return record.failures < a.maxFailures
}

// Fail records a rejected credential.
func (a *Attempts) Fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.entries[ip]
	if !ok || time.Now().After(record.until) {
		record = &entry{}
		a.entries[ip] = record
	}
	record.failures++
	record.until = time.Now().Add(a.lockout)
	a.prune()
}

// Succeed clears an address after a credential is accepted.
func (a *Attempts) Succeed(ip string) {
	a.mu.Lock()
	delete(a.entries, ip)
	a.mu.Unlock()
}

// prune bounds the table. Dropping expired entries is the cheap pass; if every
// entry is still live the oldest are evicted anyway, because a spray wide enough
// to fill this table must not be able to grow it without limit. Evicting a live
// entry only forgives an attacker who is already being locked out elsewhere.
func (a *Attempts) prune() {
	if len(a.entries) <= a.capacity {
		return
	}
	for key, value := range a.entries {
		if time.Now().After(value.until) {
			delete(a.entries, key)
		}
	}
	for key := range a.entries {
		if len(a.entries) <= a.capacity {
			break
		}
		delete(a.entries, key)
	}
}
