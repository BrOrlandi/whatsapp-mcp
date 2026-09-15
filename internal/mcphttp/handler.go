// Package mcphttp serves the MCP endpoint over Streamable HTTP.
//
// The transport is deliberately stateless: every request carries its own
// credential and is authorised on its own, so a dropped connection costs
// nothing and the next request simply works. That is what makes the remote
// endpoint resilient without any reconnection protocol.
package mcphttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
)

// Authenticator resolves a presented credential to the instance it authorises.
// It returns an error for an unknown or revoked key without distinguishing
// them, because telling them apart would confirm which keys once existed.
type Authenticator interface {
	Authenticate(ctx context.Context, secret string) (instanceID string, err error)
}

const (
	// maxBody bounds a single JSON-RPC request. Tool arguments are short; a
	// larger body is a mistake or an attack, and either way is refused rather
	// than buffered.
	maxBody = 1 << 20
	// callTimeout bounds one tool call, so a stalled Evolution request cannot
	// hold a connection open indefinitely.
	callTimeout = 60 * time.Second
)

type handler struct {
	server *mcp.Server
	auth   Authenticator
	logger *slog.Logger
	limit  *attempts
}

// New builds the MCP HTTP handler. Mount it at the endpoint the client is
// configured with.
func New(server *mcp.Server, auth Authenticator, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{server: server, auth: auth, logger: logger, limit: newAttempts()}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Streamable HTTP allows a GET for a server-initiated stream. This server
		// has nothing to push, so it says so plainly instead of holding a
		// connection open.
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "this MCP endpoint accepts POST")
		return
	}

	secret, ok := bearer(r)
	if !ok {
		h.deny(w, r, "missing credential")
		return
	}
	if !h.limit.allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	instanceID, err := h.auth.Authenticate(r.Context(), secret)
	if err != nil {
		h.limit.fail(clientIP(r))
		h.deny(w, r, "rejected credential")
		return
	}
	h.limit.succeed(clientIP(r))

	session, err := h.server.Resolve(r.Context(), instanceID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "the instance this credential authorises is not available")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}

	ctx, cancel := context.WithTimeout(mcp.WithSession(r.Context(), session), callTimeout)
	defer cancel()

	response := h.server.Handle(ctx, body)
	if len(response) == 0 {
		// A notification has no reply. Streamable HTTP expects 202 for it.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(response)
}

// deny answers an unauthenticated request. The reason is logged, never
// returned, and the credential itself never appears in either.
func (h *handler) deny(w http.ResponseWriter, r *http.Request, reason string) {
	h.logger.Warn("MCP request denied", "reason", reason, "remote", clientIP(r))
	w.Header().Set("WWW-Authenticate", `Bearer realm="whatsapp-mcp"`)
	writeError(w, http.StatusUnauthorized, "a valid API key is required")
}

// bearer extracts the credential from the Authorization header. A credential in
// a query string would end up in proxy and browser logs, so only the header is
// accepted.
func bearer(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	secret, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		secret, found = strings.CutPrefix(header, "bearer ")
	}
	secret = strings.TrimSpace(secret)
	return secret, found && secret != ""
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// attempts throttles credential guessing per source address. A high-entropy key
// cannot realistically be guessed, but an unthrottled endpoint still invites
// the attempt and fills the logs with it.
type attempts struct {
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	failures int
	until    time.Time
}

const (
	maxFailures = 10
	lockout     = time.Minute
)

func newAttempts() *attempts { return &attempts{entries: map[string]*entry{}} }

func (a *attempts) allow(ip string) bool {
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
	return record.failures < maxFailures
}

func (a *attempts) fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.entries[ip]
	if !ok || time.Now().After(record.until) {
		record = &entry{}
		a.entries[ip] = record
	}
	record.failures++
	record.until = time.Now().Add(lockout)
	// Bound the table so a spray across many addresses cannot grow it forever.
	if len(a.entries) > 10000 {
		for key, value := range a.entries {
			if time.Now().After(value.until) {
				delete(a.entries, key)
			}
		}
	}
}

func (a *attempts) succeed(ip string) {
	a.mu.Lock()
	delete(a.entries, ip)
	a.mu.Unlock()
}
