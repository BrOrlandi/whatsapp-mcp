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

type ConnectionStatus string

const (
	StatusConnected    ConnectionStatus = "connected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusDisconnected ConnectionStatus = "disconnected"
)

type Instance struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Number string           `json:"number,omitempty"`
	Status ConnectionStatus `json:"status"`
}

func New(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: timeout}}
}

func (c *Client) FetchInstances(ctx context.Context) ([]Instance, error) {
	endpoint, err := url.JoinPath(c.baseURL, "/instance/fetchInstances")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Evolution instances: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Evolution instances returned HTTP %d", resp.StatusCode)
	}
	var raw []struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		InstanceName     string `json:"instanceName"`
		InstanceID       string `json:"instanceId"`
		OwnerJID         string `json:"ownerJid"`
		State            string `json:"state"`
		ConnectionStatus string `json:"connectionStatus"`
		Instance         struct {
			ID               string `json:"id"`
			InstanceID       string `json:"instanceId"`
			Name             string `json:"name"`
			InstanceName     string `json:"instanceName"`
			OwnerJID         string `json:"ownerJid"`
			State            string `json:"state"`
			Status           string `json:"status"`
			ConnectionStatus string `json:"connectionStatus"`
		} `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode Evolution instances: %w", err)
	}
	result := make([]Instance, 0, len(raw))
	for _, item := range raw {
		id := first(item.ID, item.InstanceID, item.Instance.ID, item.Instance.InstanceID, item.Name, item.InstanceName, item.Instance.Name, item.Instance.InstanceName)
		name := first(item.Name, item.InstanceName, item.Instance.Name, item.Instance.InstanceName, id)
		jid := first(item.OwnerJID, item.Instance.OwnerJID)
		number, _, _ := strings.Cut(jid, "@")
		state := first(item.ConnectionStatus, item.State, item.Instance.ConnectionStatus, item.Instance.State, item.Instance.Status)
		result = append(result, Instance{ID: id, Name: name, Number: number, Status: normalizeStatus(state)})
	}
	return result, nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func normalizeStatus(value string) ConnectionStatus {
	switch strings.ToLower(value) {
	case "open", "online", "connected":
		return StatusConnected
	case "connecting", "pairing":
		return StatusConnecting
	default:
		return StatusDisconnected
	}
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
