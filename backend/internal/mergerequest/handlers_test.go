package mergerequest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

const testJWTSecret = "test-secret"

// newTestHandlers sets up a repostore with one repo ("sample") that has
// "main" and "feature-x" branches (feature-x one commit ahead), plus a
// fresh mergerequest.Store, and returns Handlers wired to both along with
// the bare repo's on-disk path (so tests can verify ref state directly via
// the git CLI, not just through the API's response body).
func newTestHandlers(t *testing.T) (*Handlers, string) {
	t.Helper()
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("sample")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "remote", "add", "origin", repoPath)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("line one\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "push", "origin", "main")

	runGit(t, work, "checkout", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "add line two")
	runGit(t, work, "push", "origin", "feature-x")

	h := &Handlers{
		Store: NewStore(filepath.Join(dataDir, "merge-requests")),
		Repos: repos,
	}
	return h, repoPath
}

// signTestToken mints a JWT this test suite's mux will accept, so handler
// tests exercise the real auth.RequireAuth middleware exactly as
// server.NewRouter wires it, rather than short-circuiting authentication.
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

func addAuth(r *http.Request, t *testing.T, subject, role string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	return r
}

func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repos/{repo}/merge-requests", authMW(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/repos/{repo}/merge-requests", authMW(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/repos/{repo}/merge-requests/{id}", authMW(http.HandlerFunc(h.Get)))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/approve",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Approve))))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/reject",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Reject))))
	return mux
}

func TestCreate_ReturnsCreatedRequest(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":        "Add line two",
		"sourceBranch": "feature-x",
		"targetBranch": "main",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var mr MergeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &mr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if mr.Author != "dev-1" || mr.Status != StatusOpen {
		t.Errorf("mr = %+v, unexpected fields", mr)
	}
}

func TestCreate_RejectsUnknownBranch(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":        "Bad request",
		"sourceBranch": "does-not-exist",
		"targetBranch": "main",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreate_AllowsNonexistentTargetBranch(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":        "First commit onto main",
		"sourceBranch": "feature-x",
		"targetBranch": "brand-new-branch",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestApprove_CreatesRepoDefaultBranchOnFirstMerge covers the scenario the
// smoke test caught: a freshly created repo has no commits at all, so
// "main" doesn't exist as a ref yet, and protectingLoader rejects every
// direct push to it unconditionally — meaning the merge-request flow is
// the only way a first commit ever reaches it. This exercises that whole
// path end to end: create, then approve.
func TestApprove_CreatesRepoDefaultBranchOnFirstMerge(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("empty-repo")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "remote", "add", "origin", repoPath)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("line one\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "push", "origin", "feature-x")

	h := &Handlers{
		Store: NewStore(filepath.Join(dataDir, "merge-requests")),
		Repos: repos,
	}
	mux := newMux(h)

	createBody, _ := json.Marshal(map[string]string{
		"title":        "First commit onto main",
		"sourceBranch": "feature-x",
		"targetBranch": "main",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/repos/empty-repo/merge-requests", bytes.NewReader(createBody))
	createReq = addAuth(createReq, t, "dev-1", "developer")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var created MergeRequest
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/repos/empty-repo/merge-requests/"+created.ID+"/approve", nil)
	approveReq = addAuth(approveReq, t, "admin-1", "admin")
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d, body: %s", approveRec.Code, http.StatusOK, approveRec.Body.String())
	}

	mainTip := runGit(t, repoPath, "rev-parse", "main")
	if strings.TrimSpace(mainTip) == "" {
		t.Fatal("expected main to exist on the server after approval, but rev-parse returned nothing")
	}
}

func TestCreate_RejectsUnknownRepo(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":        "Bad request",
		"sourceBranch": "feature-x",
		"targetBranch": "main",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/does-not-exist/merge-requests", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGet_ReturnsMergeRequestWithDiff(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/merge-requests/"+created.ID, nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var detail mergeRequestDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if detail.ID != created.ID {
		t.Errorf("ID = %q, want %q", detail.ID, created.ID)
	}
	if len(detail.Diff.Stats) == 0 {
		t.Error("expected non-empty diff stats")
	}
}

func TestApprove_RejectsNonAdmin(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/approve", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	reread, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Status != StatusOpen {
		t.Errorf("Status = %q, want %q (must stay unchanged after a rejected approval)", reread.Status, StatusOpen)
	}
}

func TestApprove_AllowsAdmin(t *testing.T) {
	h, repoPath := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/approve", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	reread, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", reread.Status, StatusApproved)
	}
	if reread.MergedCommit == "" {
		t.Error("expected MergedCommit to be recorded after a successful approval")
	}

	mainTip := runGit(t, repoPath, "rev-parse", "main")
	if reread.MergedCommit+"\n" != mainTip {
		t.Errorf("main tip after approval = %s, want %s", mainTip, reread.MergedCommit+"\n")
	}
}

func TestApprove_RejectsAlreadyDecidedRequest(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := h.Store.MarkApproved("sample", created.ID, "deadbeef"); err != nil {
		t.Fatalf("MarkApproved setup failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/approve", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestReject_AllowsAdmin(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/reject", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	reread, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Status != StatusRejected {
		t.Errorf("Status = %q, want %q", reread.Status, StatusRejected)
	}
}

func TestList_ReturnsCreatedRequests(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	if _, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/merge-requests", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var mrs []MergeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &mrs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("got %d merge requests, want 1", len(mrs))
	}
}

func TestCreate_NotifiesAllAdmins(t *testing.T) {
	h, _ := newTestHandlers(t)
	n := notify.NewStore(t.TempDir())
	h.Notify = n

	userStore := users.NewStore(filepath.Join(t.TempDir(), "users.json"))
	if _, err := userStore.Upsert("admin-1", "admin-1@example.com", "admin"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	if _, err := userStore.Upsert("admin-2", "admin-2@example.com", "admin"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	if _, err := userStore.Upsert("dev-1", "dev-1@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	h.Users = userStore

	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":        "Add line two",
		"sourceBranch": "feature-x",
		"targetBranch": "main",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	for _, admin := range []string{"admin-1", "admin-2"} {
		notifications, err := n.ListForUser(admin)
		if err != nil {
			t.Fatalf("ListForUser(%q) failed: %v", admin, err)
		}
		if len(notifications) != 1 {
			t.Fatalf("got %d notifications for %s, want 1", len(notifications), admin)
		}
		if notifications[0].Kind != "merge_request_opened" {
			t.Errorf("Kind = %q, want %q", notifications[0].Kind, "merge_request_opened")
		}
		if !strings.Contains(notifications[0].Message, "Add line two") {
			t.Errorf("Message = %q, want it to mention the merge request title", notifications[0].Message)
		}
	}

	devNotifications, err := n.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(devNotifications) != 0 {
		t.Errorf("Developer got %d notifications, want 0", len(devNotifications))
	}
}
