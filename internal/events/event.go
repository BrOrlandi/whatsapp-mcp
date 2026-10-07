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

// DecoderVersion identifies the shape this package reads. Raise it whenever a
// fix changes what Decode produces from the same bytes: the gateway compares it
// against the version stamped in the database and rebuilds the message
// projection when they differ, so a decoder correction repairs the past on its
// own rather than leaving a hole only someone who noticed would fix.
//
// 1: read the Evolution API v2 shape, which Evolution Go does not publish, so
//
//	every live message landed without instance, chat, sender or timestamp.
//
// 2: reads whatsmeow's own structs, and walks history sync conversations.
//
// 3: reads quotes, mentions, forwards, reactions, polls and the file of a
// media message; applies edits and deletions to the message they change
// instead of indexing them as empty messages; reads each conversation's
// unread count and flags from a history sync, and the account's own read
// receipts.
const DecoderVersion = 3

// Kind classifies a decoded event so the consumer knows what it changes: the
// message index, the connection state, or neither.
type Kind string

const (
	KindMessage    Kind = "message"
	KindHistory    Kind = "history"
	KindConnection Kind = "connection"
	KindReceipt    Kind = "receipt"
	KindOther      Kind = "other"
)

type storeMessage = store.Message

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
	// Live is a live message as it arrived, even when it is an edit or a
	// deletion that changes another row instead of adding its own; Details
	// are what it carries beyond its row. Both are for webhooks.
	Live    *store.Message
	Details *Details
	// Receipt is a receipt event, for webhooks.
	Receipt *Receipt
}

// Receipt says messages were delivered, read or played. Type is WhatsApp's:
// "" (delivered), "read", "read-self" (the account read them on another
// device), "played"…
type Receipt struct {
	ChatJID    string
	SenderJID  string
	FromMe     bool
	IsGroup    bool
	MessageIDs []string
	Type       string
	Timestamp  time.Time
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
		if message, id, aliases, details := decodeLive(env.Data, instance); message != nil {
			decoded.Details, decoded.Live = &details, message
			decoded.Record.Aliases = aliases
			decoded.Record.ID = eventID(instance, name, id, raw)
			switch {
			case details.RevokeOf != "":
				decoded.Record.Edits = []store.Edit{{InstanceID: instance, ChatJID: message.ChatJID, MessageID: details.RevokeOf, Revoked: true,
					SenderJID: message.SenderJID, FromMe: message.FromMe}}
			case details.EditOf != "":
				decoded.Record.Edits = []store.Edit{{InstanceID: instance, ChatJID: message.ChatJID, MessageID: details.EditOf, Text: details.EditText,
					SenderJID: message.SenderJID, FromMe: message.FromMe}}
			default:
				decoded.Record.Messages = []store.Message{*message}
			}
			// Writing in a chat is reading it: whatever came before was seen.
			if message.FromMe && !message.SentAt.IsZero() {
				decoded.Record.Reads = []store.ChatRead{{InstanceID: instance, ChatJID: message.ChatJID, At: message.SentAt}}
			}
		}
	case name == "HistorySync":
		decoded.Kind = KindHistory
		var edits []store.Edit
		decoded.Record.Messages, edits = decodeHistory(env.Data, instance)
		decoded.Record.Edits = edits
		decoded.Record.Aliases = historyAliases(env.Data, instance)
		decoded.Record.Chats = historyChats(env.Data, instance)
	case name == "Receipt":
		decoded.Kind = KindReceipt
		decoded.Record.Skip = true
		if receipt := decodeReceipt(env.Data); receipt != nil {
			decoded.Receipt = receipt
			if receipt.Type == "read-self" && !receipt.Timestamp.IsZero() {
				decoded.Record.Reads = []store.ChatRead{{InstanceID: instance, ChatJID: receipt.ChatJID, At: receipt.Timestamp}}
			}
		}
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
	// SenderAlt and RecipientAlt carry the phone-number JID of a chat that
	// WhatsApp addresses by LID: the sender's for a message received, the
	// recipient's for one the account sent.
	SenderAlt    string `json:"SenderAlt"`
	RecipientAlt string `json:"RecipientAlt"`
}

// liveAlias pairs a one-to-one LID chat with its phone number, when the
// message reveals it.
func liveAlias(info messageInfo, instance string) (store.Alias, bool) {
	if info.IsGroup || !strings.HasSuffix(info.Chat, "@lid") {
		return store.Alias{}, false
	}
	pn := info.SenderAlt
	if info.IsFromMe {
		pn = info.RecipientAlt
	}
	pn = bareJID(pn)
	if !strings.HasSuffix(pn, "@s.whatsapp.net") {
		return store.Alias{}, false
	}
	return store.Alias{InstanceID: instance, LID: info.Chat, PN: pn}, true
}

