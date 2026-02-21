package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("invalid limit: %q", q))
			return
		}
		limit = n
	}

	traces, err := s.traceService.ListTraces(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Normalize nil to empty slice so JSON encodes as [] not null.
	if traces == nil {
		traces = []TraceListEntry{}
	}
	writeJSON(w, http.StatusOK, traces)
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	trace, err := s.traceService.GetTrace(id)
	if err != nil {
		if errors.Is(err, ErrTraceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, trace)
}
