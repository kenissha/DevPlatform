package repoimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes imports over HTTP. Every route behind these is
// admin-only at the router — importing is creating a repository, and that
// has always been an admin action.
type Handlers struct {
	Store *Store
	Audit *audit.Logger
}

type startRequest struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	// Token is used once and never stored — not on the Job, not on disk,
	// not in the audit entry. See the Store.Start doc comment.
	Token string `json:"token"`
}

// Start handles POST /api/repo-imports.
//
// Returns 202, not 201: the repository does not exist yet. A clone of a
// real project takes minutes, so the caller gets a job to watch rather
// than a request that hangs until a proxy times it out.
func (h *Handlers) Start(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	job, err := h.Store.Start(req.Name, req.Source, req.Token)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidName):
			http.Error(w, "400 depo adı sadece harf, rakam, tire ve alt çizgi içerebilir", http.StatusBadRequest)
		case errors.Is(err, ErrInvalidSource):
			http.Error(w, "400 kaynak adresi http:// veya https:// ile başlayan bir git adresi olmalı", http.StatusBadRequest)
		case errors.Is(err, ErrAlreadyExists):
			http.Error(w, "409 bu adda bir depo zaten var", http.StatusConflict)
		default:
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Logged at the start rather than the end so an import that never
	// finishes still leaves a record of who asked for it. job.Source is
	// the sanitised URL — a pasted credential never reaches the log.
	if h.Audit != nil {
		_ = h.Audit.Log(user.Subject, audit.ActionRepoImported, job.Name, job.ID,
			fmt.Sprintf("Depo içe aktarılıyor: %s ← %s", job.Name, job.Source))
	}

	writeJSON(w, http.StatusAccepted, job)
}

// Get handles GET /api/repo-imports/{id} — the panel polls this while a
// clone runs.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	job, err := h.Store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "404 içe aktarım bulunamadı", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// List handles GET /api/repo-imports — every import this process has run,
// newest first. Jobs do not survive a restart (see Store), so an empty
// list means "nothing since the server started", not "never happened".
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Store.List())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
