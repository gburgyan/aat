package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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

	savedOnly := r.URL.Query().Get("saved") == "true"

	runs, err := s.service.ListRuns(limit, savedOnly)
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

	savedOnly := r.URL.Query().Get("saved") == "true"

	batches, err := s.service.ListBatches(limit, savedOnly)
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

func (s *Server) handleRenameRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}

	newRef, err := s.service.RenameRun(id, req.Name)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "rename_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RenameResponse{Ref: newRef, Name: displayName(newRef)})
}

func (s *Server) handleUnnameRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	newRef, err := s.service.UnnameRun(id)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "rename_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RenameResponse{Ref: newRef, Name: displayName(newRef)})
}

func (s *Server) handleRenameBatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}

	newRef, err := s.service.RenameBatch(id, req.Name)
	if err != nil {
		if errors.Is(err, ErrBatchNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "rename_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RenameResponse{Ref: newRef, Name: displayName(newRef)})
}

func (s *Server) handleUnnameBatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	newRef, err := s.service.UnnameBatch(id)
	if err != nil {
		if errors.Is(err, ErrBatchNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "rename_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RenameResponse{Ref: newRef, Name: displayName(newRef)})
}

func (s *Server) handleExportRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var buf bytes.Buffer
	filename, err := s.service.ExportRun(id, &buf)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "export_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeHeaderValue(filename)))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, &buf)
}

func (s *Server) handleExportBatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var buf bytes.Buffer
	filename, err := s.service.ExportBatch(id, &buf)
	if err != nil {
		if errors.Is(err, ErrBatchNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "export_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeHeaderValue(filename)))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, &buf)
}

const maxUploadSize = 100 * 1024 * 1024 // 100MB

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	// Limit upload size.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds 100MB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "missing_file", "missing 'file' field in multipart form")
		return
	}
	defer func() { _ = file.Close() }()

	// Read into buffer for ReaderAt access.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds 100MB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_archive", "failed to read uploaded file")
		return
	}

	data := buf.Bytes()
	reader := bytes.NewReader(data)

	ref, archiveType, err := s.service.ImportArchive(reader, int64(len(data)), header.Filename)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "already exists") {
			writeError(w, http.StatusConflict, "name_conflict", errMsg)
			return
		}
		if strings.Contains(errMsg, "invalid archive name") {
			writeError(w, http.StatusBadRequest, "invalid_filename", errMsg)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_archive", errMsg)
		return
	}

	writeJSON(w, http.StatusCreated, ImportResponse{
		Ref:  ref,
		Name: ref,
		Type: archiveType,
	})
}

// sanitizeHeaderValue removes characters that could cause header injection.
func sanitizeHeaderValue(s string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(s)
}
