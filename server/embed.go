package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed web/dist
var webAssets embed.FS

// HasWebAssets reports whether the compiled frontend bundle is embedded in
// this binary. Vite builds the bundle into web/dist/app, which git ignores, for
// `make build` and the release pipeline. A plain `go install` build embeds only
// the tracked web/dist/placeholder.txt.
func HasWebAssets() bool {
	entries, err := fs.ReadDir(webAssets, "web/dist/app/assets")
	return err == nil && len(entries) > 0
}

// spaFileServer returns an http.Handler that serves static files from the
// embedded web/dist/app bundle. Unknown paths fall back to index.html so that
// client-side routing works (SPA behavior).
func spaFileServer() http.Handler {
	sub, err := fs.Sub(webAssets, "web/dist/app")
	if err != nil {
		panic("server: embedded web/dist/app not found: " + err.Error())
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
