package audit

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(events)
}
