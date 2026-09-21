package evolution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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
//
// Evolution Go returns whatsmeow's own SendResponse, which carries the id and
// the timestamp nested under Info rather than at the top level. Reading only
// the flat fields yielded an empty id on every send, and an empty id is not a
// cosmetic loss: /message/status is keyed by it, so without it there is no way
// to ask whether the message actually arrived. The flat spellings are kept as
// fallbacks because other Evolution builds answer in that shape.
type sendResult struct {
	Info struct {
		ID        string `json:"ID"`
		Timestamp string `json:"Timestamp"`
	} `json:"Info"`
	ID        string `json:"ID"`
	LowerID   string `json:"id"`
	MessageID string `json:"messageId"`
	Timestamp string `json:"Timestamp"`
	LowerTime string `json:"timestamp"`
}

func (r sendResult) toSent() SentMessage {
	sent := SentMessage{ID: first(r.Info.ID, r.ID, r.LowerID)}
	for _, value := range []string{r.Info.Timestamp, r.Timestamp, r.LowerTime} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			sent.Timestamp = parsed.UTC()
			break
		}
	}
	return sent
}

// Delivery is what WhatsApp knows about a message after it was sent. An empty
// Status means Evolution holds no delivery record at all, which is exactly what
// a message that could not be encrypted for the recipient's device looks like.
type Delivery struct {
	MessageID string `json:"message_id,omitempty"`
	Status    string `json:"status,omitempty"`
	At        string `json:"at,omitempty"`
}

