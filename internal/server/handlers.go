package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"revboard/internal/diff"
	"revboard/internal/gitx"
	"revboard/internal/review"
	"revboard/internal/store"
)

// maxBodyBytes limits JSON request bodies.
const maxBodyBytes = 1 << 20

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("GET /api/projects/{id}/sessions", s.listProjectSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("PATCH /api/sessions/{id}", s.patchSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.deleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/refresh", s.refreshSession)
	mux.HandleFunc("POST /api/sessions/{id}/status", s.bulkStatus)
	mux.HandleFunc("GET /api/sessions/{id}/commits", s.listCommits)
	mux.HandleFunc("GET /api/sessions/{id}/comments", s.listComments)
	mux.HandleFunc("GET /api/sessions/{id}/notes", s.listNotes)
	mux.HandleFunc("POST /api/sessions/{id}/comments", s.createComment)
	mux.HandleFunc("GET /api/files/{id}", s.getFile)
	mux.HandleFunc("PUT /api/files/{id}/status", s.setFileStatus)
	mux.HandleFunc("PUT /api/files/{id}/pin", s.setPin)
	mux.HandleFunc("PUT /api/hunks/{id}/status", s.setHunkStatus)
	mux.HandleFunc("PATCH /api/comments/{id}", s.patchComment)
	mux.HandleFunc("DELETE /api/comments/{id}", s.deleteComment)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps domain errors to HTTP status codes.
func fail(w http.ResponseWriter, err error) {
	var gitErr *gitx.Error
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, &gitErr), errors.Is(err, review.ErrEmptyDiff), errors.Is(err, diff.ErrCombinedDiff):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) listProjectSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetProject(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	sessions, err := s.store.ListSessions(r.Context(), id, r.URL.Query().Get("archived") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.TouchSession(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	detail, err := s.store.GetSessionDetail(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) patchSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name       *string         `json:"name"`
		State      *string         `json:"state"`
		Exclusions *[]string       `json:"exclusions"`
		UIState    json.RawMessage `json:"uiState"`
	}
	if !decode(w, r, &body) {
		return
	}
	se, err := s.store.UpdateSession(r.Context(), id, store.SessionPatch{
		Name: body.Name, State: body.State, Exclusions: body.Exclusions, UIState: body.UIState,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, se)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSession(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refreshSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.review.Refresh(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	detail, err := s.store.GetSessionDetail(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) bulkStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		FileIDs []int64      `json:"fileIds"`
		Status  store.Status `json:"status"`
	}
	if !decode(w, r, &body) {
		return
	}
	sc, err := s.store.SetFilesStatus(r.Context(), id, body.FileIDs, body.Status)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (s *Server) listCommits(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.review.Commits(r.Context(), id, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	comments, err := s.store.ListComments(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	notes, err := s.store.ListNotesWithContext(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		FilePath string `json:"filePath"`
		Side     string `json:"side"`
		Line     int    `json:"line"`
		Body     string `json:"body"`
	}
	if !decode(w, r, &body) {
		return
	}
	c, err := s.store.CreateComment(r.Context(), store.Comment{
		SessionID: id, FilePath: body.FilePath, Side: body.Side, Line: body.Line, Body: body.Body,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetFileDetail(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type statusBody struct {
	Status store.Status `json:"status"`
}

func (s *Server) setFileStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body statusBody
	if !decode(w, r, &body) {
		return
	}
	sc, err := s.store.SetFileStatus(r.Context(), id, body.Status)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (s *Server) setHunkStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body statusBody
	if !decode(w, r, &body) {
		return
	}
	sc, err := s.store.SetHunkStatus(r.Context(), id, body.Status)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (s *Server) setPin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if !decode(w, r, &body) {
		return
	}
	f, err := s.store.SetPinned(r.Context(), id, body.Pinned)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) patchComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &body) {
		return
	}
	c, err := s.store.UpdateComment(r.Context(), id, body.Body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteComment(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
