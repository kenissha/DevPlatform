package taskboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/kenissha/DevPlatform/backend/internal/access"
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
	// Access is optional; a nil Store means every caller sees every repo
	// (see internal/access). ListAll is the only place this package needs
	// it: every per-repo endpoint is instead gated by
	// access.RequireRepoAccess at the router level, but ListAll spans every
	// repo in one response, so it has to do its own narrowing to keep a
	// restricted caller from seeing another repo's tasks through it.
	Access *access.Store
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
		// Link points at the task list page, not a per-task detail route —
		// the frontend has no /repos/{repo}/tasks/{id} route to land on
		// (see RepoTasksPage in frontend/src/App.tsx), only the list.
		_, _ = h.Notify.Create(task.AssignedTo, "task_assigned",
			"Görev size atandı: "+task.Title+" ("+repo+")",
			"/repos/"+repo+"/tasks")
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
	if user, ok := auth.UserFromContext(r.Context()); ok && user.Role != auth.RoleAdmin {
		repos, err = h.Access.FilterRepos(user.Subject, repos)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
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
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Status      *Status   `json:"status"`
	Priority    *Priority `json:"priority"`
	DueDate     *string   `json:"dueDate"`
	AssignedTo  *string   `json:"assignedTo"`
}

func (r updateRequest) changes() Changes {
	return Changes{
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
		Priority:    r.Priority,
		DueDate:     r.DueDate,
		AssignedTo:  r.AssignedTo,
	}
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

	// Store.Update mutates the task in place and never hands back the
	// pre-update value, so a genuine reassignment ("dev-1" → "dev-2")
	// can't be told apart from a client resending the current assignee
	// unchanged (e.g. a UI that echoes the whole task back when it only
	// meant to flip urgent) purely from Update's return value. Fetching
	// the previous state first — the same Get-then-mutate shape Approve
	// already uses — is the least invasive way to make that distinction
	// without changing Store.Update's signature or its other callers.
	var previousAssignedTo string
	if req.AssignedTo != nil {
		previous, err := h.Store.Get(repo, id)
		if err != nil {
			h.writeStoreError(w, err)
			return
		}
		previousAssignedTo = previous.AssignedTo
	}

	task, err := h.Store.Update(repo, id, req.changes())
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionTaskUpdated, repo, task.ID,
			"Görev güncellendi: "+task.Title+" ("+describeUpdate(req)+")")
	}

	if req.AssignedTo != nil && *req.AssignedTo != "" && *req.AssignedTo != previousAssignedTo && h.Notify != nil {
		// Same list-page link as Create — see the comment there.
		_, _ = h.Notify.Create(task.AssignedTo, "task_assigned",
			"Bir görev size atandı: "+task.Title+" ("+repo+")",
			"/repos/"+repo+"/tasks")
	}

	writeJSON(w, http.StatusOK, task)
}

// Delete handles DELETE /api/repos/{repo}/tasks/{id}.
//
// Unlike every other endpoint in this package it is not open to all
// comers: deletion is the one action here that destroys information, so
// it is limited to the person who opened the task and to admins. Anyone
// else gets 403 — moving a task to "Bitti" is what the rest of the team
// does with work that is over.
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	// Read it first: the permission check needs the author, and the audit
	// line needs the title, neither of which survives the delete.
	task, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	if user.Role != auth.RoleAdmin && task.Author != user.Subject {
		http.Error(w, "403 bu görevi sadece açan kişi veya yönetici silebilir", http.StatusForbidden)
		return
	}

	if err := h.Store.Delete(repo, id); err != nil {
		h.writeStoreError(w, err)
		return
	}

	// The task is gone from disk, so this log line is the only remaining
	// record that it ever existed — worth keeping the title in it.
	_ = h.Audit.Log(user.Subject, audit.ActionTaskDeleted, repo, id, "Görev silindi: "+task.Title)

	w.WriteHeader(http.StatusNoContent)
}

type subtaskRequest struct {
	Title *string `json:"title"`
	Done  *bool   `json:"done"`
}

