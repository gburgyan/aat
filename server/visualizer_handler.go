package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleGetVisualizer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Find the visualizer definition.
	var def *VisualizerDef
	for i := range s.visualizers {
		if s.visualizers[i].ID == id {
			def = &s.visualizers[i]
			break
		}
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "not_found", "visualizer not found")
		return
	}

	// Resolve the file path and prevent path traversal.
	filePath := filepath.Join(s.opts.VisualizerDir, def.File)
	filePath = filepath.Clean(filePath)

	// Ensure the resolved path is within the visualizer directory.
	vizDir := filepath.Clean(s.opts.VisualizerDir)
	if !strings.HasPrefix(filePath, vizDir+string(filepath.Separator)) && filePath != vizDir {
		writeError(w, http.StatusForbidden, "forbidden", "path traversal denied")
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not_found", "visualizer file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to read visualizer file")
		return
	}

	// Set CSP headers for defense-in-depth: allow inline scripts/styles but block
	// all network access from the iframe.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src https: data:; connect-src 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// enrichStepVisualizers populates the Visualizers field on a StepDetail
// by matching loaded visualizer definitions against the step's node and response body.
func (s *Server) enrichStepVisualizers(step *StepDetail) {
	if len(s.visualizers) == 0 || step == nil {
		return
	}

	var responseBody []byte
	if step.Response != nil && step.Response.Body != nil {
		responseBody = step.Response.Body
	}

	hits := matchVisualizers(s.visualizers, step.Node, responseBody)
	if len(hits) > 0 {
		step.Visualizers = hits
	}
}
