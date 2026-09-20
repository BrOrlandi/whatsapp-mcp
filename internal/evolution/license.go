package evolution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotActivated reports the one failure that looks like an outage and is not
// one: Evolution Go needs a licence, and answers 503 on every route until it
// has one. Telling the operator to try again in a moment is wrong — nothing
// will change until somebody registers it — so this is a distinct error and
// the panel says what to do about it.
var ErrNotActivated = errors.New("Evolution Go is not activated yet")

// License is what Evolution reports about its own activation, and the URL that
// starts it.
type License struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	// RegisterURL is where the operator activates this installation. It comes
	// from Evolution itself and is only present while activation is pending.
	RegisterURL string `json:"register_url"`
}

// Activated reports whether Evolution will answer its API at all.
func (l License) Activated() bool { return strings.EqualFold(l.Status, "active") }

// License asks Evolution about its activation, and for the registration URL
// when it has not been activated. Both routes answer without the API key,
// which is what makes this readable while everything else is refusing.
func (c *Client) License(ctx context.Context) (License, error) {
	var status License
	if err := c.getJSON(ctx, "/license/status", &status); err != nil {
		return License{}, err
	}
	if status.Activated() {
		return status, nil
	}
	var registration License
	if err := c.getJSON(ctx, "/license/register", &registration); err == nil {
		status.RegisterURL = registration.RegisterURL
	}
	return status, nil
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	endpoint, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Evolution %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Evolution %s returned HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
