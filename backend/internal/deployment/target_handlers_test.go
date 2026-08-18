package deployment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

// newDeployTargetsHandlers builds a bare Handlers wired only for
// deploy-target management — no git repo, no Pipeline, unlike
// newTestHandlers, since target CRUD never touches either.
func newDeployTargetsHandlers(t *testing.T) *Handlers {
	t.Helper()
	dataDir := t.TempDir()
	return &Handlers{
		Targets:      NewTargetStore(filepath.Join(dataDir, "deploy-targets.json")),
		AllowedSites: map[string]bool{"Approved Site": true},
	}
}

func TestListTargets_ReturnsEveryConfiguredTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/deploy-targets", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []Target
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "sample" {
		t.Errorf("got %+v, want one target for repo sample", got)
	}
}

func TestSetTarget_CreatesANewTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Approved Site", "keepVersions": 3})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	target, err := h.Targets.Find("sample", "test")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if target.SiteName != "Approved Site" || target.KeepVersions != 3 {
		t.Errorf("target = %+v, unexpected fields", target)
	}
}

func TestSetTarget_RejectsASiteNotOnTheAllowList(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Someone Else's Site"})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if _, err := h.Targets.Find("sample", "test"); err != ErrNoTarget {
		t.Error("expected the rejected target to not be persisted")
	}
}

func TestSetTarget_ReplacesAnExistingTargetRatherThanDuplicating(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "dotnet", "siteName": "Approved Site", "keepVersions": 9})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	list, err := h.Targets.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 || list[0].Recipe != deploy.RecipeDotnet || list[0].KeepVersions != 9 {
		t.Errorf("list = %+v, want exactly one replaced entry", list)
	}
}

func TestSetTarget_RejectsNonAdmin(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Approved Site"})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDeleteTarget_RemovesTheTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/deploy-targets/sample/test", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, err := h.Targets.Find("sample", "test"); err != ErrNoTarget {
		t.Error("expected the target to be gone")
	}
}

func TestListAllowedSites_ReturnsTheConfiguredList(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/allowed-sites", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || got[0] != "Approved Site" {
		t.Errorf("got %v, want [\"Approved Site\"]", got)
	}
}
