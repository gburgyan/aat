package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

const defaultViteURL = "http://localhost:5173"

// devProxy returns an http.HandlerFunc that reverse-proxies requests to a
// Vite dev server. This enables HMR and fast refresh during frontend development.
func (s *Server) devProxy() http.HandlerFunc {
	viteURL := s.opts.ViteURL
	if viteURL == "" {
		viteURL = defaultViteURL
	}

	target, err := url.Parse(viteURL)
	if err != nil {
		panic("server: invalid ViteURL: " + err.Error())
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "Vite dev server is not running at %s.\n\nStart it with:\n  cd server/web && npm run dev\n", viteURL)
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}
}
