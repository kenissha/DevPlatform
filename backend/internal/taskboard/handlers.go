package taskboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes the task board API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth. Unlike merge
// requests, no endpoint here is Admin-gated — see Store.Update's doc
// comment for why.
type Handlers struct {
	Store *Store
	Repos *repostore.Store
}

type createRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AssignedTo  string `json:"assignedTo"`
}

// Create handles POST /api/repos/{repo}/tasks.
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "400 title is required", http.StatusBadRequest)
		return
	}

	task, err := h.Store.Create(repo, req.Title, req.Description, req.AssignedTo, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, task)
}

// List handles GET /api/repos/{repo}/tasks.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	tasks, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// Get handles GET /api/repos/{repo}/tasks/{id}.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	task, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

type updateRequest struct {
	Status     *Status `json:"status"`
	Urgent     *bool   `json:"urgent"`
	AssignedTo *string `json:"assignedTo"`
}

// Update handles PATCH /api/repos/{repo}/tasks/{id}. Any field omitted
// from the request body (nil after decoding) is left unchanged.
func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	task, err := h.Store.Update(repo, id, req.Status, req.Urgent, req.AssignedTo)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handlers) repoExists(repo string) bool {
	repos, err := h.Repos.List()
	if err != nil {
		return false
	}
	return slices.Contains(repos, repo)
}

func (h *Handlers) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "404 task not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidRepo), errors.Is(err, ErrInvalidID), errors.Is(err, ErrInvalidStatus):
		http.Error(w, "400 Bad Request", http.StatusBadRequest)
	default:
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
