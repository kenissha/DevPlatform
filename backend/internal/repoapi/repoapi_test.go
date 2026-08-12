package repoapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

const testJWTSecret = "test-secret"

func signTestToken(t *testing.T, subject, role string) string {
	t.Helper()
	c := jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

// listAsUser round-trips req through the real auth.RequireAuth middleware
// (so auth.UserFromContext behaves exactly as it would in production)
// before calling h.List, letting these tests exercise List's Access
// filtering as an authenticated subject/role would experience it.
func listAsUser(h *Handlers, subject, role string, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	rec := httptest.NewRecorder()
	auth.RequireAuth([]byte(testJWTSecret), http.HandlerFunc(h.List)).ServeHTTP(rec, req)
	return rec
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found on PATH, skipping integration test")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput:\n%s", args, err, out)
	}
	return string(out)
}

func TestList_ReturnsRepoNamesSorted(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("zeta"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := repos.Create("alpha"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := &Handlers{Repos: repos}

	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Errorf("names = %v, want [alpha zeta]", names)
	}
}

func TestList_NarrowsToAllowedReposForARestrictedDeveloper(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := repos.Create("intranet-frontend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	h := &Handlers{Repos: repos, Access: accessStore}

	rec := listAsUser(h, "dev-1", "developer", t)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 1 || names[0] != "intranet-backend" {
		t.Errorf("names = %v, want [intranet-backend]", names)
	}
}

func TestList_AdminSeesEveryRepoEvenWhenRestricted(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := repos.Create("intranet-frontend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("admin-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	h := &Handlers{Repos: repos, Access: accessStore}

	rec := listAsUser(h, "admin-1", "admin", t)

	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("names = %v, want both repos (admins bypass restriction)", names)
	}
}

func TestList_UnrestrictedDeveloperSeesEveryRepo(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := &Handlers{Repos: repos, Access: access.NewStore(t.TempDir() + "/access.json")}

	rec := listAsUser(h, "dev-1", "developer", t)

	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("names = %v, want the one repo (unrestricted by default)", names)
	}
}

func TestCreate_CreatesRepo(t *testing.T) {
	repos := repostore.New(t.TempDir())
	h := &Handlers{Repos: repos}

	body, _ := json.Marshal(map[string]string{"name": "intranet-backend"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	names, err := repos.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 1 || names[0] != "intranet-backend" {
		t.Errorf("names = %v, want [intranet-backend]", names)
	}
}

func TestCreate_RejectsInvalidName(t *testing.T) {
	repos := repostore.New(t.TempDir())
	h := &Handlers{Repos: repos}

	body, _ := json.Marshal(map[string]string{"name": "../escape"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreate_RejectsDuplicateName(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := &Handlers{Repos: repos}

	body, _ := json.Marshal(map[string]string{"name": "intranet-backend"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestBranches_ReturnsBranchNamesSorted(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("sample")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "remote", "add", "origin", repoPath)
	runGit(t, work, "commit", "--allow-empty", "-m", "initial commit")
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "checkout", "-b", "feature-x")
	runGit(t, work, "commit", "--allow-empty", "-m", "feature work")
	runGit(t, work, "push", "origin", "feature-x")

	h := &Handlers{Repos: repos}
	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/branches", nil)
	req.SetPathValue("repo", "sample")
	rec := httptest.NewRecorder()
	h.Branches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 2 || names[0] != "feature-x" || names[1] != "main" {
		t.Errorf("names = %v, want [feature-x main]", names)
	}
}

func TestBranches_ReturnsNotFoundForUnknownRepo(t *testing.T) {
	repos := repostore.New(t.TempDir())
	h := &Handlers{Repos: repos}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/does-not-exist/branches", nil)
	req.SetPathValue("repo", "does-not-exist")
	rec := httptest.NewRecorder()
	h.Branches(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
