package repoapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repodesc"
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

// decodeRepoNames pulls just the names out of a GET /api/repos body. The
// endpoint returns objects (name + description); most of these tests only
// care about which repos came back and in what order.
func decodeRepoNames(t *testing.T, body []byte) []string {
	t.Helper()
	var repos []repoResponse
	if err := json.Unmarshal(body, &repos); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		names = append(names, r.Name)
	}
	return names
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
	names := decodeRepoNames(t, rec.Body.Bytes())
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
	names := decodeRepoNames(t, rec.Body.Bytes())
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

	names := decodeRepoNames(t, rec.Body.Bytes())
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

	names := decodeRepoNames(t, rec.Body.Bytes())
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

// ------------------------------------------------------- descriptions

func describeAsAdmin(h *Handlers, repo, description string, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"description": description})
	req := httptest.NewRequest(http.MethodPut, "/api/repos/"+repo+"/description", bytes.NewReader(body))
	req.SetPathValue("repo", repo)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "admin-1", "admin"))
	rec := httptest.NewRecorder()
	auth.RequireAuth([]byte(testJWTSecret), http.HandlerFunc(h.Describe)).ServeHTTP(rec, req)
	return rec
}

func TestList_CarriesEachReposDescription(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("alpha"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := repos.Create("zeta"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	descriptions := repodesc.NewStore(t.TempDir() + "/repo-descriptions.json")
	if err := descriptions.Set("alpha", "İlk proje"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	h := &Handlers{Repos: repos, Descriptions: descriptions}

	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	var got []repoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[0].Description != "İlk proje" {
		t.Errorf("alpha = %+v, want its description", got[0])
	}
	// A repo with no description carries an empty string, not a missing
	// key — the frontend renders the field unconditionally.
	if got[1].Name != "zeta" || got[1].Description != "" {
		t.Errorf("zeta = %+v, want an empty description", got[1])
	}
}

// The list is what the sidebar and every repo picker are built on. A
// descriptions store that can't be read must cost the descriptions, not
// the list.
func TestList_StillAnswersWithoutADescriptionsStore(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("alpha"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := &Handlers{Repos: repos}

	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if names := decodeRepoNames(t, rec.Body.Bytes()); len(names) != 1 || names[0] != "alpha" {
		t.Errorf("names = %v, want [alpha]", names)
	}
}

func TestCreate_StoresTheDescriptionItWasGiven(t *testing.T) {
	repos := repostore.New(t.TempDir())
	descriptions := repodesc.NewStore(t.TempDir() + "/repo-descriptions.json")
	h := &Handlers{Repos: repos, Descriptions: descriptions}

	body, _ := json.Marshal(map[string]string{"name": "alpha", "description": "İlk proje"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	if got := descriptions.Get("alpha"); got != "İlk proje" {
		t.Errorf("stored description = %q, want %q", got, "İlk proje")
	}
}

// A description long enough to be rejected must be caught before the repo
// is created. Otherwise the caller gets an error while a repository they
// believe doesn't exist sits on disk — and the name is then taken.
func TestCreate_RejectsATooLongDescriptionWithoutCreatingTheRepo(t *testing.T) {
	repos := repostore.New(t.TempDir())
	h := &Handlers{Repos: repos, Descriptions: repodesc.NewStore(t.TempDir() + "/repo-descriptions.json")}

	body, _ := json.Marshal(map[string]string{
		"name":        "alpha",
		"description": strings.Repeat("ğ", repodesc.MaxLength+1),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	names, err := repos.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("repos on disk = %v, want none — the repo was created despite the rejection", names)
	}
}

func TestDescribe_SetsAndThenClearsADescription(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("alpha"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	descriptions := repodesc.NewStore(t.TempDir() + "/repo-descriptions.json")
	h := &Handlers{Repos: repos, Descriptions: descriptions}

	if rec := describeAsAdmin(h, "alpha", "İlk proje", t); rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if got := descriptions.Get("alpha"); got != "İlk proje" {
		t.Errorf("description = %q, want %q", got, "İlk proje")
	}

	// Clearing goes through the same route — there is no separate delete.
	if rec := describeAsAdmin(h, "alpha", "", t); rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if got := descriptions.Get("alpha"); got != "" {
		t.Errorf("description after clearing = %q, want empty", got)
	}
}

func TestDescribe_RejectsAnUnknownRepo(t *testing.T) {
	h := &Handlers{
		Repos:        repostore.New(t.TempDir()),
		Descriptions: repodesc.NewStore(t.TempDir() + "/repo-descriptions.json"),
	}

	if rec := describeAsAdmin(h, "yok-boyle-bir-repo", "açıklama", t); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

func TestDescribe_RejectsATooLongDescription(t *testing.T) {
	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("alpha"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := &Handlers{Repos: repos, Descriptions: repodesc.NewStore(t.TempDir() + "/repo-descriptions.json")}

	long := strings.Repeat("ğ", repodesc.MaxLength+1)
	if rec := describeAsAdmin(h, "alpha", long, t); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}
