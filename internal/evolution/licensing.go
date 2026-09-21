package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// licensingServer is Evolution Foundation's licensing service, the only party
// that can issue a licence credential. The register page it serves does the
// same thing this client does — POST /v1/auth/magic-link — so a licence can
// be started without a person ever opening that page. It is a variable so
// tests can point it at a stub.
var licensingServer = "https://license.evolutionfoundation.com.br"

// LicenseActivation is what a completed registration yields. Only InstanceID
// is filled in: the credential itself is exchanged by Evolution and kept in
// Evolution's own database, and /license/status reports it masked, so the
// remaining fields exist for the store's shape rather than carrying anything.
type LicenseActivation struct {
	APIKey     string `json:"api_key"`
	Tier       string `json:"tier"`
	CustomerID int    `json:"customer_id"`
	InstanceID string `json:"instance_id"`
}

// RegisterOperator starts a licence registration for an email without any
// browser form. Evolution mints a registration token bound to this instance,
// the licensing server turns it into a magic link in the operator's inbox, and
// clicking that link is what proves the operator owns the email — the one step
// with no way around it, because the identity is the thing being registered.
//
// callback is where the licensing server sends the operator after the link is
// clicked; it becomes Evolution's redirect_uri for the registration token.
func (c *Client) RegisterOperator(ctx context.Context, email, name, callback string) error {
	registration, err := c.License(ctx, callback)
	if err != nil {
		return err
	}
	if registration.RegisterURL == "" {
		return fmt.Errorf("a Evolution não devolveu um link de registro")
	}
	token, err := registrationToken(registration.RegisterURL)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"token": token, "email": email, "name": name})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, licensingServer+"/v1/auth/magic-link", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("licensing server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("licensing server returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// registrationToken reads the one-time token Evolution embeds in its register
// URL. The URL is Evolution's own output, and the token only works together
// with this installation's instance id, so it is safe to carry into the next
// request as it is.
func registrationToken(registerURL string) (string, error) {
	parsed, err := url.Parse(registerURL)
	if err != nil {
		return "", fmt.Errorf("link de registro inválido: %w", err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		return "", fmt.Errorf("link de registro sem token")
	}
	return token, nil
}

// CompleteActivation finishes what RegisterOperator started. The code arrives
// in the link that was clicked; only the licensing server can judge it, and it
// can be spent exactly once — which is why it is spent in the one place that
// makes Evolution licensed.
func (c *Client) CompleteActivation(ctx context.Context, code string) (LicenseActivation, error) {
	var activation LicenseActivation
	var status License
	if err := c.getJSON(ctx, "/license/status", &status); err != nil {
		return activation, err
	}
	activation.InstanceID = status.InstanceID
	if code == "" {
		return activation, fmt.Errorf("código de ativação ausente")
	}
	// Hand the code to Evolution rather than spending it here.
	//
	// This panel used to POST /v1/register/exchange itself, keep the api_key,
	// and then give Evolution the key. Evolution's GET /license/activate
	// looks like it takes either — _58 (pkg/core/c0.go:891) falls back from
	// code to key — but the route never reaches that: it POSTs whatever
	// arrives in ?code= to /v1/register/exchange first and returns the
	// licensing server's status on failure (c0.go:786-806). A key is not an
	// authorization code, so every activation answered 401 while the
	// registration itself was already marked completed upstream — the licence
	// was issued and nothing here could use it.
	//
	// So the code goes through untouched and Evolution does the one exchange
	// it is allowed. The cost is that the api_key never passes through this
	// panel: Evolution stores it in its own database and /license/status only
	// reports it masked (c0.go:694). Tier and CustomerID are unknown here for
	// the same reason.
	if err := c.activate(ctx, code); err != nil {
		return activation, err
	}
	return activation, nil
}

// ReactivateLicense is kept for the caller's shape and cannot work through
// this route.
//
// The idea was that a rebuild which loses Evolution's database volume could be
// handed back a key this panel had kept. Evolution offers nowhere to put one:
// GET /license/activate exchanges its ?code= with the licensing server before
// anything else (pkg/core/c0.go:786), so a key arrives as an authorization
// code and is refused. The only place Evolution accepts a bare key is at
// startup, from GLOBAL_API_KEY (c0.go:476) — an environment variable, which a
// running container cannot give itself.
//
// A rebuild that loses that volume therefore needs a new registration, which
// in automatic mode the wizard performs by itself with no one asked anything.
func (c *Client) ReactivateLicense(ctx context.Context, apiKey string) error {
	return fmt.Errorf("a Evolution só aceita uma chave de licença em GLOBAL_API_KEY, na subida do processo")
}

// activate gives Evolution the one-time authorization code from the callback.
// Evolution exchanges it with the licensing server, stores what comes back and
// starts serving. The code is all this route accepts, despite its ?code= also
// being where a bare key would go: the exchange happens before anything else.
func (c *Client) activate(ctx context.Context, code string) error {
	endpoint, err := url.JoinPath(c.baseURL, "/license/activate")
	if err != nil {
		return err
	}
	params := url.Values{"code": []string{code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Evolution /license/activate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Evolution /license/activate returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("resposta da Evolution: %w", err)
	}
	if out.Status != "active" {
		return fmt.Errorf("ativação devolveu status %q", out.Status)
	}
	return nil
}
