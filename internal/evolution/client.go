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
	// Token is the credential Evolution resolves this instance from. The
	// listing carries it, which is what lets the panel adopt an instance it did
	// not create instead of forcing a fresh pairing. It is an internal secret
	// and never leaves the backend.
	Token string `json:"-"`
}

func New(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: timeout}}
}

func (c *Client) FetchInstances(ctx context.Context) ([]Instance, error) {
	endpoint, err := url.JoinPath(c.baseURL, "/instance/all")
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
	var envelope struct {
		Data []struct {
			ID               string `json:"id"`
			Name             string `json:"name"`
			Token            string `json:"token"`
			InstanceName     string `json:"instanceName"`
			InstanceID       string `json:"instanceId"`
			OwnerJID         string `json:"ownerJid"`
			JID              string `json:"jid"`
			Connected        bool   `json:"connected"`
			State            string `json:"state"`
			ConnectionStatus string `json:"connectionStatus"`
			Instance         struct {
				ID               string `json:"id"`
				InstanceID       string `json:"instanceId"`
				Token            string `json:"token"`
				Name             string `json:"name"`
				InstanceName     string `json:"instanceName"`
				OwnerJID         string `json:"ownerJid"`
				JID              string `json:"jid"`
				Connected        bool   `json:"connected"`
				State            string `json:"state"`
				Status           string `json:"status"`
				ConnectionStatus string `json:"connectionStatus"`
			} `json:"instance"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode Evolution instances: %w", err)
	}
	result := make([]Instance, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		id := first(item.ID, item.InstanceID, item.Instance.ID, item.Instance.InstanceID, item.Name, item.InstanceName, item.Instance.Name, item.Instance.InstanceName)
		name := first(item.Name, item.InstanceName, item.Instance.Name, item.Instance.InstanceName, id)
		jid := first(item.OwnerJID, item.JID, item.Instance.OwnerJID, item.Instance.JID)
		number, _, _ := strings.Cut(jid, "@")
		state := first(item.ConnectionStatus, item.State, item.Instance.ConnectionStatus, item.Instance.State, item.Instance.Status)
		status := normalizeStatus(state)
		if item.Connected || item.Instance.Connected {
			status = StatusConnected
		}
		result = append(result, Instance{ID: id, Name: name, Number: number, Status: status, Token: first(item.Token, item.Instance.Token)})
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
