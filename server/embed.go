package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed web/dist
var webAssets embed.FS

// spaFileServer returns an http.Handler that serves static files from the
// embedded web/dist directory. Unknown paths fall back to index.html so that
// client-side routing works (SPA behavior).
func spaFileServer() http.Handler {
	sub, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		panic("server: embedded web/dist not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path and check if the file exists.
		p := path.Clean(r.URL.Path)
		if p == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Strip leading slash for fs.Stat lookup.
		name := p[1:]
		if _, err := fs.Stat(sub, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for SPA routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
