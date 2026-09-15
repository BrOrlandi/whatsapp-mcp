// Package events decodes the payloads Evolution Go publishes to RabbitMQ.
//
// Evolution Go wraps whatsmeow's own event structs, so the shapes here follow
// whatsmeow rather than the Evolution API v2 documented elsewhere: live
// messages arrive as {"event":"Message","data":{"Info":{...},"Message":{...}}}
// with Go field names, while history sync arrives as WhatsApp's own
// WebMessageInfo records nested under conversations.
package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Kind classifies a decoded event so the consumer knows what it changes: the
// message index, the connection state, or neither.
type Kind string

const (
	KindMessage    Kind = "message"
	KindHistory    Kind = "history"
	KindConnection Kind = "connection"
	KindOther      Kind = "other"
)

// ConnectionState is the WhatsApp session state a connection event reports.
type ConnectionState string

const (
	StateConnected    ConnectionState = "connected"
	StateDisconnected ConnectionState = "disconnected"
	StateLoggedOut    ConnectionState = "logged_out"
	StateBanned       ConnectionState = "banned"
	StateFailed       ConnectionState = "failed"
	StatePairing      ConnectionState = "pairing"
)

// Connection carries what a connection event says about the session, including
// the reason Evolution gave for losing it.
type Connection struct {
	State    ConnectionState
	Reason   string
	JID      string
	PushName string
}

// Event is one decoded Evolution payload. A single event can carry many
// messages, because a history sync delivers whole conversations at once.
type Event struct {
	Record     store.Event
	Kind       Kind
	Connection *Connection
}

// Decode turns one RabbitMQ payload into an event ready to persist. The raw
// bytes are always preserved, so an event this package does not understand is
// still stored and can be reinterpreted later.
func Decode(raw []byte) (Event, error) {
	var env struct {
		Event        string          `json:"event"`
		Type         string          `json:"type"`
		InstanceID   string          `json:"instanceId"`
		InstanceName string          `json:"instanceName"`
		Instance     string          `json:"instance"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	name := first(env.Event, env.Type, "Unknown")
	instance := first(env.InstanceID, env.Instance)
	decoded := Event{
		Record: store.Event{Type: name, InstanceID: instance, Payload: raw, ReceivedAt: time.Now().UTC()},
		Kind:   KindOther,
	}

	switch {
	case name == "Message" || name == "SendMessage":
		decoded.Kind = KindMessage
		if message, id := decodeLiveMessage(env.Data, instance); message != nil {
			decoded.Record.Messages = []store.Message{*message}
			decoded.Record.ID = eventID(instance, name, id, raw)
		}
	case name == "HistorySync":
		decoded.Kind = KindHistory
		decoded.Record.Messages = decodeHistory(env.Data, instance)
	case connectionStates[name] != "":
		decoded.Kind = KindConnection
		decoded.Connection = decodeConnection(name, env.Data)
	}
	if decoded.Record.ID == "" {
		decoded.Record.ID = eventID(instance, name, "", raw)
	}
	return decoded, nil
}

// eventID keeps deliveries idempotent. A message keeps its WhatsApp id so a
// redelivery collapses onto the same row; anything else is identified by the
// content hash, which is the only stable handle Evolution offers.
func eventID(instance, name, messageID string, raw []byte) string {
	if messageID != "" {
		return instance + ":" + name + ":" + messageID
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// messageInfo mirrors whatsmeow's types.MessageInfo, which carries no JSON tags
// and therefore serialises with Go field names.
type messageInfo struct {
	ID        string `json:"ID"`
	Chat      string `json:"Chat"`
	Sender    string `json:"Sender"`
	IsFromMe  bool   `json:"IsFromMe"`
	IsGroup   bool   `json:"IsGroup"`
	PushName  string `json:"PushName"`
	Timestamp string `json:"Timestamp"`
}

func decodeLiveMessage(data json.RawMessage, instance string) (*store.Message, string) {
	if len(data) == 0 {
		return nil, ""
	}
	var payload struct {
		Info    messageInfo     `json:"Info"`
		Message json.RawMessage `json:"Message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, ""
	}
	if payload.Info.ID == "" {
		return nil, ""
	}
	return &store.Message{
		InstanceID: instance,
		MessageID:  payload.Info.ID,
		ChatJID:    payload.Info.Chat,
		SenderJID:  payload.Info.Sender,
		SenderName: payload.Info.PushName,
		FromMe:     payload.Info.IsFromMe,
		IsGroup:    payload.Info.IsGroup,
		Text:       messageText(payload.Message),
		MediaType:  mediaType(payload.Message),
		SentAt:     parseTimestamp(payload.Info.Timestamp),
	}, payload.Info.ID
}

