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
	"net/http"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/ratelimit"
)

// Authenticator resolves a presented credential to the instance it authorises.
// It returns an error for an unknown or revoked key without distinguishing
// them, because telling them apart would confirm which keys once existed.
type Authenticator interface {
	Authenticate(ctx context.Context, secret string) (instanceID string, err error)
}

// ClientReporter records which MCP client a credential is being used from. It
// is optional: an Authenticator that does not implement it simply loses the
// detail, and nothing else changes.
//
// The name comes from the client itself, in the `initialize` handshake, so it
// is a claim rather than a proof. That is fine for what it is used for — the
// panel saying "Claude Desktop" instead of a key prefix — and it is never
// trusted for authorisation.
type ClientReporter interface {
	NoteClient(ctx context.Context, secret, name, version string)
}

// clientInfo is the slice of the `initialize` request worth keeping.
type initializeRequest struct {
	Method string `json:"method"`
	Params struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	} `json:"params"`
}

// maxClientField bounds what a client can write into the panel. The values are
// displayed, so an unbounded string is a defacement waiting to happen.
const maxClientField = 60

// handshakeClient reads the client's self-description out of a request body,
// reporting false for anything that is not an `initialize` naming itself.
func handshakeClient(body []byte) (name, version string, ok bool) {
	var request initializeRequest
	if err := json.Unmarshal(body, &request); err != nil || request.Method != "initialize" {
		return "", "", false
	}
	name = strings.TrimSpace(request.Params.ClientInfo.Name)
	version = strings.TrimSpace(request.Params.ClientInfo.Version)
	if name == "" {
		return "", "", false
	}
	return truncate(name, maxClientField), truncate(version, maxClientField), true
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
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
	limit  *ratelimit.Attempts
}

// New builds the MCP HTTP handler. Mount it at the endpoint the client is
// configured with.
func New(server *mcp.Server, auth Authenticator, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{server: server, auth: auth, logger: logger, limit: ratelimit.New(maxFailures, lockout)}
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
	if !h.limit.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	instanceID, err := h.auth.Authenticate(r.Context(), secret)
	if err != nil {
		h.limit.Fail(clientIP(r))
		h.deny(w, r, "rejected credential")
		return
	}
	h.limit.Succeed(clientIP(r))

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

	// Every client announces itself once, when it connects. Catching it here is
	// what lets the panel name the tool on the other end instead of only the
	// key it presented.
	if reporter, ok := h.auth.(ClientReporter); ok {
		if name, version, isHandshake := handshakeClient(body); isHandshake {
			go func() {
				noteCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				reporter.NoteClient(noteCtx, secret, name, version)
			}()
		}
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

// clientIP attributes a request to an address. X-Forwarded-For is believed only
// when the request arrived from a proxy on a private network — see the
// ratelimit package for why.
func clientIP(r *http.Request) string { return ratelimit.ClientIP(r) }

const (
	maxFailures = 10
	lockout     = time.Minute
)
