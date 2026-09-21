package httpapi

import (
	"fmt"
	"strings"
	"unicode"
)

// phone renders a WhatsApp identifier the way a person writes their own
// number. Evolution reports it as the protocol carries it — "5511923456789:89",
// sometimes with a "@s.whatsapp.net" suffix — and that string is unreadable to
// anyone who has not seen a JID before. Everything after the device suffix is
// dropped, since it identifies the linked device rather than the account.
//
// Brazilian numbers get the local shape because that is who the panel is for;
// anything else is returned in the international form rather than guessed at,
// which is wrong for nobody.
func phone(raw string) string {
	digits := make([]byte, 0, len(raw))
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if character == ':' || character == '@' {
			break
		}
		if character >= '0' && character <= '9' {
			digits = append(digits, character)
		}
	}
	number := string(digits)
	if number == "" {
		return ""
	}
	// 55 + two-digit area code + an eight or nine digit local number. The split
	// is counted from the end so both lengths land with four digits after the
	// dash, which is how the number is written either way.
	if strings.HasPrefix(number, "55") && (len(number) == 12 || len(number) == 13) {
		area, local := number[2:4], number[4:]
		return fmt.Sprintf("+55 (%s) %s-%s", area, local[:len(local)-4], local[len(local)-4:])
	}
	return "+" + number
}

// knownClients maps what MCP clients call themselves in the handshake to the
// name their users know them by. The panel talks about tools, not protocol
// identifiers, so "claude-ai" has to come back as "Claude Desktop".
var knownClients = map[string]string{
	"claude-ai":          "Claude Desktop",
	"claude-desktop":     "Claude Desktop",
	"claude":             "Claude",
	"claude-code":        "Claude Code",
	"cursor":             "Cursor",
	"cursor-vscode":      "Cursor",
	"windsurf":           "Windsurf",
	"cline":              "Cline",
	"roo-cline":          "Roo Code",
	"continue":           "Continue",
	"zed":                "Zed",
	"vscode":             "VS Code",
	"visual studio code": "VS Code",
	"mcp-inspector":      "MCP Inspector",
	"chatgpt":            "ChatGPT",
	"openai-mcp":         "ChatGPT",
	"librechat":          "LibreChat",
	"goose":              "Goose",
	"n8n":                "n8n",
}

// toolLabel names the AI tool on the other end of a credential. An unknown
// client is shown as it announced itself: a name nobody recognises still says
// more than none at all.
func toolLabel(reported string) string {
	name := strings.TrimSpace(reported)
	if name == "" {
		return ""
	}
	if label, known := knownClients[strings.ToLower(name)]; known {
		return label
	}
	return name
}

// initial is the first letter of a name, for the badge that stands in for a
// tool's logo. It walks runes rather than bytes so an accented name does not
// come back as half a character.
func initial(name string) string {
	for _, letter := range strings.TrimSpace(name) {
		return string(unicode.ToUpper(letter))
	}
	return "?"
}
