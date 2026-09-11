package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL, apiKey string
	http            *http.Client
}

func New(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: timeout}}
}

func (c *Client) Status(ctx context.Context) (bool, string, error) {
	endpoint, err := url.JoinPath(c.baseURL, "/instance/status")
	if err != nil {
		return false, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("apikey", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, "", fmt.Errorf("Evolution status returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		State    string `json:"state"`
		Status   string `json:"status"`
		Instance struct {
			State  string `json:"state"`
			Status string `json:"status"`
		} `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, "", fmt.Errorf("decode Evolution status: %w", err)
	}
	state := payload.State
	if state == "" {
		state = payload.Status
	}
	if state == "" {
		state = payload.Instance.State
	}
	if state == "" {
		state = payload.Instance.Status
	}
	state = strings.ToLower(state)
	return state == "connected" || state == "open" || state == "online", state, nil
}