// AddSubtask handles POST /api/repos/{repo}/tasks/{id}/subtasks.
//
// Every subtask endpoint responds with the whole updated task rather than
// the item alone: a checklist is only meaningful as a set, and the caller
// wants the new progress ("3/5") anyway.
func (h *Handlers) AddSubtask(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	var req subtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	task, err := h.Store.AddSubtask(repo, id, *req.Title)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.logSubtaskChange(r, repo, task, "alt görev eklendi: "+*req.Title)
	writeJSON(w, http.StatusOK, task)
}

// UpdateSubtask handles PATCH .../subtasks/{subtaskID} — tick, untick or
// rename, whichever field the body carries.
func (h *Handlers) UpdateSubtask(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	subtaskID := r.PathValue("subtaskID")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	var req subtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	var task Task
	var err error
	summary := ""
	switch {
	case req.Done != nil:
		task, err = h.Store.SetSubtaskDone(repo, id, subtaskID, *req.Done)
		if *req.Done {
			summary = "alt görev tamamlandı"
		} else {
			summary = "alt görev geri açıldı"
		}
	case req.Title != nil:
		task, err = h.Store.RenameSubtask(repo, id, subtaskID, *req.Title)
		summary = "alt görev yeniden adlandırıldı"
	default:
		http.Error(w, "400 değiştirilecek bir alan yok", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.logSubtaskChange(r, repo, task, summary)
	writeJSON(w, http.StatusOK, task)
}

// RemoveSubtask handles DELETE .../subtasks/{subtaskID}.
func (h *Handlers) RemoveSubtask(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	subtaskID := r.PathValue("subtaskID")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	task, err := h.Store.RemoveSubtask(repo, id, subtaskID)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.logSubtaskChange(r, repo, task, "alt görev silindi")
	writeJSON(w, http.StatusOK, task)
}

// logSubtaskChange records a checklist change against the task, so the
// task's own history shows it alongside every other change. The progress
// is included because "3/5 tamamlandı" is the part somebody reading the
// history back actually wants.
func (h *Handlers) logSubtaskChange(r *http.Request, repo string, task Task, summary string) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return
	}
	done, total := task.SubtaskProgress()
	_ = h.Audit.Log(user.Subject, audit.ActionTaskUpdated, repo, task.ID,
		fmt.Sprintf("Görev güncellendi: %s (%s, %d/%d)", task.Title, summary, done, total))
}

type commentRequest struct {
	Body string `json:"body"`
}