// decodeHistory walks the conversations WhatsApp delivers in a history sync.
// Each entry is a WebMessageInfo, the protobuf shape, so its keys are the
// lowercase protobuf names rather than whatsmeow's Go field names.
func decodeHistory(data json.RawMessage, instance string) []store.Message {
	if len(data) == 0 {
		return nil
	}
	var payload struct {
		Data struct {
			Conversations []struct {
				ID       string `json:"id"`
				Messages []struct {
					Message struct {
						Key struct {
							ID          string `json:"id"`
							RemoteJID   string `json:"remoteJid"`
							FromMe      bool   `json:"fromMe"`
							Participant string `json:"participant"`
						} `json:"key"`
						Message          json.RawMessage `json:"message"`
						MessageTimestamp json.RawMessage `json:"messageTimestamp"`
						PushName         string          `json:"pushName"`
					} `json:"message"`
				} `json:"messages"`
			} `json:"conversations"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	var messages []store.Message
	for _, conversation := range payload.Data.Conversations {
		for _, entry := range conversation.Messages {
			record := entry.Message
			if record.Key.ID == "" {
				continue
			}
			chat := first(record.Key.RemoteJID, conversation.ID)
			messages = append(messages, store.Message{
				InstanceID: instance,
				MessageID:  record.Key.ID,
				ChatJID:    chat,
				SenderJID:  first(record.Key.Participant, chat),
				SenderName: record.PushName,
				FromMe:     record.Key.FromMe,
				IsGroup:    strings.HasSuffix(chat, "@g.us"),
				Text:       messageText(record.Message),
				MediaType:  mediaType(record.Message),
				SentAt:     parseNumericTimestamp(record.MessageTimestamp),
			})
		}
	}
	return messages
}

// content mirrors the parts of WhatsApp's message protobuf this gateway reads.
// Wrappers repeat the same shape, so unwrapping is recursive with a depth
// bound: the payload is remote input and must not drive unbounded recursion.
type content struct {
	Conversation string `json:"conversation"`
	ExtendedText *struct {
		Text string `json:"text"`
	} `json:"extendedTextMessage"`
	Image *struct {
		Caption string `json:"caption"`
	} `json:"imageMessage"`
	Video *struct {
		Caption string `json:"caption"`
	} `json:"videoMessage"`
	Document *struct {
		Caption  string `json:"caption"`
		FileName string `json:"fileName"`
	} `json:"documentMessage"`
	Audio    *json.RawMessage `json:"audioMessage"`
	Sticker  *json.RawMessage `json:"stickerMessage"`
	Location *json.RawMessage `json:"locationMessage"`
	Contact  *struct {
		DisplayName string `json:"displayName"`
	} `json:"contactMessage"`
	Ephemeral *struct {
		Message json.RawMessage `json:"message"`
	} `json:"ephemeralMessage"`
	ViewOnce *struct {
		Message json.RawMessage `json:"message"`
	} `json:"viewOnceMessage"`
	ViewOnceV2 *struct {
		Message json.RawMessage `json:"message"`
	} `json:"viewOnceMessageV2"`
	DocumentWithCaption *struct {
		Message json.RawMessage `json:"message"`
	} `json:"documentWithCaptionMessage"`
}

// unwrap returns the innermost message, following the wrappers WhatsApp uses
// for disappearing, view-once and captioned-document messages.
func unwrap(raw json.RawMessage, depth int) (content, json.RawMessage) {
	var parsed content
	if len(raw) == 0 || depth > 4 {
		return parsed, raw
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return content{}, raw
	}
	for _, wrapper := range []*struct {
		Message json.RawMessage `json:"message"`
	}{parsed.Ephemeral, parsed.ViewOnce, parsed.ViewOnceV2, parsed.DocumentWithCaption} {
		if wrapper != nil && len(wrapper.Message) > 0 {
			return unwrap(wrapper.Message, depth+1)
		}
	}
	return parsed, raw
}

// messageText extracts the searchable text of a message. Media without a
// caption has none, which is expected and not an error.
func messageText(raw json.RawMessage) string {
	parsed, _ := unwrap(raw, 0)
	if parsed.Conversation != "" {
		return parsed.Conversation
	}
	if parsed.ExtendedText != nil && parsed.ExtendedText.Text != "" {
		return parsed.ExtendedText.Text
	}
	if parsed.Image != nil && parsed.Image.Caption != "" {
		return parsed.Image.Caption
	}
	if parsed.Video != nil && parsed.Video.Caption != "" {
		return parsed.Video.Caption
	}
	if parsed.Document != nil {
		return first(parsed.Document.Caption, parsed.Document.FileName)
	}
	if parsed.Contact != nil {
		return parsed.Contact.DisplayName
	}
	return ""
}

// mediaType records what kind of message arrived, so an audio message can be
// found later without reparsing the raw payload.
func mediaType(raw json.RawMessage) string {
	parsed, _ := unwrap(raw, 0)
	switch {
	case parsed.Audio != nil:
		return "audio"
	case parsed.Image != nil:
		return "image"
	case parsed.Video != nil:
		return "video"
	case parsed.Document != nil:
		return "document"
	case parsed.Sticker != nil:
		return "sticker"
	case parsed.Location != nil:
		return "location"
	case parsed.Contact != nil:
		return "contact"
	case parsed.Conversation != "" || parsed.ExtendedText != nil:
		return "text"
	}
	return ""
}

// connectionStates maps Evolution's connection event names to the session state
// each one implies.
var connectionStates = map[string]ConnectionState{
	"Connected":      StateConnected,
	"PairSuccess":    StatePairing,
	"Disconnected":   StateDisconnected,
	"LoggedOut":      StateLoggedOut,
	"ConnectFailure": StateFailed,
	"TemporaryBan":   StateBanned,
}

func decodeConnection(name string, data json.RawMessage) *Connection {
	update := &Connection{State: connectionStates[name]}
	if len(data) == 0 {
		return update
	}
	var payload struct {
		Reason   string `json:"reason"`
		JID      string `json:"jid"`
		PushName string `json:"pushName"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		update.Reason, update.JID, update.PushName = payload.Reason, payload.JID, payload.PushName
	}
	return update
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// parseTimestamp reads whatsmeow's RFC 3339 timestamps, falling back to the
// epoch seconds the protobuf records use.
func parseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC()
	}
	return time.Time{}
}

func parseNumericTimestamp(raw json.RawMessage) time.Time {
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
		return parseTimestamp(value)
	}
	return time.Time{}
}
