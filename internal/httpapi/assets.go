package httpapi

import (
	"embed"
	"net/http"

	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
)

// assets holds the few static files the panel serves. They are embedded so the
// binary stays self-contained and the pages never reach for a CDN, which is
// also what lets the content security policy stay at 'self'.
//
//go:embed assets/app.js
//go:embed assets/theme.js
var assets embed.FS

// assetHandler serves the embedded files.
func assetHandler() http.Handler {
	served := http.FS(assets)
	return http.StripPrefix("/", http.FileServer(neverListed{served}))
}

// neverListed hides directory listings, which a file server offers by default
// and which has no place here.
type neverListed struct{ fs http.FileSystem }

func (n neverListed) Open(name string) (http.File, error) {
	file, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		file.Close()
		return nil, http.ErrMissingFile
	}
	return file, nil
}

// icon describes one embedded browser icon: the bytes, the media type, and
// nothing else. They are served unauthenticated because a browser asks for the
// tab icon before anybody logs in, and because there is nothing private in a
// logo.
type icon struct {
	body        []byte
	contentType string
}

// iconRoutes maps each icon path to its asset. /favicon.ico is served even
// though the pages link the SVG, because browsers request that path on their
// own and a missing one costs a 404 on every page load.
func iconRoutes() map[string]icon {
	return map[string]icon{
		"GET /favicon.svg":          {brand.FaviconSVG(), "image/svg+xml"},
		"GET /favicon.ico":          {brand.FaviconICO(), "image/x-icon"},
		"GET /apple-touch-icon.png": {brand.AppleTouchIcon(), "image/png"},
	}
}

// iconHandler serves one icon. The mark changes only when the binary does, so a
// long immutable cache is safe and keeps it out of every subsequent request.
func iconHandler(i icon) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", i.contentType)
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.Write(i.body)
	}
}