// Delivered reports whether WhatsApp acknowledged the message reaching the
// recipient. It is the only honest answer available: Evolution reports a send
// as successful even when it silently skipped a device it could not encrypt
// for, so the send call alone cannot tell arrival from loss.
func (c *Client) Delivered(ctx context.Context, token, messageID string) (Delivery, error) {
	if messageID == "" {
		return Delivery{}, errors.New("a message id is required")
	}
	var payload struct {
		Result *struct {
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
			Timestamp string `json:"timestamp"`
		} `json:"result"`
	}
	if err := c.call(ctx, http.MethodPost, "/message/status", token, map[string]any{"id": messageID}, &payload); err != nil {
		return Delivery{}, classify(err)
	}
	if payload.Result == nil {
		return Delivery{MessageID: messageID}, nil
	}
	return Delivery{MessageID: first(payload.Result.MessageID, messageID), Status: payload.Result.Status, At: payload.Result.Timestamp}, nil
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

// WarmSession asks WhatsApp for the recipient's device list before a message is
// encrypted for it.
//
// The first message a freshly paired device sends to a device it has never
// talked to can be dropped for that device alone: whatsmeow finds no Signal
// session, skips it, and the send is still reported as a success, so the
// recipient is left with a message that never decrypts. Refreshing the device
// list first is the only lever this API offers against that.
//
// It is deliberately best effort. A failure here says nothing about whether the
// message can be sent, so the caller sends anyway rather than refusing on the
// strength of a warm-up.
func (c *Client) WarmSession(ctx context.Context, token, jid string) error {
	if jid == "" {
		return errors.New("a recipient is required")
	}
	return classify(c.call(ctx, http.MethodPost, "/user/info", token, map[string]any{"number": []string{jid}}, nil))
}

// DeleteMessage revokes a message for everyone. WhatsApp models this as a new
// message rather than an edit of the old one, so the returned id belongs to the
// revocation, not to the message that was removed.
func (c *Client) DeleteMessage(ctx context.Context, token, chatJID, messageID string) (SentMessage, error) {
	if chatJID == "" || messageID == "" {
		return SentMessage{}, errors.New("a chat and a message are both required")
	}
	var result sendResult
	body := map[string]any{"chat": chatJID, "messageId": messageID}
	if err := c.call(ctx, http.MethodPost, "/message/delete", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	sent := result.toSent()
	if sent.ID == "" {
		sent.ID = result.MessageID
	}
	return sent, nil
}

// EditMessage replaces the text of a message already sent. WhatsApp only allows
// this for the account's own messages and only for a while after sending, so a
// refusal here is usually the window having closed rather than a broken call.
func (c *Client) EditMessage(ctx context.Context, token, chatJID, messageID, text string) (SentMessage, error) {
	if chatJID == "" || messageID == "" || text == "" {
		return SentMessage{}, errors.New("a chat, a message and the new text are all required")
	}
	var result sendResult
	body := map[string]any{"chat": chatJID, "messageId": messageID, "message": text}
	if err := c.call(ctx, http.MethodPost, "/message/edit", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// React attaches an emoji to a message, or removes the account's reaction when
// the emoji is empty. The reaction is addressed by the message's own key, which
// is why it needs to know whether the target was sent by this account.
func (c *Client) React(ctx context.Context, token, chatJID, messageID, emoji string, fromMe bool, participant string) (SentMessage, error) {
	if chatJID == "" || messageID == "" {
		return SentMessage{}, errors.New("a chat and a message are both required")
	}
	var result sendResult
	body := map[string]any{"number": chatJID, "id": messageID, "reaction": emoji, "fromMe": fromMe}
	if participant != "" {
		body["participant"] = participant
	}
	if err := c.call(ctx, http.MethodPost, "/message/react", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// Presence is what WhatsApp knows about one number: whether it exists at all,
// and the identity it answers to. Checking before sending is the difference
// between a message that fails and one that is never attempted.
type Presence struct {
	Number     string `json:"number"`
	JID        string `json:"jid,omitempty"`
	OnWhatsApp bool   `json:"on_whatsapp"`
}

// CheckNumbers reports which of the given numbers have WhatsApp accounts.
func (c *Client) CheckNumbers(ctx context.Context, token string, numbers []string) ([]Presence, error) {
	if len(numbers) == 0 {
		return nil, errors.New("at least one number is required")
	}
	// Evolution answers {"data":{"Users":[…]}}, and c.call already unwraps the
	// "data", so what arrives here is an object with a Users array — not the
	// bare array this used to ask for. The field is IsInWhatsapp too, not
	// IsIn. Both were wrong, so every call failed on the decode and the tool
	// reported a WhatsApp error for a query WhatsApp had answered correctly.
	var raw struct {
		Users []struct {
			Query        string `json:"Query"`
			JID          string `json:"JID"`
			RemoteJID    string `json:"RemoteJID"`
			IsInWhatsapp bool   `json:"IsInWhatsapp"`
		} `json:"Users"`
	}
	if err := c.call(ctx, http.MethodPost, "/user/check", token, map[string]any{"number": numbers, "formatJid": true}, &raw); err != nil {
		return nil, classify(err)
	}
	found := make([]Presence, 0, len(raw.Users))
	for _, item := range raw.Users {
		jid := item.JID
		if jid == "" {
			jid = item.RemoteJID
		}
		found = append(found, Presence{Number: item.Query, JID: jid, OnWhatsApp: item.IsInWhatsapp || jid != ""})
	}
	return found, nil
}

// Avatar returns the URL of a profile picture. WhatsApp serves the image from
// its own CDN on a short-lived link, so this is a URL to fetch rather than
// bytes to keep.
func (c *Client) Avatar(ctx context.Context, token, jid string, full bool) (string, error) {
	if jid == "" {
		return "", errors.New("a number is required")
	}
	var raw struct {
		URL string `json:"URL"`
		Url string `json:"url"`
	}
	if err := c.call(ctx, http.MethodPost, "/user/avatar", token, map[string]any{"number": jid, "preview": !full}, &raw); err != nil {
		return "", classify(err)
	}
	return first(raw.URL, raw.Url), nil
}

// SendLocation sends a point on the map, optionally named.
func (c *Client) SendLocation(ctx context.Context, token, recipient string, latitude, longitude float64, name, address string) (SentMessage, error) {
	var result sendResult
	body := map[string]any{"number": recipient, "latitude": latitude, "longitude": longitude, "formatJid": true}
	if name != "" {
		body["name"] = name
	}
	if address != "" {
		body["address"] = address
	}
	if err := c.call(ctx, http.MethodPost, "/send/location", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// SendContact shares a contact card.
func (c *Client) SendContact(ctx context.Context, token, recipient, fullName, phone, organization string) (SentMessage, error) {
	if fullName == "" || phone == "" {
		return SentMessage{}, errors.New("a contact needs a name and a phone number")
	}
	var result sendResult
	card := map[string]any{"fullName": fullName, "phone": phone}
	if organization != "" {
		card["organization"] = organization
	}
	body := map[string]any{"number": recipient, "vcard": card, "formatJid": true}
	if err := c.call(ctx, http.MethodPost, "/send/contact", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// SendPoll sends a poll. maxAnswers of one makes it single-choice, which is
// what most callers mean by a poll.
func (c *Client) SendPoll(ctx context.Context, token, recipient, question string, options []string, maxAnswers int) (SentMessage, error) {
	if question == "" || len(options) < 2 {
		return SentMessage{}, errors.New("a poll needs a question and at least two options")
	}
	if maxAnswers <= 0 || maxAnswers > len(options) {
		maxAnswers = 1
	}
	var result sendResult
	body := map[string]any{"number": recipient, "question": question, "options": options, "maxAnswer": maxAnswers, "formatJid": true}
	if err := c.call(ctx, http.MethodPost, "/send/poll", token, body, &result); err != nil {
		return SentMessage{}, classify(err)
	}
	return result.toSent(), nil
}

// PollResult is one option of a poll and the people who chose it.
type PollResult struct {
	Option string   `json:"option"`
	Votes  int      `json:"votes"`
	Voters []string `json:"voters,omitempty"`
}

// PollResults reads the tally of a poll already sent. A poll nobody can read
// the answers to is barely a poll, which is why this belongs alongside sending
// one rather than as a later addition.
func (c *Client) PollResults(ctx context.Context, token, pollMessageID string) ([]PollResult, error) {
	if pollMessageID == "" {
		return nil, errors.New("a poll message id is required")
	}
	var raw []struct {
		Name    string   `json:"Name"`
		Option  string   `json:"option"`
		Count   int      `json:"Count"`
		Votes   int      `json:"votes"`
		Voters  []string `json:"Voters"`
		Senders []string `json:"voters"`
	}
	if err := c.call(ctx, http.MethodGet, "/polls/"+url.PathEscape(pollMessageID)+"/results", token, nil, &raw); err != nil {
		return nil, classify(err)
	}
	results := make([]PollResult, 0, len(raw))
	for _, item := range raw {
		voters := item.Voters
		if len(voters) == 0 {
			voters = item.Senders
		}
		count := item.Count
		if count == 0 {
			count = item.Votes
		}
		if count == 0 {
			count = len(voters)
		}
		results = append(results, PollResult{Option: first(item.Name, item.Option), Votes: count, Voters: voters})
	}
	return results, nil
}

// OrganiseChat archives, pins or mutes a conversation, and undoes each. These
// only change how the account's own client displays the chat, so unlike a
// revocation they are private and reversible.
func (c *Client) OrganiseChat(ctx context.Context, token, chatJID, action string) error {
	if chatJID == "" {
		return errors.New("a chat is required")
	}
	paths := map[string]string{
		"archive": "/chat/archive", "unarchive": "/chat/unarchive",
		"pin": "/chat/pin", "unpin": "/chat/unpin",
		"mute": "/chat/mute", "unmute": "/chat/unmute",
	}
	path, ok := paths[action]
	if !ok {
		return errors.New("action must be archive, unarchive, pin, unpin, mute or unmute")
	}
	return classify(c.call(ctx, http.MethodPost, path, token, map[string]any{"chat": chatJID}, nil))
}
