package deployment

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

// setTargetRequest is the body PUT /api/deploy-targets/{repo}/{environment}
// expects — everything about a Target except its (repo, environment)
// key, which comes from the URL instead, so the body can never disagree
// with the path about which target is being written.
type setTargetRequest struct {
	Recipe        deploy.Recipe `json:"recipe"`
	SiteName      string        `json:"siteName"`
	SecretsTarget string        `json:"secretsTarget,omitempty"`
	KeepVersions  int           `json:"keepVersions"`
}

// ListTargets handles GET /api/deploy-targets, returning every configured
// deploy target for the admin panel's management table.
func (h *Handlers) ListTargets(w http.ResponseWriter, r *http.Request) {
	list, err := h.Targets.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// SetTarget handles PUT /api/deploy-targets/{repo}/{environment},
// creating or replacing the deploy target for that pair. siteName must
// be one of the server's ops-approved allow-list (h.AllowedSites) — see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
// "Güvenlik" section for why that list is never panel-editable.
func (h *Handlers) SetTarget(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if repo == "" || environment == "" {
		http.Error(w, "400 repo and environment are required", http.StatusBadRequest)
		return
	}

	var req setTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	target := Target{
		Repo:          repo,
		Environment:   environment,
		Recipe:        req.Recipe,
		SiteName:      req.SiteName,
		SecretsTarget: req.SecretsTarget,
		KeepVersions:  req.KeepVersions,
	}
	if err := h.Targets.Set(target, h.AllowedSites); err != nil {
		if errors.Is(err, ErrInvalidTarget) {
			http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

// DeleteTarget handles DELETE /api/deploy-targets/{repo}/{environment}.
func (h *Handlers) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if repo == "" || environment == "" {
		http.Error(w, "400 repo and environment are required", http.StatusBadRequest)
		return
	}
	if err := h.Targets.Delete(repo, environment); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAllowedSites handles GET /api/allowed-sites — the read-only,
// ops-managed list of IIS site names a deploy target's siteName may
// name. Lets the panel render a dropdown instead of free text; the list
// itself can only be changed by editing DEVPLATFORM_ALLOWED_SITES_FILE
// on the server and restarting, never through this or any other API.
func (h *Handlers) ListAllowedSites(w http.ResponseWriter, r *http.Request) {
	sites := make([]string, 0, len(h.AllowedSites))
	for name := range h.AllowedSites {
		sites = append(sites, name)
	}
	sort.Strings(sites)
	writeJSON(w, http.StatusOK, sites)
}
