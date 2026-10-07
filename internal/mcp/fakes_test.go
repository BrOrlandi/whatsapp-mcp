package mcp

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// The index methods the 1.3 tools use, faked over the same message list.

func (f *fakeIndex) Setting(_ context.Context, key string) (string, error) {
	return f.settings[key], nil
}
func (f *fakeIndex) SetSetting(_ context.Context, key, value string) error {
	if f.settings == nil {
		f.settings = map[string]string{}
	}
	f.settings[key] = value
	return nil
}
func (f *fakeIndex) MessageInChat(ctx context.Context, instance, id, chat string) (store.Message, error) {
	for _, m := range f.messages {
		if m.MessageID == id && (chat == "" || m.ChatJID == chat) {
			return m, nil
		}
	}
	return f.MessageByID(ctx, instance, id)
}
func (f *fakeIndex) ChatInfo(_ context.Context, _ string, jid string) (store.Chat, bool) {
	for _, c := range append(append([]store.Chat{}, f.chats...), f.unread...) {
		if c.ChatJID == jid {
			return c, true
		}
	}
	return store.Chat{}, false
}
func (f *fakeIndex) ChatName(_ context.Context, _ string, jid string) string { return f.names[jid] }
func (f *fakeIndex) ChatOldest(context.Context, string, string) (time.Time, error) {
	return f.coverage.OldestAt, nil
}
func (f *fakeIndex) UnreadChats(context.Context, string, bool, int) ([]store.Chat, error) {
	return f.unread, nil
}
func (f *fakeIndex) CountMessages(_ context.Context, _ string, filter store.Filter) (int64, error) {
	f.lastFilter = filter
	return int64(len(f.messages)), nil
}
func (f *fakeIndex) Stats(_ context.Context, _ string, filter store.Filter, groupBy, _ string, _ int) ([]store.Bucket, int64, int, error) {
	f.lastFilter = filter
	return f.stats, int64(len(f.messages)), len(f.stats), nil
}
func (f *fakeIndex) EachMessage(_ context.Context, _ string, filter store.Filter, fn func(store.Message) error) error {
	f.lastFilter = filter
	for _, m := range f.messages {
		if filter.ChatJID != "" && m.ChatJID != filter.ChatJID {
			continue
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeIndex) MessageContext(_ context.Context, _ string, target store.Message, before, after int) ([]store.Message, []store.Message, error) {
	var earlier, later []store.Message
	for _, m := range f.messages {
		if m.ChatJID != target.ChatJID || m.MessageID == target.MessageID {
			continue
		}
		if m.SentAt.Before(target.SentAt) {
			earlier = append(earlier, m)
		} else {
			later = append(later, m)
		}
	}
	if len(earlier) > before {
		earlier = earlier[len(earlier)-before:]
	}
	if len(later) > after {
		later = later[:after]
	}
	return earlier, later, nil
}
func (f *fakeIndex) Incoming(_ context.Context, _ string, chat string, n int) ([]store.Message, error) {
	var out []store.Message
	for i := len(f.messages) - 1; i >= 0 && len(out) < n; i-- {
		if m := f.messages[i]; m.ChatJID == chat && !m.FromMe {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeIndex) LastMessages(context.Context, string, time.Time) ([]store.Message, error) {
	last := map[string]store.Message{}
	var order []string
	for _, m := range f.messages {
		if _, ok := last[m.ChatJID]; !ok {
			order = append(order, m.ChatJID)
		}
		if m.SentAt.After(last[m.ChatJID].SentAt) || last[m.ChatJID].MessageID == "" {
			last[m.ChatJID] = m
		}
	}
	out := make([]store.Message, 0, len(order))
	for _, c := range order {
		out = append(out, last[c])
	}
	return out, nil
}
func (f *fakeIndex) Waiting(ctx context.Context, instance, chat string) (int64, time.Time, error) {
	sent := f.LastSent(ctx, instance, chat)
	var n int64
	var first time.Time
	for _, m := range f.messages {
		if m.ChatJID == chat && !m.FromMe && m.SentAt.After(sent) {
			n++
			if first.IsZero() {
				first = m.SentAt
			}
		}
	}
	return n, first, nil
}
func (f *fakeIndex) LastSent(_ context.Context, _ string, chat string) time.Time {
	var last time.Time
	for _, m := range f.messages {
		if m.ChatJID == chat && m.FromMe && m.SentAt.After(last) {
			last = m.SentAt
		}
	}
	return last
}
func (f *fakeIndex) Mentions(context.Context, string, []string, string, time.Time, int) ([]store.Message, error) {
	return f.mentions, nil
}
func (f *fakeIndex) OwnIDs(context.Context, string) []string { return f.own }
func (f *fakeIndex) Activity(context.Context, string, time.Time) (store.Activity, error) {
	var a store.Activity
	for _, m := range f.messages {
		at := m.SentAt
		if !m.FromMe && (a.NewestIncoming == nil || at.After(*a.NewestIncoming)) {
			a.NewestIncoming = &at
		}
	}
	return a, nil
}
func (f *fakeIndex) SetChatFlag(_ context.Context, _ string, chat, action string, _ time.Time) error {
	f.flags = append(f.flags, chat+"|"+action)
	return nil
}
func (f *fakeIndex) MarkChatRead(_ context.Context, _ string, chat string, _ time.Time) error {
	f.reads = append(f.reads, chat)
	return nil
}
func (f *fakeIndex) Marks(context.Context, string) (map[string]store.Mark, error) {
	return f.marks, nil
}
func (f *fakeIndex) MarkHandled(_ context.Context, _ string, chat, note string, at time.Time) error {
	if f.marks == nil {
		f.marks = map[string]store.Mark{}
	}
	f.marks[chat] = store.Mark{ChatJID: chat, HandledAt: at, Note: note}
	return nil
}
func (f *fakeIndex) Snooze(_ context.Context, _ string, chat, note string, at, until time.Time) error {
	if f.marks == nil {
		f.marks = map[string]store.Mark{}
	}
	f.marks[chat] = store.Mark{ChatJID: chat, HandledAt: at, SnoozedUntil: &until, Note: note}
	return nil
}
func (f *fakeIndex) ClearMark(_ context.Context, _ string, chat string) error {
	delete(f.marks, chat)
	return nil
}

// The live operations the 1.3 tools add.

func (f *fakeLive) MarkRead(_ context.Context, token, chat string, ids []string) error {
	f.note(token)
	f.actions = append(f.actions, "markread|"+chat+"|"+strings.Join(ids, ","))
	return f.err
}
func (f *fakeLive) Presence(_ context.Context, token, to string, typing, audio bool) error {
	f.note(token)
	state := "paused"
	if typing {
		state = "composing"
	}
	if audio {
		state += "+audio"
	}
	f.actions = append(f.actions, "presence|"+to+"|"+state)
	return f.err
}
func (f *fakeLive) UpdateParticipants(_ context.Context, token, group, action string, participants []string) error {
	f.note(token)
	f.actions = append(f.actions, "participants|"+group+"|"+action+"|"+strings.Join(participants, ","))
	return f.err
}
func (f *fakeLive) SetGroupName(_ context.Context, token, group, name string) error {
	f.note(token)
	f.actions = append(f.actions, "name|"+group+"|"+name)
	return f.err
}
func (f *fakeLive) SetGroupDescription(_ context.Context, token, group, description string) error {
	f.note(token)
	f.actions = append(f.actions, "description|"+group+"|"+description)
	return f.err
}
func (f *fakeLive) GroupInviteLink(_ context.Context, token, group string, reset bool) (string, error) {
	f.note(token)
	if reset {
		f.actions = append(f.actions, "invite-reset|"+group)
	}
	return f.inviteLink, f.err
}
func (f *fakeLive) LeaveGroup(_ context.Context, token, group string) error {
	f.note(token)
	f.actions = append(f.actions, "leave|"+group)
	return f.err
}

// testFiles is a fresh folder for the media and exports a test writes.
func testFiles() string {
	dir, err := os.MkdirTemp("", "whatsapp-mcp-test-")
	if err != nil {
		panic(err)
	}
	return dir
}