// bareJID drops the device part of a JID, "5511…:12@s.whatsapp.net", which
// names one of a person's devices rather than the person.
func bareJID(jid string) string {
	user, server, found := strings.Cut(jid, "@")
	if !found {
		return jid
	}
	user, _, _ = strings.Cut(user, ":")
	return user + "@" + server
}

func decodeLiveMessage(data json.RawMessage, instance string) (*store.Message, string) {
	message, id, _, _ := decodeLive(data, instance)
	return message, id
}

func decodeLive(data json.RawMessage, instance string) (*store.Message, string, []store.Alias, Details) {
	if len(data) == 0 {
		return nil, "", nil, Details{}
	}
	var payload struct {
		Info    messageInfo     `json:"Info"`
		Message json.RawMessage `json:"Message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, "", nil, Details{}
	}
	if payload.Info.ID == "" {
		return nil, "", nil, Details{}
	}
	var aliases []store.Alias
	if alias, ok := liveAlias(payload.Info, instance); ok {
		aliases = append(aliases, alias)
	}
	details := ReadDetails(payload.Message)
	message := &store.Message{
		InstanceID: instance,
		MessageID:  payload.Info.ID,
		ChatJID:    payload.Info.Chat,
		SenderJID:  payload.Info.Sender,
		SenderName: payload.Info.PushName,
		FromMe:     payload.Info.IsFromMe,
		IsGroup:    payload.Info.IsGroup,
		SentAt:     parseTimestamp(payload.Info.Timestamp),
	}
	details.apply(message)
	return message, payload.Info.ID, aliases, details
}

// decodeHistory walks the conversations WhatsApp delivers in a history sync.
// Each entry is a WebMessageInfo, the protobuf shape, so its keys are the
// lowercase protobuf names rather than whatsmeow's Go field names. Edits and
// deletions are returned apart, to be applied to the messages they change.
func decodeHistory(data json.RawMessage, instance string) ([]store.Message, []store.Edit) {
	if len(data) == 0 {
		return nil, nil
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
		return nil, nil
	}
	var messages []store.Message
	var edits []store.Edit
	for _, conversation := range payload.Data.Conversations {
		for _, entry := range conversation.Messages {
			record := entry.Message
			if record.Key.ID == "" {
				continue
			}
			chat := first(record.Key.RemoteJID, conversation.ID)
			// The sender of a one-to-one message is the chat itself, unless the
			// account sent it: the record does not name the account.
			sender := record.Key.Participant
			if sender == "" && !record.Key.FromMe {
				sender = chat
			}
			details := ReadDetails(record.Message)
			switch {
			case details.RevokeOf != "":
				edits = append(edits, store.Edit{InstanceID: instance, ChatJID: chat, MessageID: details.RevokeOf, Revoked: true, SenderJID: sender, FromMe: record.Key.FromMe})
				continue
			case details.EditOf != "":
				edits = append(edits, store.Edit{InstanceID: instance, ChatJID: chat, MessageID: details.EditOf, Text: details.EditText, SenderJID: sender, FromMe: record.Key.FromMe})
				continue
			}
			message := store.Message{
				InstanceID: instance,
				MessageID:  record.Key.ID,
				ChatJID:    chat,
				SenderJID:  sender,
				SenderName: record.PushName,
				FromMe:     record.Key.FromMe,
				IsGroup:    strings.HasSuffix(chat, "@g.us"),
				SentAt:     parseNumericTimestamp(record.MessageTimestamp),
			}
			details.apply(&message)
			messages = append(messages, message)
		}
	}
	return messages, edits
}

// historyChats reads what a history sync says about each conversation: its
// unread count, whether it is archived, pinned, muted or marked unread, and
// its name, as of the conversation's timestamp.
func historyChats(data json.RawMessage, instance string) []store.ChatState {
	var payload struct {
		Data struct {
			Conversations []struct {
				ID                    string          `json:"id"`
				Name                  string          `json:"name"`
				UnreadCount           *uint32         `json:"unreadCount"`
				MarkedAsUnread        *bool           `json:"markedAsUnread"`
				Archived              *bool           `json:"archived"`
				Pinned                *flexUint       `json:"pinned"`
				MuteEndTime           *flexUint       `json:"muteEndTime"`
				ConversationTimestamp json.RawMessage `json:"conversationTimestamp"`
			} `json:"conversations"`
		} `json:"Data"`
	}
	if len(data) == 0 || json.Unmarshal(data, &payload) != nil {
		return nil
	}
	var chats []store.ChatState
	for _, c := range payload.Data.Conversations {
		if c.ID == "" || strings.HasSuffix(c.ID, "@broadcast") || strings.HasSuffix(c.ID, "@newsletter") {
			continue
		}
		// A field the sync leaves out says nothing, so it stays nil and the
		// stored value stands.
		state := store.ChatState{InstanceID: instance, ChatJID: c.ID, MarkedUnread: c.MarkedAsUnread, Archived: c.Archived,
			Name: c.Name, SnapshotAt: parseNumericTimestamp(c.ConversationTimestamp)}
		if c.Pinned != nil {
			pinned := *c.Pinned > 0
			state.Pinned = &pinned
		}
		if c.MuteEndTime != nil {
			end := time.Time{}
			if *c.MuteEndTime > 0 {
				end = muteEnd(uint64(*c.MuteEndTime))
			}
			state.MutedUntil = &end
		}
		if c.UnreadCount != nil {
			state.HasUnread, state.UnreadCount = true, int(*c.UnreadCount)
		}
		chats = append(chats, state)
	}
	return chats
}

// muteEnd reads a mute's end: -1 (all ones) is forever, otherwise epoch
// seconds or milliseconds.
func muteEnd(v uint64) time.Time {
	if int64(v) < 0 {
		return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if v > 1e12 {
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), 0).UTC()
}

// decodeReceipt reads whatsmeow's events.Receipt, which Evolution publishes
// with Go field names: the message source flattened, then the ids, the time
// and the type.
func decodeReceipt(data json.RawMessage) *Receipt {
	var payload struct {
		Chat       string   `json:"Chat"`
		Sender     string   `json:"Sender"`
		IsFromMe   bool     `json:"IsFromMe"`
		IsGroup    bool     `json:"IsGroup"`
		MessageIDs []string `json:"MessageIDs"`
		Timestamp  string   `json:"Timestamp"`
		Type       string   `json:"Type"`
	}
	if len(data) == 0 || json.Unmarshal(data, &payload) != nil || payload.Chat == "" || len(payload.MessageIDs) == 0 {
		return nil
	}
	return &Receipt{ChatJID: bareJID(payload.Chat), SenderJID: bareJID(payload.Sender), FromMe: payload.IsFromMe, IsGroup: payload.IsGroup,
		MessageIDs: payload.MessageIDs, Type: payload.Type, Timestamp: parseTimestamp(payload.Timestamp)}
}

// historyAliases pairs every LID conversation of a history sync with the
// phone number WhatsApp names for it.
func historyAliases(data json.RawMessage, instance string) []store.Alias {
	var payload struct {
		Data struct {
			Conversations []struct {
				ID    string `json:"id"`
				PnJID string `json:"pnJID"`
			} `json:"conversations"`
		} `json:"Data"`
	}
	if len(data) == 0 || json.Unmarshal(data, &payload) != nil {
		return nil
	}
	var aliases []store.Alias
	for _, conversation := range payload.Data.Conversations {
		pn := bareJID(conversation.PnJID)
		if strings.HasSuffix(conversation.ID, "@lid") && strings.HasSuffix(pn, "@s.whatsapp.net") {
			aliases = append(aliases, store.Alias{InstanceID: instance, LID: conversation.ID, PN: pn})
		}
	}
	return aliases
}

// messageText extracts the searchable text of a message. Media without a
// caption has none, which is expected and not an error.
func messageText(raw json.RawMessage) string { return ReadDetails(raw).Text }

// mediaType records what kind of message arrived, so an audio message can be
// found later without reparsing the raw payload.
func mediaType(raw json.RawMessage) string { return ReadDetails(raw).MediaType }

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

// MessageContent finds the WhatsApp message protobuf of one message inside a
// stored event payload, which is what Evolution needs back to decode media.
//
// A live event carries one message at data.Message. A history sync carries
// whole conversations, and the message is one entry among them, found by its
// id. Reading only the live shape made every voice note that arrived through
// a history sync look as if it carried no media.
func MessageContent(payload []byte, messageID string) json.RawMessage {
	var event struct {
		Data struct {
			Message json.RawMessage `json:"Message"`
			Data    struct {
				Conversations []struct {
					Messages []struct {
						Message struct {
							Key struct {
								ID string `json:"id"`
							} `json:"key"`
							Message json.RawMessage `json:"message"`
						} `json:"message"`
					} `json:"messages"`
				} `json:"conversations"`
			} `json:"Data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	if len(event.Data.Message) > 0 && string(event.Data.Message) != "null" {
		return event.Data.Message
	}
	for _, conversation := range event.Data.Data.Conversations {
		for _, entry := range conversation.Messages {
			if entry.Message.Key.ID == messageID && len(entry.Message.Message) > 0 && string(entry.Message.Message) != "null" {
				return entry.Message.Message
			}
		}
	}
	return nil
}