// Comments handles GET /api/repos/{repo}/tasks/{id}/comments.
func (h *Handlers) Comments(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	list, err := h.Store.Comments(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AddComment handles POST /api/repos/{repo}/tasks/{id}/comments.
//
// Open to anyone with access to the repository: a conversation nobody but
// the assignee may join is not a conversation. The author of each comment
// is taken from the token, never from the body.
func (h *Handlers) AddComment(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req commentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	comment, err := h.Store.AddComment(repo, id, user.Subject, req.Body)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	task, taskErr := h.Store.Get(repo, id)
	title := id
	if taskErr == nil {
		title = task.Title
	}
	_ = h.Audit.Log(user.Subject, audit.ActionTaskCommented, repo, id, "Göreve yorum yapıldı: "+title)

	// Told to the two people who have a stake in it and are not the one
	// typing: whoever the work is assigned to, and whoever opened it.
	if taskErr == nil {
		h.notifyAboutComment(repo, task, user.Subject)
	}

	writeJSON(w, http.StatusCreated, comment)
}

// EditComment handles PATCH .../comments/{commentID} — author only.
func (h *Handlers) EditComment(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	commentID := r.PathValue("commentID")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req commentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	comment, err := h.Store.EditComment(repo, id, commentID, user.Subject, req.Body)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

// DeleteComment handles DELETE .../comments/{commentID}.
//
// Its author or an admin, the same rule task deletion uses: removing what
// somebody said is destructive, and everyone else's recourse is to reply.
func (h *Handlers) DeleteComment(w http.ResponseWriter, r *http.Request) {
	repo, id := r.PathValue("repo"), r.PathValue("id")
	commentID := r.PathValue("commentID")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.Store.DeleteComment(repo, id, commentID, user.Subject, user.Role == auth.RoleAdmin); err != nil {
		h.writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notifyAboutComment tells the assignee and the task's author that
// something was said, skipping whoever said it. A no-op without Notify.
func (h *Handlers) notifyAboutComment(repo string, task Task, commenter string) {
	if h.Notify == nil {
		return
	}
	message := "Görevine yorum yapıldı: " + task.Title
	link := "/repos/" + repo + "/tasks/" + task.ID

	told := map[string]bool{commenter: true}
	for _, subject := range []string{task.AssignedTo, task.Author} {
		if subject == "" || told[subject] {
			continue
		}
		told[subject] = true
		_, _ = h.Notify.Create(subject, "task_commented", message, link)
	}
}

// History handles GET /api/repos/{repo}/tasks/{id}/history — everything
// recorded against this task, newest first.
//
// Open to anyone with access to the repository, deliberately: the point
// of a history is that the person doing the work can see what was decided
// about it and by whom. Nothing here is more sensitive than the task
// itself, which they can already read.
func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	// Confirms the task exists (and that the id is well-formed) before
	// reporting a history for it — an unknown id should 404, not return
	// an empty list that reads as "nothing ever happened".
	if _, err := h.Store.Get(repo, id); err != nil {
		h.writeStoreError(w, err)
		return
	}

	events, err := h.Audit.ListForTarget(repo, id, 200)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// statusLabels renders a Status the way the audit log reads it out. The
// rest of the summary is Turkish prose, so the raw enum value ("awaiting_test")
// would be the one untranslated token in the sentence.
// priorityLabels renders a Priority the way the audit log reads it out —
// the same reason statusLabels exists: the raw enum value would be the one
// untranslated token in a Turkish sentence.
var priorityLabels = map[Priority]string{
	PriorityLow:      "düşük",
	PriorityNormal:   "normal",
	PriorityHigh:     "yüksek",
	PriorityCritical: "kritik",
}

var statusLabels = map[Status]string{
	StatusTodo:         "yapılacak",
	StatusInProgress:   "yapılıyor",
	StatusAwaitingTest: "test bekliyor",
	StatusDone:         "bitti",
}

// describeUpdate summarises which fields a PATCH actually changed, so the
// audit line says what happened rather than just "updated".
func describeUpdate(req updateRequest) string {
	parts := []string{}
	// Titles and descriptions are free text and can be long; the audit line
	// records that they changed, not what to. The task itself carries the
	// current value.
	if req.Title != nil {
		parts = append(parts, "başlık değişti")
	}
	if req.Description != nil {
		parts = append(parts, "açıklama değişti")
	}
	if req.Status != nil {
		label, ok := statusLabels[*req.Status]
		if !ok {
			label = string(*req.Status)
		}
		parts = append(parts, "durum → "+label)
	}
	if req.Priority != nil {
		label, ok := priorityLabels[*req.Priority]
		if !ok {
			label = string(*req.Priority)
		}
		parts = append(parts, "öncelik → "+label)
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			parts = append(parts, "bitiş tarihi kaldırıldı")
		} else {
			parts = append(parts, "bitiş tarihi → "+*req.DueDate)
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
	case errors.Is(err, ErrCommentNotOurs):
		// Covers both rules without overstating either: editing is
		// author-only, deleting is author-or-admin.
		http.Error(w, "403 bu yorum sana ait değil", http.StatusForbidden)
	case errors.Is(err, ErrEmptySubtask):
		http.Error(w, "400 alt görev başlığı boş olamaz", http.StatusBadRequest)
	case errors.Is(err, ErrTooManySubtasks):
		http.Error(w, "400 bir görevde en fazla 50 alt görev olabilir", http.StatusBadRequest)
	case errors.Is(err, ErrEmptyComment):
		http.Error(w, "400 yorum boş olamaz", http.StatusBadRequest)
	case errors.Is(err, ErrCommentTooLong):
		http.Error(w, "400 yorum çok uzun", http.StatusBadRequest)
	case errors.Is(err, ErrEmptyTitle):
		// Named rather than folded into the generic 400 below: this is the
		// one store rejection a person can hit by typing, so the panel has
		// something to show them instead of "Bad Request".
		http.Error(w, "400 görev başlığı boş olamaz", http.StatusBadRequest)
	case errors.Is(err, ErrInvalidPriority):
		http.Error(w, "400 geçersiz öncelik", http.StatusBadRequest)
	case errors.Is(err, ErrInvalidDueDate):
		http.Error(w, "400 bitiş tarihi GG.AA.YYYY biçiminde geçerli bir gün olmalı", http.StatusBadRequest)
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
