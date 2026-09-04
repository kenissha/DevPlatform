package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/deployment"
	"github.com/kenissha/DevPlatform/backend/internal/displaynames"
	"github.com/kenissha/DevPlatform/backend/internal/gittoken"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repoapi"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
	"github.com/kenissha/DevPlatform/backend/internal/taskboard"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

const testSecret = "test-secret"

func testAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testSecret), next)
	}
}

// newTestRouter wires every handler against one shared repostore, the way
// main.go does. Sharing the store matters: a repo created through POST
// /api/repos has to be visible to the task and merge request handlers in
// the same test, which separate per-handler stores would silently break.
// The returned access.Store is real (not nil) but starts with nobody
// restricted — identical behavior to a nil one (see access.Store's doc
// comment) until a test calls Set on it, which is what lets
// access-specific tests below restrict a subject without needing a
// separate router construction path.
func newTestRouter(t *testing.T) (*http.ServeMux, *repostore.Store, *access.Store) {
	t.Helper()
	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	auditLogger := audit.New(filepath.Join(dataDir, "audit.jsonl"))
	accessStore := access.NewStore(filepath.Join(dataDir, "access.json"))

	mrHandlers := &mergerequest.Handlers{
		Store:  mergerequest.NewStore(filepath.Join(dataDir, "merge-requests")),
		Repos:  store,
		Audit:  auditLogger,
		Access: accessStore,
	}
	repoHandlers := &repoapi.Handlers{Repos: store, Audit: auditLogger, Access: accessStore}
	taskHandlers := &taskboard.Handlers{
		Store:  taskboard.NewStore(filepath.Join(dataDir, "tasks")),
		Repos:  store,
		Audit:  auditLogger,
		Access: accessStore,
	}
	statsHandlers := &gitstats.Handlers{Repos: store}
	auditHandlers := &audit.Handlers{Logger: auditLogger, Access: accessStore}
	notifyHandlers := &notify.Handlers{Store: notify.NewStore(filepath.Join(dataDir, "notifications"))}
	// No Pipeline/Targets wired here: nothing in this file's tests exercises
	// a deploy approval — that full-pipeline path (real checkout, real
	// build, faked-only IIS swap) is covered in internal/deployment's own
	// tests instead.
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(dataDir, "deployments")),
		Repos:        store,
		Targets:      deployment.NewTargetStore(filepath.Join(dataDir, "deploy-targets.json")),
		CheckoutRoot: t.TempDir(),
		Audit:        auditLogger,
		Access:       accessStore,
	}
	gitTokenHandlers := &gittoken.Handlers{Store: gittoken.NewStore(filepath.Join(dataDir, "git-tokens.json"))}

	router := NewRouter(Deps{
		GitHandler:     http.NotFoundHandler(),
		AuthMiddleware: testAuthMiddleware(),
		MergeRequests:  mrHandlers,
		Repos:          repoHandlers,
		Tasks:          taskHandlers,
		Stats:          statsHandlers,
		Audit:          auditHandlers,
		Notifications:  notifyHandlers,
		Deployments:    deploymentHandlers,
		Users:          users.NewStore(filepath.Join(dataDir, "users.json")),
		Access:         accessStore,
		GitTokens:      gitTokenHandlers,
	})
	return router, store, accessStore
}

