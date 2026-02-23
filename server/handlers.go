package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// apiError is the JSON envelope for error responses.
type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: message, Code: code})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("invalid limit: %q", q))
			return
		}
		limit = n
	}

	runs, err := s.service.ListRuns(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Normalize nil to empty slice so JSON encodes as [] not null.
	if runs == nil {
		runs = []RunListEntry{}
	}
	writeJSON(w, http.StatusOK, runs)
}

// handleLatestRunRedirect redirects /runs/latest to /runs/{id} so the SPA has a real run ID.
func (s *Server) handleLatestRunRedirect(w http.ResponseWriter, r *http.Request) {
	id, err := s.service.LatestRunID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found", "no runs available")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/runs/%s", id), http.StatusFound)
}

func (s *Server) handleLatestRun(w http.ResponseWriter, r *http.Request) {
	id, err := s.service.LatestRunID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found", "no runs available")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/api/runs/%s", id), http.StatusFound)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	run, err := s.service.GetRun(id)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListBatches(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("invalid limit: %q", q))
			return
		}
		limit = n
	}

	batches, err := s.service.ListBatches(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if batches == nil {
		batches = []BatchListEntry{}
	}
	writeJSON(w, http.StatusOK, batches)
}

func (s *Server) handleGetBatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	batch, err := s.service.GetBatch(id)
	if err != nil {
		if errors.Is(err, ErrBatchNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (s *Server) handleGetAttempt(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	attemptStr := chi.URLParam(r, "attempt")
	attemptNum, err := strconv.Atoi(attemptStr)
	if err != nil || attemptNum < 1 {
		writeError(w, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("invalid attempt number: %q", attemptStr))
		return
	}

	detail, err := s.service.GetAttempt(runID, attemptNum)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleGetAttemptStep(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	attemptStr := chi.URLParam(r, "attempt")
	stepID := chi.URLParam(r, "stepId")

	attemptNum, err := strconv.Atoi(attemptStr)
	if err != nil || attemptNum < 1 {
		writeError(w, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("invalid attempt number: %q", attemptStr))
		return
	}

	step, err := s.service.GetAttemptStep(runID, attemptNum, stepID)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) || errors.Is(err, ErrStepNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, step)
}

func (s *Server) handleGetStep(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	stepID := chi.URLParam(r, "stepId")

	step, err := s.service.GetStep(runID, stepID)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) || errors.Is(err, ErrStepNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, step)
}
