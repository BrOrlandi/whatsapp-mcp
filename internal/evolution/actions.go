package evolution

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// MarkRead sends read receipts for messages of a chat, which is also what
// clears the chat as unread on the account's other devices. Evolution passes
// the chat as the sender of every message, so in a group the receipts are
// addressed to the group rather than to each author.
func (c *Client) MarkRead(ctx context.Context, token, chatJID string, messageIDs []string) error {
	if chatJID == "" || len(messageIDs) == 0 {
		return errors.New("a chat and at least one message are required")
	}
	return classify(c.call(ctx, http.MethodPost, "/message/markread", token, map[string]any{"number": chatJID, "id": messageIDs}, nil))
}

// Presence shows "typing…" (or "recording audio…") in a chat, or clears it.
func (c *Client) Presence(ctx context.Context, token, to string, typing, audio bool) error {
	if to == "" {
		return errors.New("a chat is required")
	}
	state := "paused"
	if typing {
		state = "composing"
	}
	return classify(c.call(ctx, http.MethodPost, "/message/presence", token, map[string]any{"number": to, "state": state, "isAudio": audio}, nil))
}

// UpdateParticipants adds, removes, promotes or demotes people in a group.
func (c *Client) UpdateParticipants(ctx context.Context, token, groupJID, action string, participants []string) error {
	switch action {
	case "add", "remove", "promote", "demote":
	default:
		return errors.New("action must be add, remove, promote or demote")
	}
	if !strings.HasSuffix(groupJID, "@g.us") || len(participants) == 0 {
		return errors.New("a group and at least one participant are required")
	}
	body := map[string]any{"groupJid": groupJID, "action": action, "participants": participants}
	return classify(c.call(ctx, http.MethodPost, "/group/participant", token, body, nil))
}

// SetGroupName renames a group.
func (c *Client) SetGroupName(ctx context.Context, token, groupJID, name string) error {
	return classify(c.call(ctx, http.MethodPost, "/group/name", token, map[string]any{"groupJid": groupJID, "name": name}, nil))
}

// SetGroupDescription replaces a group's description; empty clears it.
func (c *Client) SetGroupDescription(ctx context.Context, token, groupJID, description string) error {
	return classify(c.call(ctx, http.MethodPost, "/group/description", token, map[string]any{"groupJid": groupJID, "description": description}, nil))
}

// GroupInviteLink returns a group's invite link; reset revokes the current
// one and makes a new one.
func (c *Client) GroupInviteLink(ctx context.Context, token, groupJID string, reset bool) (string, error) {
	var link string
	if err := c.call(ctx, http.MethodPost, "/group/invitelink", token, map[string]any{"groupJid": groupJID, "reset": reset}, &link); err != nil {
		return "", classify(err)
	}
	return link, nil
}

// LeaveGroup leaves a group.
func (c *Client) LeaveGroup(ctx context.Context, token, groupJID string) error {
	return classify(c.call(ctx, http.MethodPost, "/group/leave", token, map[string]any{"groupJid": groupJID}, nil))
}