func signTestToken(t *testing.T, subject, role string) string {
	t.Helper()
	c := jwt.MapClaims{
		"sub":   subject,
		"email": subject + "@example.com",
		"role":  role,
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

// do issues an authenticated request through the router and returns the
// recorder.
func do(t *testing.T, router *http.ServeMux, method, path, subject, role string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHealthz_ReturnsOK(t *testing.T) {
	router, _, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if body := rec.Body.String(); body != `{"status":"ok"}`+"\n" {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`+"\n")
	}
}

func TestSecrets_Set_RequiresAdmin(t *testing.T) {
	dataDir := t.TempDir()
	// A fixed 32-byte key — this test only exercises HTTP routing/gating,
	// never real encryption strength, so a literal (not randomly
	// generated) key is fine here.
	key := []byte("01234567890123456789012345678901"[:32])
	vault := secretsvault.NewStore(filepath.Join(dataDir, "secrets"), key)

	router := NewRouter(Deps{
		GitHandler:     http.NotFoundHandler(),
		AuthMiddleware: testAuthMiddleware(),
		SecretsVault:   vault,
	})

	rec := do(t, router, http.MethodPut, "/api/secrets/sample/test", "dev-1", "developer", map[string]string{"content": "OAS_PASSWORD=hunter2"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("developer PUT /api/secrets: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodPut, "/api/secrets/sample/test", "admin-1", "admin", map[string]string{"content": "OAS_PASSWORD=hunter2"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin PUT /api/secrets: status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("response echoed the secret content back: %s", rec.Body.String())
	}

	got, err := vault.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "OAS_PASSWORD=hunter2" {
		t.Errorf("stored content = %q, want %q", got, "OAS_PASSWORD=hunter2")
	}
}

func TestMe_RejectsUnauthenticatedRequest(t *testing.T) {
	router, _, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMe_ReturnsAuthenticatedUser(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/api/me", "user-1", "developer", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got auth.User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got.Subject != "user-1" || got.Email != "user-1@example.com" || got.Role != auth.RoleDeveloper {
		t.Errorf("user = %+v, unexpected fields", got)
	}
}

func TestMe_IncludesDisplayNameOverride(t *testing.T) {
	dataDir := t.TempDir()
	displayNames := displaynames.NewStore(filepath.Join(dataDir, "display-names.json"))
	if err := displayNames.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	router := NewRouter(Deps{
		GitHandler:     http.NotFoundHandler(),
		AuthMiddleware: testAuthMiddleware(),
		Users:          users.NewStore(filepath.Join(dataDir, "users.json")),
		DisplayNames:   displayNames,
	})

	rec := do(t, router, http.MethodGet, "/api/me", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Rifat") {
		t.Errorf("body = %q, want it to contain the display name override", rec.Body.String())
	}
}

func TestReposCreate_RejectsNonAdminThroughRouter(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodPost, "/api/repos", "dev-1", "developer",
		map[string]string{"name": "intranet-backend"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestReposCreate_AllowsAdminThenListReturnsIt(t *testing.T) {
	router, _, _ := newTestRouter(t)

	createRec := do(t, router, http.MethodPost, "/api/repos", "admin-1", "admin",
		map[string]string{"name": "intranet-backend"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	listRec := do(t, router, http.MethodGet, "/api/repos", "admin-1", "admin", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var names []string
	if err := json.Unmarshal(listRec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 1 || names[0] != "intranet-backend" {
		t.Errorf("names = %v, want [intranet-backend]", names)
	}
}

func TestMergeRequestApprove_RejectsNonAdminThroughRouter(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodPost,
		"/api/repos/sample/merge-requests/0123456789abcdef/approve", "dev-1", "developer", nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestMergeRequestList_ReturnsNotFoundForUnknownRepo(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/api/repos/does-not-exist/merge-requests", "dev-1", "developer", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTasksCreate_AllowsAnyAuthenticatedUserThroughRouter(t *testing.T) {
	router, store, _ := newTestRouter(t)
	if _, err := store.Create("sample"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	rec := do(t, router, http.MethodPost, "/api/repos/sample/tasks", "dev-1", "developer",
		map[string]string{"title": "Fix login bug"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestTasksListAll_SpansReposAndFiltersByAssignee(t *testing.T) {
	router, store, _ := newTestRouter(t)
	for _, name := range []string{"repo-a", "repo-b"} {
		if _, err := store.Create(name); err != nil {
			t.Fatalf("failed to create test repo: %v", err)
		}
	}

	do(t, router, http.MethodPost, "/api/repos/repo-a/tasks", "dev-1", "developer",
		map[string]string{"title": "A tarafi", "assignedTo": "dev-2"})
	do(t, router, http.MethodPost, "/api/repos/repo-b/tasks", "dev-1", "developer",
		map[string]string{"title": "B tarafi", "assignedTo": "dev-3"})

	allRec := do(t, router, http.MethodGet, "/api/tasks", "dev-1", "developer", nil)
	if allRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", allRec.Code, http.StatusOK, allRec.Body.String())
	}
	var all []taskboard.Task
	if err := json.Unmarshal(allRec.Body.Bytes(), &all); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d tasks across repos, want 2", len(all))
	}

	filteredRec := do(t, router, http.MethodGet, "/api/tasks?assignedTo=dev-3", "dev-1", "developer", nil)
	var filtered []taskboard.Task
	if err := json.Unmarshal(filteredRec.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(filtered) != 1 || filtered[0].AssignedTo != "dev-3" {
		t.Fatalf("filtered = %+v, want a single task assigned to dev-3", filtered)
	}
}

func TestMergeRequestsListAll_ReturnsEmptyArrayWhenNoRepos(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/api/merge-requests", "dev-1", "developer", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// An empty JSON array, not null — the frontend maps over this directly.
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want %q", body, "[]\n")
	}
}

func TestUsers_AreRegisteredJustInTimeOnFirstCall(t *testing.T) {
	router, _, _ := newTestRouter(t)

	// Nobody has called /api/me yet, so the registry is empty.
	before := do(t, router, http.MethodGet, "/api/users", "dev-1", "developer", nil)
	if body := before.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want %q before anyone identifies themselves", body, "[]\n")
	}

	do(t, router, http.MethodGet, "/api/me", "dev-1", "developer", nil)
	do(t, router, http.MethodGet, "/api/me", "admin-1", "admin", nil)

	rec := do(t, router, http.MethodGet, "/api/users", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var list []users.User
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d users, want 2: %+v", len(list), list)
	}

	subjects := map[string]string{}
	for _, u := range list {
		subjects[u.Subject] = u.Role
	}
	if subjects["dev-1"] != "developer" || subjects["admin-1"] != "admin" {
		t.Errorf("registry = %+v, want dev-1/developer and admin-1/admin", subjects)
	}
}

func TestAudit_RecordsActionsTakenThroughTheAPI(t *testing.T) {
	router, _, _ := newTestRouter(t)

	createRec := do(t, router, http.MethodPost, "/api/repos", "admin-1", "admin",
		map[string]string{"name": "intranet-backend"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("repo create status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	taskRec := do(t, router, http.MethodPost, "/api/repos/intranet-backend/tasks", "dev-1", "developer",
		map[string]string{"title": "Login hatasi"})
	if taskRec.Code != http.StatusCreated {
		t.Fatalf("task create status = %d, want %d", taskRec.Code, http.StatusCreated)
	}

	rec := do(t, router, http.MethodGet, "/api/audit", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var events []audit.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}

	// Newest first: the task creation, then the repo creation.
	if events[0].Action != audit.ActionTaskCreated || events[0].Actor != "dev-1" {
		t.Errorf("newest event = %+v, want task.created by dev-1", events[0])
	}
	if events[1].Action != audit.ActionRepoCreated || events[1].Actor != "admin-1" {
		t.Errorf("older event = %+v, want repo.created by admin-1", events[1])
	}
	if events[1].Repo != "intranet-backend" {
		t.Errorf("repo = %q, want %q", events[1].Repo, "intranet-backend")
	}
}

func TestAudit_ReturnsEmptyArrayBeforeAnythingHappens(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/api/audit", "dev-1", "developer", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want %q", body, "[]\n")
	}
}

func TestActivity_ReturnsNotFoundForUnknownRepo(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/api/repos/does-not-exist/activity", "dev-1", "developer", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestActivity_ReturnsRequestedNumberOfDays(t *testing.T) {
	router, store, _ := newTestRouter(t)
	if _, err := store.Create("sample"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	rec := do(t, router, http.MethodGet, "/api/repos/sample/activity?days=5", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var days []gitstats.DayCount
	if err := json.Unmarshal(rec.Body.Bytes(), &days); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(days) != 5 {
		t.Fatalf("got %d days, want 5", len(days))
	}
}

func TestAccess_RestrictedDeveloperIsBlockedFromAnUngrantedRepoThroughEveryRepoScopedRoute(t *testing.T) {
	router, store, accessStore := newTestRouter(t)
	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}
	if err := accessStore.Set("dev-1", []string{"some-other-repo"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/repos/intranet-backend/branches"},
		{http.MethodGet, "/api/repos/intranet-backend/tasks"},
		{http.MethodGet, "/api/repos/intranet-backend/merge-requests"},
		{http.MethodGet, "/api/repos/intranet-backend/commits"},
		{http.MethodGet, "/api/repos/intranet-backend/deploy-targets"},
		{http.MethodGet, "/api/repos/intranet-backend/deployments"},
		{http.MethodGet, "/api/repos/intranet-backend/branches/feature-x/commits"},
		{http.MethodGet, "/api/repos/intranet-backend/branches/feature-x/preview"},
	} {
		rec := do(t, router, tc.method, tc.path, "dev-1", "developer", nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusForbidden)
		}
	}
}

func TestAccess_RestrictedDeveloperCanStillUseTheirGrantedRepo(t *testing.T) {
	router, store, accessStore := newTestRouter(t)
	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}
	if err := accessStore.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	rec := do(t, router, http.MethodGet, "/api/repos/intranet-backend/tasks", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAccess_AdminBypassesRestrictionEntirely(t *testing.T) {
	router, store, accessStore := newTestRouter(t)
	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}
	if err := accessStore.Set("admin-1", []string{"some-other-repo"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	rec := do(t, router, http.MethodGet, "/api/repos/intranet-backend/tasks", "admin-1", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAccess_ReposListIsNarrowedForARestrictedDeveloper(t *testing.T) {
	router, store, accessStore := newTestRouter(t)
	for _, name := range []string{"intranet-backend", "intranet-frontend"} {
		if _, err := store.Create(name); err != nil {
			t.Fatalf("failed to create test repo: %v", err)
		}
	}
	if err := accessStore.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	rec := do(t, router, http.MethodGet, "/api/repos", "dev-1", "developer", nil)
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(names) != 1 || names[0] != "intranet-backend" {
		t.Errorf("names = %v, want [intranet-backend]", names)
	}
}

func TestAccess_ManagementAPIIsAdminOnly(t *testing.T) {
	router, _, _ := newTestRouter(t)

	rec := do(t, router, http.MethodGet, "/api/access", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/access as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodPut, "/api/access/dev-2", "dev-1", "developer",
		map[string][]string{"repos": {"intranet-backend"}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT /api/access/dev-2 as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodDelete, "/api/access/dev-2", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE /api/access/dev-2 as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodDelete, "/api/access/dev-2", "admin-1", "admin", nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE /api/access/dev-2 as admin: status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestGitToken_RevokeIsAdminOnly(t *testing.T) {
	router, _, _ := newTestRouter(t)

	rec := do(t, router, http.MethodDelete, "/api/git-token/dev-2", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE /api/git-token/dev-2 as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodDelete, "/api/git-token/dev-2", "admin-1", "admin", nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE /api/git-token/dev-2 as admin: status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestDeployTargets_ManagementAPIIsAdminOnly(t *testing.T) {
	router, _, _ := newTestRouter(t)

	rec := do(t, router, http.MethodGet, "/api/deploy-targets", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/deploy-targets as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodPut, "/api/deploy-targets/sample/test", "dev-1", "developer",
		map[string]any{"recipe": "npm", "siteName": "A"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT /api/deploy-targets/sample/test as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodDelete, "/api/deploy-targets/sample/test", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE /api/deploy-targets/sample/test as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodGet, "/api/allowed-sites", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/allowed-sites as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodGet, "/api/deploy-targets", "admin-1", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/deploy-targets as admin: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGitToken_GenerateMineRejectsUnauthenticatedRequest(t *testing.T) {
	router, _, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/me/git-token", bytes.NewReader(nil))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGitToken_GenerateMineReturnsATokenForAnyAuthenticatedUser(t *testing.T) {
	router, _, _ := newTestRouter(t)
	rec := do(t, router, http.MethodPost, "/api/me/git-token", "dev-1", "developer", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Token == "" {
		t.Errorf("token = %q, want non-empty", got.Token)
	}
}

func TestAccess_AdminCanGrantThenRevokeThroughTheRouter(t *testing.T) {
	router, store, _ := newTestRouter(t)
	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	setRec := do(t, router, http.MethodPut, "/api/access/dev-1", "admin-1", "admin",
		map[string][]string{"repos": {"intranet-backend"}})
	if setRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d, body: %s", setRec.Code, http.StatusOK, setRec.Body.String())
	}

	afterGrantRec := do(t, router, http.MethodGet, "/api/repos/intranet-backend/tasks", "dev-1", "developer", nil)
	if afterGrantRec.Code != http.StatusOK {
		t.Fatalf("status after grant = %d, want %d", afterGrantRec.Code, http.StatusOK)
	}

	clearRec := do(t, router, http.MethodDelete, "/api/access/dev-1", "admin-1", "admin", nil)
	if clearRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want %d", clearRec.Code, http.StatusNoContent)
	}

	otherRec := do(t, router, http.MethodGet, "/api/repos/some-other-repo/tasks", "dev-1", "developer", nil)
	// "some-other-repo" doesn't exist, so this 404s rather than 200s — the
	// point of this assertion is that it's not a 403 anymore, proving Clear
	// actually removed the restriction rather than just emptying the list.
	if otherRec.Code == http.StatusForbidden {
		t.Errorf("status after clear = %d, want anything but 403 (restriction should be gone)", otherRec.Code)
	}
}
