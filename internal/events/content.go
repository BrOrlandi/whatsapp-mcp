package events

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Details is everything the gateway reads out of one WhatsApp message
// protobuf: its text, its media, the message it quotes, who it mentions, the
// message a reaction is to, and the edit or deletion a protocol message
// carries. The keys follow whatsmeow's protobuf JSON names (live events) and
// protojson's (history sync); encoding/json matches them case-insensitively,
// which is what lets one shape read both.
type Details struct {
	Text      string
	MediaType string
	MimeType  string
	Filename  string
	Caption   string
	Bytes     uint64

	QuotedID          string
	QuotedParticipant string
	QuotedText        string
	Mentions          []string
	Forwarded         bool

	ReactionTo string
	Reaction   string

	// EditOf is the message a protocol message edits, with its new text;
	// RevokeOf the message it deletes for everyone.
	EditOf   string
	EditText string
	RevokeOf string

	Location *Location
	Poll     *Poll
}

// Location is a point on the map a message carries.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	Live      bool    `json:"live,omitempty"`
}

// Poll is the question and options of a poll a message creates.
type Poll struct {
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	MaxAnswers uint32   `json:"max_answers,omitempty"`
}

type contextInfo struct {
	StanzaID        string          `json:"stanzaID"`
	Participant     string          `json:"participant"`
	QuotedMessage   json.RawMessage `json:"quotedMessage"`
	MentionedJID    []string        `json:"mentionedJID"`
	IsForwarded     bool            `json:"isForwarded"`
	ForwardingScore uint32          `json:"forwardingScore"`
}

type mediaFields struct {
	Caption     string       `json:"caption"`
	Mimetype    string       `json:"mimetype"`
	FileName    string       `json:"fileName"`
	FileLength  flexUint     `json:"fileLength"`
	ContextInfo *contextInfo `json:"contextInfo"`
}

type messageKey struct {
	RemoteJID   string `json:"remoteJID"`
	FromMe      bool   `json:"fromMe"`
	ID          string `json:"ID"`
	Participant string `json:"participant"`
}

// rich mirrors the parts of the message protobuf Details reads.
type rich struct {
	Conversation string `json:"conversation"`
	ExtendedText *struct {
		Text        string       `json:"text"`
		ContextInfo *contextInfo `json:"contextInfo"`
	} `json:"extendedTextMessage"`
	Image    *mediaFields `json:"imageMessage"`
	Video    *mediaFields `json:"videoMessage"`
	Audio    *mediaFields `json:"audioMessage"`
	Document *mediaFields `json:"documentMessage"`
	Sticker  *mediaFields `json:"stickerMessage"`
	Location *struct {
		DegreesLatitude  float64      `json:"degreesLatitude"`
		DegreesLongitude float64      `json:"degreesLongitude"`
		Name             string       `json:"name"`
		Address          string       `json:"address"`
		ContextInfo      *contextInfo `json:"contextInfo"`
	} `json:"locationMessage"`
	LiveLocation *struct {
		DegreesLatitude  float64      `json:"degreesLatitude"`
		DegreesLongitude float64      `json:"degreesLongitude"`
		Caption          string       `json:"caption"`
		ContextInfo      *contextInfo `json:"contextInfo"`
	} `json:"liveLocationMessage"`
	Contact *struct {
		DisplayName string       `json:"displayName"`
		ContextInfo *contextInfo `json:"contextInfo"`
	} `json:"contactMessage"`
	Contacts *struct {
		DisplayName string `json:"displayName"`
	} `json:"contactsArrayMessage"`
	Reaction *struct {
		Key  messageKey `json:"key"`
		Text string     `json:"text"`
	} `json:"reactionMessage"`
	Protocol *struct {
		Key           messageKey      `json:"key"`
		Type          json.RawMessage `json:"type"`
		EditedMessage json.RawMessage `json:"editedMessage"`
	} `json:"protocolMessage"`
	PollV1 *pollFields `json:"pollCreationMessage"`
	PollV2 *pollFields `json:"pollCreationMessageV2"`
	PollV3 *pollFields `json:"pollCreationMessageV3"`
	Edited *struct {
		Message json.RawMessage `json:"message"`
	} `json:"editedMessage"`
}

type pollFields struct {
	Name    string `json:"name"`
	Options []struct {
		OptionName string `json:"optionName"`
	} `json:"options"`
	SelectableOptionsCount uint32       `json:"selectableOptionsCount"`
	ContextInfo            *contextInfo `json:"contextInfo"`
}

// flexUint reads a number written as a number or as a string, which is how
// protojson writes 64-bit integers.
type flexUint uint64

func (f *flexUint) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return nil
	}
	*f = flexUint(n)
	return nil
}

// ReadDetails reads one message protobuf. Wrappers (disappearing,
// view-once, captioned documents), quotes and edits are followed with a depth
// bound, because the payload is remote input and its nesting is the sender's
// to choose.
func ReadDetails(raw json.RawMessage) Details { return readDetails(raw, 0) }

