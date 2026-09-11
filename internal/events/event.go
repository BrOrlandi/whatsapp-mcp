package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type envelope struct {
	ID       string `json:"id"`
	EventID  string `json:"event_id"`
	Event    string `json:"event"`
	Type     string `json:"type"`
	Instance string `json:"instance"`
	Data     struct {
		ID  string `json:"id"`
		Key struct {
			ID        string `json:"id"`
			RemoteJID string `json:"remoteJid"`
			FromMe    bool   `json:"fromMe"`
		} `json:"key"`
		PushName         string          `json:"pushName"`
		MessageTimestamp json.RawMessage `json:"messageTimestamp"`
		Message          struct {
			Conversation string `json:"conversation"`
			ExtendedText struct {
				Text string `json:"text"`
			} `json:"extendedTextMessage"`
		} `json:"message"`
	} `json:"data"`
}

func Decode(raw []byte) (store.Event, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return store.Event{}, fmt.Errorf("decode event: %w", err)
	}
	id := first(env.EventID, env.ID, env.Data.Key.ID, env.Data.ID)
	if id == "" {
		sum := sha256.Sum256(raw)
		id = "sha256:" + hex.EncodeToString(sum[:])
	}
	e := store.Event{ID: id, Type: first(env.Event, env.Type, "unknown"), InstanceID: env.Instance, Payload: raw, ReceivedAt: time.Now().UTC()}
	text := first(env.Data.Message.Conversation, env.Data.Message.ExtendedText.Text)
	if env.Data.Key.ID != "" || text != "" {
		e.Message = &store.Message{InstanceID: env.Instance, MessageID: first(env.Data.Key.ID, id), ChatJID: env.Data.Key.RemoteJID, SenderName: env.Data.PushName, FromMe: env.Data.Key.FromMe, Text: text, SentAt: parseTimestamp(env.Data.MessageTimestamp)}
	}
	return e, nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func parseTimestamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		if seconds, err := strconv.ParseInt(string(number), 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
