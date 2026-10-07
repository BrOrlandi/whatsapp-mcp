package mcp

import (
	"context"
	"strings"
)

func groupJID(a arguments) (string, map[string]any) {
	jid := strings.TrimSpace(a.GroupJID)
	if !strings.HasSuffix(jid, "@g.us") {
		return "", toolError("group_jid must be a group JID ending in @g.us")
	}
	return jid, nil
}

// groupName is a group's name, live from WhatsApp, or "" when it cannot be
// read.
func (s *Server) groupName(ctx context.Context, session Session, jid string) (string, int) {
	if g, err := s.live.Group(ctx, session.Token, jid); err == nil {
		return g.Name, g.MemberCount
	}
	return s.index.ChatName(ctx, session.InstanceID, jid), 0
}

// participantJID turns a phone number into the JID WhatsApp addresses it by;
// a JID is kept as it is.
func participantJID(p string) string {
	p = strings.TrimSpace(p)
	if strings.Contains(p, "@") {
		return p
	}
	if d := phoneDigits(p); d != "" {
		return d + "@s.whatsapp.net"
	}
	return p
}

func (s *Server) manageParticipants(ctx context.Context, session Session, a arguments) map[string]any {
	jid, failure := groupJID(a)
	if failure != nil {
		return failure
	}
	actions := map[string]bool{"add": true, "remove": true, "promote": true, "demote": true}
	if !actions[a.Action] {
		return toolError("action must be add, remove, promote or demote")
	}
	if len(a.Participants) == 0 || len(a.Participants) > 50 {
		return toolError("participants must list one to fifty phone numbers or JIDs")
	}
	if a.Action == "remove" && !a.Confirm {
		people := make([]map[string]string, 0, len(a.Participants))
		for _, p := range a.Participants {
			people = append(people, map[string]string{"participant": p, "name": s.index.ChatName(ctx, session.InstanceID, participantJID(p))})
		}
		name, _ := s.groupName(ctx, session, jid)
		return textResult(map[string]any{"preview": true, "group": name, "group_jid": jid, "would_remove": people,
			"next": "call manage_group_participants again with confirm true to remove them; WhatsApp tells the group"}, false)
	}
	participants := make([]string, 0, len(a.Participants))
	for _, p := range a.Participants {
		participants = append(participants, participantJID(p))
	}
	if err := s.live.UpdateParticipants(ctx, session.Token, jid, a.Action, participants); err != nil {
		return liveError(err)
	}
	return textResult(map[string]any{"done": true, "action": a.Action, "group_jid": jid, "participants": participants}, false)
}

func (s *Server) updateGroup(ctx context.Context, session Session, a arguments) map[string]any {
	jid, failure := groupJID(a)
	if failure != nil {
		return failure
	}
	name := strings.TrimSpace(a.Name)
	if name == "" && a.Description == nil {
		return toolError("give a new name, a new description, or both")
	}
	if name != "" {
		if err := s.live.SetGroupName(ctx, session.Token, jid, name); err != nil {
			return liveError(err)
		}
	}
	if a.Description != nil {
		if err := s.live.SetGroupDescription(ctx, session.Token, jid, *a.Description); err != nil {
			return liveError(err)
		}
	}
	result := map[string]any{"done": true, "group_jid": jid}
	if name != "" {
		result["name"] = name
	}
	if a.Description != nil {
		result["description"] = *a.Description
	}
	return textResult(result, false)
}

func (s *Server) groupInviteLink(ctx context.Context, session Session, a arguments) map[string]any {
	jid, failure := groupJID(a)
	if failure != nil {
		return failure
	}
	name, _ := s.groupName(ctx, session, jid)
	if a.Reset && !a.Confirm {
		return textResult(map[string]any{"preview": true, "group": name, "group_jid": jid,
			"next": "call get_group_invite_link again with reset and confirm true: the current link stops working for everyone who has it"}, false)
	}
	link, err := s.live.GroupInviteLink(ctx, session.Token, jid, a.Reset)
	if err != nil {
		return liveError(err)
	}
	if link != "" && !strings.HasPrefix(link, "http") {
		link = "https://chat.whatsapp.com/" + link
	}
	return textResult(map[string]any{"group_jid": jid, "group": name, "reset": a.Reset, "link": link}, false)
}

func (s *Server) leaveGroup(ctx context.Context, session Session, a arguments) map[string]any {
	jid, failure := groupJID(a)
	if failure != nil {
		return failure
	}
	if !a.Confirm {
		name, size := s.groupName(ctx, session, jid)
		preview := map[string]any{"preview": true, "group_jid": jid, "group": name,
			"next": "call leave_group again with confirm true to leave; getting back in needs an invite"}
		if size > 0 {
			preview["participants"] = size
		}
		return textResult(preview, false)
	}
	if err := s.live.LeaveGroup(ctx, session.Token, jid); err != nil {
		return liveError(err)
	}
	return textResult(map[string]any{"left": true, "group_jid": jid}, false)
}
