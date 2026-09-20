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

// LicenseActivation is what a completed registration yields: the credential
// issued by the licensing server, which is what activating Evolution Go means.
// The panel keeps a copy so a rebuild that loses Evolution's own database can
// be reactivated without asking the operator to register again.
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
// in the link the operator clicked; only the licensing server can judge it, and
// it can be used exactly once, so this is also the step that takes custody of
// the api_key for the panel's own records. Activating with the key rather than
// the code is deliberate: Evolution accepts either, and this way the code is
// spent here while the key stays reusable for a rebuild.
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
	body, err := json.Marshal(map[string]string{"authorization_code": code, "instance_id": status.InstanceID})
	if err != nil {
		return activation, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, licensingServer+"/v1/register/exchange", strings.NewReader(string(body)))
	if err != nil {
		return activation, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return activation, fmt.Errorf("licensing server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return activation, fmt.Errorf("licensing server returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&activation); err != nil {
		return activation, fmt.Errorf("resposta do licensing server: %w", err)
	}
	activation.InstanceID = status.InstanceID
	if activation.APIKey == "" {
		return activation, fmt.Errorf("licensing server não devolveu a api_key")
	}
	if err := c.activateWithKey(ctx, activation.APIKey); err != nil {
		return activation, err
	}
	return activation, nil
}

// ReactivateLicense brings back a licence the panel already holds. Evolution
// keeps its licence in a database volume that a rebuild can lose, and refuses
// every request until it has one again — handing it a key it already owned is
// enough; no registration, no email, no browser.
func (c *Client) ReactivateLicense(ctx context.Context, apiKey string) error {
	return c.activateWithKey(ctx, apiKey)
}

// activateWithKey hands Evolution a licence credential. The route accepts
// either an authorization code or the key itself, so the panel can use the
// key it had kept.
func (c *Client) activateWithKey(ctx context.Context, apiKey string) error {
	endpoint, err := url.JoinPath(c.baseURL, "/license/activate")
	if err != nil {
		return err
	}
	params := url.Values{"code": []string{apiKey}}
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
