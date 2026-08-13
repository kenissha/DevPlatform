package audit

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

// Handlers exposes the audit log over HTTP, meant to be mounted by
// internal/server behind auth.RequireAuth.
//
// Readable by any authenticated user, not Admin-only: the log records the
// same actions the task board and merge request screens already show
// (who opened what, who approved what), and the design doc's whole
// premise is that both people can see who is working on what. Gating it
// would hide from a Geliştirici only the record of decisions that already
// affect them.
type Handlers struct {
	Logger *Logger
	// Access is optional; a nil Store means every caller sees every repo's
	// events (see internal/access). Events span every repo already, the
	// same cross-repo shape as taskboard/mergerequest/deployment's ListAll,
	// so List has to do its own narrowing to keep a restricted caller from
	// reading another repo's events through the log — see
	// taskboard.Handlers.Access's doc comment.
	Access *access.Store
}

// List handles GET /api/audit?limit=N, newest first.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	events, err := h.Logger.List(limit)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok && user.Role != auth.RoleAdmin {
		filtered := []Event{}
		for _, e := range events {
			allowed, err := h.Access.CanAccess(user.Subject, e.Repo)
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			}
			if allowed {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(events)
}
