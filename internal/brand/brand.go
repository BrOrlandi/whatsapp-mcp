// Package brand holds the WhatsApp MCP visual identity. The logo is kept as a
// versioned SVG file so the control panel templates and the documentation use
// the same asset without depending on any external image or CDN.
package brand

import (
	_ "embed"
	"html/template"
	"strings"
)

//go:embed logo.svg
var logo string

// LogoSVG returns the logo mark ready to be inlined in an HTML document. It is
// trusted markup shipped with the binary, never user input.
func LogoSVG() template.HTML { return template.HTML(strings.TrimSpace(logo)) }
