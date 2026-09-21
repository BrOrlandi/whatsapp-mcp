package httpapi

import "testing"

// A WhatsApp identifier is not a phone number to anybody who has not seen one
// before, and the panel is for people who have not.
func TestPhoneIsWrittenTheWayItsOwnerWritesIt(t *testing.T) {
	for raw, want := range map[string]string{
		"5511923456789:89":             "+55 (11) 92345-6789",
		"5511923456789":                "+55 (11) 92345-6789",
		"5511923456789@s.whatsapp.net": "+55 (11) 92345-6789",
		"551134567890":                 "+55 (11) 3456-7890",
		"+55 11 92345-6789":            "+55 (11) 92345-6789",
		"14155551234":                  "+14155551234",
		"":                             "",
		"@s.whatsapp.net":              "",
	} {
		if got := phone(raw); got != want {
			t.Errorf("phone(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The panel names tools, not protocol identifiers. A client it has never heard
// of is still shown by whatever it called itself.
func TestToolLabelNamesTheToolAndNotTheHandshake(t *testing.T) {
	for reported, want := range map[string]string{
		"claude-ai":     "Claude Desktop",
		"Claude-Code":   "Claude Code",
		"cursor-vscode": "Cursor",
		"  n8n  ":       "n8n",
		"algo-novo":     "algo-novo",
		"":              "",
	} {
		if got := toolLabel(reported); got != want {
			t.Errorf("toolLabel(%q) = %q, want %q", reported, got, want)
		}
	}
}

func TestInitialStepsOverRunesRatherThanBytes(t *testing.T) {
	for name, want := range map[string]string{
		"Claude Desktop": "C",
		"ótimo":          "Ó",
		"  zed":          "Z",
		"":               "?",
	} {
		if got := initial(name); got != want {
			t.Errorf("initial(%q) = %q, want %q", name, got, want)
		}
	}
}
