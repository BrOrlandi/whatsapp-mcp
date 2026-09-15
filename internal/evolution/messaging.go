package evolution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Contact is one entry of the address book whatsmeow keeps for an instance.
type Contact struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	PushName     string `json:"push_name,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
	Number       string `json:"number,omitempty"`
}

// Group is a WhatsApp group as Evolution reports it.
type Group struct {
	JID          string        `json:"jid"`
	Name         string        `json:"name"`
	Topic        string        `json:"topic,omitempty"`
	OwnerJID     string        `json:"owner_jid,omitempty"`
	Announce     bool          `json:"announce_only,omitempty"`
	Locked       bool          `json:"locked,omitempty"`
	CreatedAt    time.Time     `json:"created_at,omitempty"`
	Participants []Participant `json:"participants,omitempty"`
	MemberCount  int           `json:"member_count"`
}

// Participant is one member of a group.
type Participant struct {
	JID          string `json:"jid"`
	Name         string `json:"name,omitempty"`
	IsAdmin      bool   `json:"is_admin,omitempty"`
	IsSuperAdmin bool   `json:"is_super_admin,omitempty"`
}

// SentMessage identifies a message this gateway sent, so the caller can refer
// to it later.
type SentMessage struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// Media is the decoded content of a media message.
type Media struct {
	MimeType string `json:"mimetype"`
	Base64   string `json:"base64"`
}

// Anchor identifies the message a history sync request pages backwards from.
// Every field is required by Evolution.
type Anchor struct {
	MessageID string
	ChatJID   string
	FromMe    bool
	IsGroup   bool
	Timestamp time.Time
}

// ErrNotConnected reports that the instance has no live WhatsApp session, which
// is a distinct condition from the API being unreachable: it is fixed by
// pairing, not by retrying.
var ErrNotConnected = errors.New("the WhatsApp instance is not connected")

// classify turns Evolution's own wording into the one failure the caller can
// act on differently.
func classify(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "no active session") || strings.Contains(text, "not connected") || strings.Contains(text, "failed to start instance") {
		return errors.Join(ErrNotConnected, err)
	}
	return err
}

// Contacts lists the address book of the instance.
func (c *Client) Contacts(ctx context.Context, token string) ([]Contact, error) {
	var raw []struct {
		JID          string `json:"Jid"`
		Found        bool   `json:"Found"`
		FirstName    string `json:"FirstName"`
		FullName     string `json:"FullName"`
		PushName     string `json:"PushName"`
		BusinessName string `json:"BusinessName"`
	}
	if err := c.call(ctx, http.MethodGet, "/user/contacts", token, nil, &raw); err != nil {
		return nil, classify(err)
	}
	contacts := make([]Contact, 0, len(raw))
	for _, item := range raw {
		number, _, _ := strings.Cut(item.JID, "@")
		contacts = append(contacts, Contact{
			JID:          item.JID,
			Name:         first(item.FullName, item.FirstName, item.PushName, item.BusinessName, number),
			PushName:     item.PushName,
			BusinessName: item.BusinessName,
			Number:       number,
		})
	}
	return contacts, nil
}

// groupPayload mirrors whatsmeow's types.GroupInfo, whose embedded structs
// flatten into the same JSON object and which carries no JSON tags.
type groupPayload struct {
	JID          string `json:"JID"`
	OwnerJID     string `json:"OwnerJID"`
	Name         string `json:"Name"`
	Topic        string `json:"Topic"`
	IsAnnounce   bool   `json:"IsAnnounce"`
	IsLocked     bool   `json:"IsLocked"`
	GroupCreated string `json:"GroupCreated"`
	Participants []struct {
		JID          string `json:"JID"`
		DisplayName  string `json:"DisplayName"`
		IsAdmin      bool   `json:"IsAdmin"`
		IsSuperAdmin bool   `json:"IsSuperAdmin"`
	} `json:"Participants"`
	ParticipantCount int `json:"ParticipantCount"`
}

func (g groupPayload) toGroup() Group {
	group := Group{
		JID: g.JID, Name: g.Name, Topic: g.Topic, OwnerJID: g.OwnerJID,
		Announce: g.IsAnnounce, Locked: g.IsLocked, MemberCount: g.ParticipantCount,
	}
	if created, err := time.Parse(time.RFC3339, g.GroupCreated); err == nil {
		group.CreatedAt = created.UTC()
	}
	for _, participant := range g.Participants {
		group.Participants = append(group.Participants, Participant{
			JID: participant.JID, Name: participant.DisplayName,
			IsAdmin: participant.IsAdmin, IsSuperAdmin: participant.IsSuperAdmin,
		})
	}
	if group.MemberCount == 0 {
		group.MemberCount = len(group.Participants)
	}
	return group
}

// Groups lists the groups the account belongs to.
func (c *Client) Groups(ctx context.Context, token string) ([]Group, error) {
	var raw []groupPayload
	if err := c.call(ctx, http.MethodGet, "/group/list", token, nil, &raw); err != nil {
		return nil, classify(err)
	}
	groups := make([]Group, 0, len(raw))
	for _, item := range raw {
		groups = append(groups, item.toGroup())
	}
	return groups, nil
}

// Group returns one group with its participants.
func (c *Client) Group(ctx context.Context, token, groupJID string) (Group, error) {
	var raw groupPayload
	if err := c.call(ctx, http.MethodPost, "/group/info", token, map[string]any{"groupJid": groupJID}, &raw); err != nil {
		return Group{}, classify(err)
	}
	return raw.toGroup(), nil
}

// sendResult covers the shapes Evolution uses for a send acknowledgement.
type sendResult struct {
	ID        string `json:"ID"`
	LowerID   string `json:"id"`
	Timestamp string `json:"Timestamp"`
	LowerTime string `json:"timestamp"`
}

func (r sendResult) toSent() SentMessage {
	sent := SentMessage{ID: first(r.ID, r.LowerID)}
	for _, value := range []string{r.Timestamp, r.LowerTime} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			sent.Timestamp = parsed.UTC()
			break
		}
	}
	return sent
}

// SendText sends a text message. The recipient is a JID or a phone number;
// Evolution formats it when asked to.
func (c *Client) SendText(ctx context.Context, token, recipient, text string) (SentMessage, error) {
	var result sendResult
	body := map[string]any{"number": recipient, "text": text, "formatJid": true}
	if err := c.call(ctx, http.MethodPost, "/send/text", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// SendMedia sends media fetched from a URL. Evolution downloads the URL itself
// and validates the format against the declared kind.
func (c *Client) SendMedia(ctx context.Context, token, recipient, kind, url, caption, filename string) (SentMessage, error) {
	var result sendResult
	body := map[string]any{"number": recipient, "type": kind, "url": url, "formatJid": true}
	if caption != "" {
		body["caption"] = caption
	}
	if filename != "" {
		body["filename"] = filename
	}
	if err := c.call(ctx, http.MethodPost, "/send/media", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// DownloadMedia decodes the media of a message. The raw protobuf message is
// what Evolution needs to locate the content, which is why the stored payload
// is passed straight through.
func (c *Client) DownloadMedia(ctx context.Context, token string, message json.RawMessage) (Media, error) {
	var media Media
	if err := c.call(ctx, http.MethodPost, "/message/downloadmedia", token, map[string]any{"message": message}, &media); err != nil {
		return Media{}, classify(err)
	}
	return media, nil
}

// RequestHistory asks WhatsApp for the messages immediately before the anchor.
// It returns as soon as the request is sent: the messages themselves arrive
// asynchronously on the historysync queue, so the caller must wait for the
// index to grow rather than for this call to return data.
func (c *Client) RequestHistory(ctx context.Context, token string, anchor Anchor, count int) error {
	if anchor.MessageID == "" || anchor.ChatJID == "" || anchor.Timestamp.IsZero() {
		return errors.New("a history request needs a known message to page back from")
	}
	if count <= 0 || count > 200 {
		count = 50
	}
	body := map[string]any{
		"count": count,
		"messageInfo": map[string]any{
			"Chat":      anchor.ChatJID,
			"IsFromMe":  anchor.FromMe,
			"IsGroup":   anchor.IsGroup,
			"ID":        anchor.MessageID,
			"Timestamp": anchor.Timestamp.UTC().Format(time.RFC3339),
		},
	}
	return classify(c.call(ctx, http.MethodPost, "/chat/history-sync", token, body, nil))
}
