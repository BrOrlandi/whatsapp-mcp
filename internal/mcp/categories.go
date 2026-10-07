package mcp

// Every tool belongs to one category, by what it touches. The categories set
// the MCP annotations clients use to tell a read from an action, and group the
// tools on the panel's Funções page.
const (
	CategoryRead     = "read"     // reads; changes nothing anyone can see
	CategoryLocal    = "local"    // changes only the gateway's own records
	CategorySend     = "send"     // something reaches other people
	CategoryDelete   = "delete"   // removes messages
	CategoryGroups   = "groups"   // administers groups
	CategoryOrganise = "organise" // changes how the account's own chats are shown
)

var toolCategories = map[string]string{
	"health":              CategoryRead,
	"whatsapp_status":     CategoryRead,
	"list_chats":          CategoryRead,
	"get_chat_messages":   CategoryRead,
	"search_messages":     CategoryRead,
	"list_contacts":       CategoryRead,
	"list_groups":         CategoryRead,
	"get_group":           CategoryRead,
	"download_media":      CategoryRead,
	"transcribe_audio":    CategoryRead,
	"get_poll_results":    CategoryRead,
	"check_numbers":       CategoryRead,
	"get_profile_picture": CategoryRead,
	"get_message_context": CategoryRead,
	"message_stats":       CategoryRead,
	"list_unread":         CategoryRead,
	"list_unanswered":     CategoryRead,
	"list_mentions":       CategoryRead,
	"media_stats":         CategoryRead,

	"save_transcript":       CategoryLocal,
	"set_transcription_key": CategoryLocal,
	"sync_history":          CategoryLocal,
	"export_messages":       CategoryLocal,
	"mark_handled":          CategoryLocal,
	"snooze_chat":           CategoryLocal,
	"purge_media":           CategoryLocal,

	"send_text_message":  CategorySend,
	"send_media_message": CategorySend,
	"send_location":      CategorySend,
	"send_contact":       CategorySend,
	"send_poll":          CategorySend,
	"react_to_message":   CategorySend,
	"edit_message":       CategorySend,
	"forward_message":    CategorySend,
	"mark_chat_read":     CategorySend,
	"send_typing":        CategorySend,

	"delete_message": CategoryDelete,

	"manage_group_participants": CategoryGroups,
	"update_group":              CategoryGroups,
	"get_group_invite_link":     CategoryGroups,
	"leave_group":               CategoryGroups,

	"organise_chat": CategoryOrganise,
}

// ToolCategory is the category of a tool, empty for an unknown one.
func ToolCategory(name string) string { return toolCategories[name] }

// Category is one group of tools, as the panel lists them.
type Category struct {
	Name  string
	Title string
	Tools []string
}

// Categories lists the categories in the order the panel shows them, each
// with its tools in the order tools/list gives them.
func Categories() []Category {
	out := []Category{
		{Name: CategoryRead, Title: "Ler e consultar"},
		{Name: CategoryLocal, Title: "Registros do gateway"},
		{Name: CategorySend, Title: "Enviar e responder"},
		{Name: CategoryDelete, Title: "Apagar"},
		{Name: CategoryGroups, Title: "Administrar grupos"},
		{Name: CategoryOrganise, Title: "Organizar conversas"},
	}
	index := map[string]int{}
	for i, c := range out {
		index[c.Name] = i
	}
	for _, d := range definitions() {
		name := d.(map[string]any)["name"].(string)
		if i, ok := index[toolCategories[name]]; ok {
			out[i].Tools = append(out[i].Tools, name)
		}
	}
	return out
}

// destructive are the tools whose effect cannot be undone.
var destructive = map[string]bool{"delete_message": true, "manage_group_participants": true, "leave_group": true,
	"get_group_invite_link": true, "purge_media": true}

// toolDefinitions is what tools/list answers: each definition with the
// annotations its category implies.
func toolDefinitions() []any {
	defs := definitions()
	for _, d := range defs {
		def := d.(map[string]any)
		name := def["name"].(string)
		readOnly := toolCategories[name] == CategoryRead
		annotations := map[string]any{"readOnlyHint": readOnly}
		if !readOnly {
			annotations["destructiveHint"] = destructive[name]
		}
		def["annotations"] = annotations
	}
	return defs
}