func readDetails(raw json.RawMessage, depth int) Details {
	if depth > 3 {
		return Details{}
	}
	inner := innermost(raw, 0)
	var r rich
	var d Details
	if len(inner) == 0 || json.Unmarshal(inner, &r) != nil {
		return d
	}
	var ci *contextInfo
	media := func(kind string, m *mediaFields) {
		d.MediaType, d.MimeType, d.Filename, d.Caption, d.Bytes = kind, m.Mimetype, m.FileName, m.Caption, uint64(m.FileLength)
		d.Text = m.Caption
		ci = m.ContextInfo
	}
	switch {
	case r.Conversation != "":
		d.Text, d.MediaType = r.Conversation, "text"
	case r.ExtendedText != nil:
		d.Text, d.MediaType, ci = r.ExtendedText.Text, "text", r.ExtendedText.ContextInfo
	case r.Image != nil:
		media("image", r.Image)
	case r.Video != nil:
		media("video", r.Video)
	case r.Audio != nil:
		media("audio", r.Audio)
	case r.Document != nil:
		media("document", r.Document)
		if d.Text == "" {
			d.Text = d.Filename
		}
	case r.Sticker != nil:
		media("sticker", r.Sticker)
	case r.Location != nil:
		d.MediaType = "location"
		d.Location = &Location{Latitude: r.Location.DegreesLatitude, Longitude: r.Location.DegreesLongitude, Name: r.Location.Name, Address: r.Location.Address}
		ci = r.Location.ContextInfo
	case r.LiveLocation != nil:
		d.MediaType = "location"
		d.Location = &Location{Latitude: r.LiveLocation.DegreesLatitude, Longitude: r.LiveLocation.DegreesLongitude, Name: r.LiveLocation.Caption, Live: true}
		ci = r.LiveLocation.ContextInfo
	case r.Contact != nil:
		d.Text, d.MediaType, ci = r.Contact.DisplayName, "contact", r.Contact.ContextInfo
	case r.Contacts != nil:
		d.Text, d.MediaType = r.Contacts.DisplayName, "contact"
	case r.Reaction != nil:
		d.ReactionTo, d.Reaction = r.Reaction.Key.ID, r.Reaction.Text
	case r.PollV1 != nil || r.PollV2 != nil || r.PollV3 != nil:
		p := r.PollV1
		if p == nil {
			p = r.PollV2
		}
		if p == nil {
			p = r.PollV3
		}
		d.MediaType = "poll"
		d.Poll = &Poll{Question: p.Name, MaxAnswers: p.SelectableOptionsCount}
		for _, o := range p.Options {
			d.Poll.Options = append(d.Poll.Options, o.OptionName)
		}
		d.Text, ci = p.Name, p.ContextInfo
	case r.Protocol != nil:
		switch protocolType(r.Protocol.Type) {
		case "REVOKE":
			d.RevokeOf = r.Protocol.Key.ID
		case "MESSAGE_EDIT":
			d.EditOf = r.Protocol.Key.ID
			d.EditText = readDetails(r.Protocol.EditedMessage, depth+1).Text
		}
	case r.Edited != nil:
		// An older edit: the protocol message travels inside editedMessage.
		return readDetails(r.Edited.Message, depth+1)
	}
	if ci != nil {
		d.QuotedID, d.QuotedParticipant = ci.StanzaID, ci.Participant
		if d.QuotedID != "" && len(ci.QuotedMessage) > 0 {
			d.QuotedText = readDetails(ci.QuotedMessage, depth+1).Text
		}
		d.Mentions = ci.MentionedJID
		d.Forwarded = ci.IsForwarded || ci.ForwardingScore > 0
	}
	return d
}

// protocolType names a protocol message's type, written as the enum's number
// (whatsmeow's structs) or its name (protojson).
func protocolType(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	switch s {
	case "0", "REVOKE":
		return "REVOKE"
	case "14", "MESSAGE_EDIT":
		return "MESSAGE_EDIT"
	}
	return s
}

// innermost follows the wrappers WhatsApp puts around a message.
func innermost(raw json.RawMessage, depth int) json.RawMessage {
	if len(raw) == 0 || depth > 4 {
		return raw
	}
	var wrappers struct {
		Ephemeral           *struct{ Message json.RawMessage } `json:"ephemeralMessage"`
		ViewOnce            *struct{ Message json.RawMessage } `json:"viewOnceMessage"`
		ViewOnceV2          *struct{ Message json.RawMessage } `json:"viewOnceMessageV2"`
		ViewOnceV2Ext       *struct{ Message json.RawMessage } `json:"viewOnceMessageV2Extension"`
		DocumentWithCaption *struct{ Message json.RawMessage } `json:"documentWithCaptionMessage"`
	}
	if json.Unmarshal(raw, &wrappers) != nil {
		return raw
	}
	for _, w := range []*struct{ Message json.RawMessage }{wrappers.Ephemeral, wrappers.ViewOnce, wrappers.ViewOnceV2, wrappers.ViewOnceV2Ext, wrappers.DocumentWithCaption} {
		if w != nil && len(w.Message) > 0 {
			return innermost(w.Message, depth+1)
		}
	}
	return raw
}

// apply copies the details a message row keeps.
func (d Details) apply(m *storeMessage) {
	m.Text, m.MediaType, m.MimeType, m.Filename = d.Text, d.MediaType, d.MimeType, d.Filename
	m.QuotedID, m.Mentions, m.Forwarded = d.QuotedID, d.Mentions, d.Forwarded
	m.ReactionTo, m.Reaction = d.ReactionTo, d.Reaction
}
