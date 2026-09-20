// Package brand holds the WhatsApp MCP visual identity. Every asset is kept as
// a versioned file and embedded into the binary, so the control panel, the
// browser tab and the documentation use the same mark without depending on any
// external image or CDN.
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

// The favicon is a separate file rather than the logo reused: the logo is an
// open outline that turns to mush at 16px, so the tab icon is the same mark
// simplified into a filled tile. The ICO and the PNG are generated from
// favicon.svg — see docs/development.md before editing them by hand.

//go:embed favicon.svg
var faviconSVG []byte

//go:embed favicon.ico
var faviconICO []byte

//go:embed apple-touch-icon.png
var appleTouchIcon []byte

// FaviconSVG is the scalable tab icon, preferred by every current browser.
func FaviconSVG() []byte { return faviconSVG }

// FaviconICO is the 16/32/48 fallback for browsers that request /favicon.ico
// directly and ignore the link tags.
func FaviconICO() []byte { return faviconICO }

// AppleTouchIcon is the 180px home-screen icon iOS asks for, which accepts no
// SVG.
func AppleTouchIcon() []byte { return appleTouchIcon }

// The project's authorship and the two links that go with it. They live here
// rather than in a template so the panel, the documentation page and anything
// else that credits the project all say the same thing, and so changing the
// donation link is one edit rather than a search.
const (
	// Name is what the project calls itself, wherever the panel or its
	// registrations need a label instead of a logo.
	Name = "WhatsApp MCP"
	// Author is the person who wrote and maintains this.
	Author = "Bruno Orlandi"
	// AuthorURL is where clicking the name goes.
	AuthorURL = "https://github.com/BrOrlandi"
	// RepositoryURL is the source, which the licence expects to stay reachable.
	RepositoryURL = "https://github.com/BrOrlandi/whatsapp-mcp"
	// SupportURL takes a voluntary payment of any amount, suggested at ten
	// dollars. Empty hides the button entirely rather than linking somewhere
	// broken, which is what it was until there was a link to point at.
	SupportURL = "https://donate.stripe.com/8x200jdhA6c1d375jF9Ve06"
)
