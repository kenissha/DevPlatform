package taskboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes the task board API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth. Unlike merge
// requests, no endpoint here is Admin-gated — see Store.Update's doc
// comment for why.
type Handlers struct {
	Store *Store
	Repos *repostore.Store
	// Audit is optional; a nil Logger records nothing (see internal/audit).
	Audit *audit.Logger
	// Notify is optional; a nil Store creates no notifications (see
	// internal/notify). Unlike Logger, notify.Store is not itself
	// nil-receiver-safe, so call sites check h.Notify != nil before use.
	Notify *notify.Store
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

	_ = h.Audit.Log(user.Subject, audit.ActionTaskCreated, repo, task.ID, "Görev açıldı: "+task.Title)

	if task.AssignedTo != "" && h.Notify != nil {
		_, _ = h.Notify.Create(task.AssignedTo, "task_assigned",
			"Görev size atandı: "+task.Title+" ("+repo+")",
			"/repos/"+repo+"/tasks/"+task.ID)
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

// ListAll handles GET /api/tasks — every repository's tasks in one
// response, newest first, optionally narrowed with ?assignedTo=. This is
// what the dashboard's per-person view reads: "kimin ne üzerinde
// çalıştığını görebilmek" is a cross-repo question, and answering it from
// the per-repo endpoint would mean the client fanning out one request per
// repository.
func (h *Handlers) ListAll(w http.ResponseWriter, r *http.Request) {
	repos, err := h.Repos.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	assignedTo := r.URL.Query().Get("assignedTo")

	all := []Task{}
	for _, repo := range repos {
		tasks, err := h.Store.List(repo)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, t := range tasks {
			if assignedTo != "" && t.AssignedTo != assignedTo {
				continue
			}
			all = append(all, t)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, all)
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

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionTaskUpdated, repo, task.ID,
			"Görev güncellendi: "+task.Title+" ("+describeUpdate(req)+")")
	}

	if req.AssignedTo != nil && *req.AssignedTo != "" && h.Notify != nil {
		_, _ = h.Notify.Create(task.AssignedTo, "task_assigned",
			"Bir görev size atandı: "+task.Title+" ("+repo+")",
			"/repos/"+repo+"/tasks/"+task.ID)
	}

	writeJSON(w, http.StatusOK, task)
}

// statusLabels renders a Status the way the audit log reads it out. The
// rest of the summary is Turkish prose, so the raw enum value ("awaiting_test")
// would be the one untranslated token in the sentence.
var statusLabels = map[Status]string{
	StatusInProgress:   "yapılıyor",
	StatusAwaitingTest: "test bekliyor",
	StatusDone:         "bitti",
}

// describeUpdate summarises which fields a PATCH actually changed, so the
// audit line says what happened rather than just "updated".
func describeUpdate(req updateRequest) string {
	parts := []string{}
	if req.Status != nil {
		label, ok := statusLabels[*req.Status]
		if !ok {
			label = string(*req.Status)
		}
		parts = append(parts, "durum → "+label)
	}
	if req.Urgent != nil {
		if *req.Urgent {
			parts = append(parts, "acil işaretlendi")
		} else {
			parts = append(parts, "acil kaldırıldı")
		}
	}
	if req.AssignedTo != nil {
		if *req.AssignedTo == "" {
			parts = append(parts, "atama kaldırıldı")
		} else {
			parts = append(parts, "atandı → "+*req.AssignedTo)
		}
	}
	if len(parts) == 0 {
		return "değişiklik yok"
	}
	return strings.Join(parts, ", ")
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
