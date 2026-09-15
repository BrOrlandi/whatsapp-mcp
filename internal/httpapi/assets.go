package httpapi

import (
	"embed"
	"net/http"
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
