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

	"github.com/kenissha/DevPlatform/backend/internal/access"
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
	mux.Handle("GET /api/merge-requests", authMW(http.HandlerFunc(h.ListAll)))
	mux.Handle("GET /api/repos/{repo}/branches/{branch}/preview", authMW(http.HandlerFunc(h.BranchPreview)))
	return mux
}

func TestListAll_NarrowsToAllowedReposForARestrictedDeveloper(t *testing.T) {
	h, _ := newTestHandlers(t)
	if _, err := h.Repos.Create("other"); err != nil {
		t.Fatalf("failed to create second repo: %v", err)
	}
	// Seeded directly through the store rather than the HTTP handler: Create
	// validates the source/target branches exist via a real git repo, which
	// this test doesn't need — only that ListAll's Access filtering hides an
	// item belonging to a repo the caller isn't allowed to see.
	if _, err := h.Store.Create("other", "unrelated", "feature", "main", "dev-2"); err != nil {
		t.Fatalf("failed to seed merge request: %v", err)
	}
	h.Access = access.NewStore(t.TempDir() + "/access.json")
	if err := h.Access.Set("dev-1", []string{"other"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
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
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/merge-requests", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var all []MergeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// dev-1 is restricted to "other". "sample"'s merge request (the one
	// just created) must be hidden; "other"'s seeded one must show through.
	if len(all) != 1 || all[0].Repo != "other" {
		t.Errorf("got %v, want exactly the one merge request in repo %q", all, "other")
	}
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

// TestApprove_AllowsAdmin confirms Approve performs no git operation —
// main's tip on the server must be completely unchanged by it (see
// Handlers.Approve's doc comment: an Admin's own direct push is what
// actually advances main now, not this endpoint).
func TestApprove_AllowsAdmin(t *testing.T) {
	h, repoPath := newTestHandlers(t)
	mux := newMux(h)

	mainTipBefore := runGit(t, repoPath, "rev-parse", "main")

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	body, _ := json.Marshal(decisionRequest{Note: "gerçek merge tamamlandı, main'e push edildi"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/approve", bytes.NewReader(body))
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
	if reread.Note != "gerçek merge tamamlandı, main'e push edildi" {
		t.Errorf("Note = %q, want the submitted note", reread.Note)
	}

	mainTipAfter := runGit(t, repoPath, "rev-parse", "main")
	if mainTipAfter != mainTipBefore {
		t.Errorf("main tip changed after approval (before=%s after=%s) — Approve must not touch git at all", mainTipBefore, mainTipAfter)
	}
}

func TestApprove_RejectsAlreadyDecidedRequest(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := h.Store.SetStatus("sample", created.ID, StatusApproved, ""); err != nil {
		t.Fatalf("SetStatus setup failed: %v", err)
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

	body, _ := json.Marshal(decisionRequest{Note: "testler eksik, tamamla ve tekrar aç"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/"+created.ID+"/reject", bytes.NewReader(body))
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
	if reread.Note != "testler eksik, tamamla ve tekrar aç" {
		t.Errorf("Note = %q, want the submitted note", reread.Note)
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

// TestBranchPreview_NoExistingRequestReturnsJustTheDiff is the branch
// detail page's default case: nobody has opened an İnceleme İsteği for
// this branch yet, so it should just show the diff and let the
// developer open one.
func TestBranchPreview_NoExistingRequestReturnsJustTheDiff(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/branches/feature-x/preview", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var preview branchPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(preview.Diff.Stats) == 0 {
		t.Error("expected non-empty diff stats")
	}
	if preview.OpenRequest != nil {
		t.Errorf("OpenRequest = %+v, want nil", preview.OpenRequest)
	}
	if preview.LastRejected != nil {
		t.Errorf("LastRejected = %+v, want nil", preview.LastRejected)
	}
}

// TestBranchPreview_ReturnsTheOpenRequestForThisBranch confirms the page
// can point the developer at their already-pending request instead of
// letting them open a duplicate.
func TestBranchPreview_ReturnsTheOpenRequestForThisBranch(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/branches/feature-x/preview", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var preview branchPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if preview.OpenRequest == nil || preview.OpenRequest.ID != created.ID {
		t.Errorf("OpenRequest = %+v, want the request just created (%s)", preview.OpenRequest, created.ID)
	}
}

// TestBranchPreview_ReturnsTheMostRecentRejectionWithItsNote lets the
// branch page show the developer why their last attempt was sent back,
// without them having to dig through the full İnceleme İstekleri list.
func TestBranchPreview_ReturnsTheMostRecentRejectionWithItsNote(t *testing.T) {
	h, _ := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Add line two", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := h.Store.SetStatus("sample", created.ID, StatusRejected, "testler eksik"); err != nil {
		t.Fatalf("SetStatus failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/branches/feature-x/preview", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var preview branchPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if preview.OpenRequest != nil {
		t.Errorf("OpenRequest = %+v, want nil (this one was rejected, not left open)", preview.OpenRequest)
	}
	if preview.LastRejected == nil || preview.LastRejected.Note != "testler eksik" {
		t.Errorf("LastRejected = %+v, want the rejected request with its note", preview.LastRejected)
	}
}
