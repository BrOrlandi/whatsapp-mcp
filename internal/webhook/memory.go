package webhook

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Memory keeps webhooks in memory: for the tests and the panel preview, which
// run without a database.
type Memory struct {
	mu    sync.Mutex
	hooks map[string]store.Webhook
}

func NewMemory() *Memory { return &Memory{hooks: map[string]store.Webhook{}} }

func (m *Memory) Webhooks(context.Context) ([]store.Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.Webhook, 0, len(m.hooks))
	for _, h := range m.hooks {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) Webhook(_ context.Context, id string) (store.Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hooks[id]
	if !ok {
		return h, store.ErrNoWebhook
	}
	return h, nil
}

func (m *Memory) SaveWebhook(_ context.Context, w store.Webhook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.hooks[w.ID]; ok {
		old.URL, old.Events, old.IncludeOwn, old.Chats, old.Enabled = w.URL, w.Events, w.IncludeOwn, w.Chats, w.Enabled
		m.hooks[w.ID] = old
		return nil
	}
	m.hooks[w.ID] = w
	return nil
}

func (m *Memory) DeleteWebhook(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.hooks[id]; !ok {
		return store.ErrNoWebhook
	}
	delete(m.hooks, id)
	return nil
}

func (m *Memory) RecordDelivery(_ context.Context, id string, at time.Time, result string, ok bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, found := m.hooks[id]
	if !found {
		return nil
	}
	at = at.UTC()
	h.LastAttemptAt, h.LastResult = &at, result
	if ok {
		h.LastSuccessAt = &at
		h.Delivered++
	}
	m.hooks[id] = h
	return nil
}

func (m *Memory) DisableWebhook(_ context.Context, id string, at time.Time, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hooks[id]; ok {
		at = at.UTC()
		h.Enabled, h.DisabledAt, h.DisabledReason = false, &at, reason
		m.hooks[id] = h
	}
	return nil
}

func (m *Memory) EnableWebhook(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hooks[id]; ok {
		h.Enabled, h.DisabledAt, h.DisabledReason = true, nil, ""
		m.hooks[id] = h
	}
	return nil
}
