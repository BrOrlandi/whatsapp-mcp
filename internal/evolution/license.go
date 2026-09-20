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
//
// The registration URL may only be generated once per process and Evolution
// keeps the first one it generated, so the redirect the caller wants has to be
// there from the very first call. An empty callback leaves the redirect to
// Evolution's own manager, which is where a manual registration lands.
func (c *Client) License(ctx context.Context, callback string) (License, error) {
	var status License
	if err := c.getJSON(ctx, "/license/status", &status); err != nil {
		return License{}, err
	}
	if status.Activated() {
		return status, nil
	}
	// The query travels as its own piece because JoinPath would escape a '?'
	// written into a path segment, which is how a redirect became
	// `%3Fredirect_uri=...` and stopped being one.
	endpoint, err := url.JoinPath(c.baseURL, "/license/register")
	if err != nil {
		return status, err
	}
	if callback != "" {
		endpoint += "?" + url.Values{"redirect_uri": []string{callback}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return status, err
	}
	req.Header.Set("apikey", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return status, fmt.Errorf("Evolution /license/register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return status, fmt.Errorf("Evolution /license/register returned HTTP %d", resp.StatusCode)
	}
	var registration License
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return status, err
	}
	status.RegisterURL = registration.RegisterURL
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
