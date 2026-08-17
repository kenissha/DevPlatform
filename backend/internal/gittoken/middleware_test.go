package gittoken

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

func stubGitHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func newTestRequest(repo, subject, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/git/"+repo+".git/info/refs", nil)
	if subject != "" || token != "" {
		req.SetBasicAuth(subject, token)
	}
	return req
}

func TestRequireTokenAndAccess_RejectsMissingCredentials(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestRequireTokenAndAccess_RejectsInvalidToken(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	if _, err := tokens.Generate("dev-1"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("repo", "dev-1", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireTokenAndAccess_AllowsAnUnrestrictedUser(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_BlocksARestrictedUserFromAnUngrantedRepo(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"intranet-frontend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireTokenAndAccess_AllowsARestrictedUserTheirGrantedRepo(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_AdminBypassesRestriction(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("admin-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("admin-1", []string{"some-other-repo"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	if _, err := usersStore.Upsert("admin-1", "admin@example.com", "admin"); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "admin-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (admins always bypass)", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_RejectsPathWithNoRepoName(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRequireTokenAndAccess_RejectsTraversalPath(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"allowed"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/allowed.git/../secret.git/info/refs", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (traversal path must be rejected before authorization)", rec.Code, http.StatusBadRequest)
	}
}

// TestRequireTokenAndAccess_RejectsEncodedTraversalPath is the realistic
// version of the round-1 table case that tried (and failed) to exercise
// URL-decoding: it builds the request via httptest.NewRequest with a raw
// %2F-encoded path so that net/url performs the same decoding real git
// clients' requests go through, then confirms the decoded ".." is still
// rejected before authorization.
func TestRequireTokenAndAccess_RejectsEncodedTraversalPath(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"allowed"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/allowed.git/..%2Fsecret.git/info/refs", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (encoded traversal path must be rejected before authorization)", rec.Code, http.StatusBadRequest)
	}
}

// TestRequireTokenAndAccess_RejectsSuffixlessTraversalTarget proves the
// class of bypass round 1 missed: gitserver.NewHandler's loader runs
// strict=false, so go-git auto-appends ".git" while resolving a target
// that doesn't carry the suffix in the request path at all. Round 1's
// "reject a second literal .git in the remainder" check would let this
// path through, because "secret" never appears with ".git" attached.
func TestRequireTokenAndAccess_RejectsSuffixlessTraversalTarget(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"allowed"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/allowed.git/../secret/info/refs", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (suffix-less traversal target must be rejected before authorization)", rec.Code, http.StatusBadRequest)
	}
}

func TestRepoNameFromPath(t *testing.T) {
	cases := []struct {
		path     string
		wantRepo string
		wantOK   bool
	}{
		{"/git/intranet-backend.git/info/refs", "intranet-backend", true},
		{"/git/intranet-backend.git/git-upload-pack", "intranet-backend", true},
		{"/git/intranet-backend.git", "intranet-backend", true},
		{"/api/repos", "", false},
		{"/git/", "", false},
		{"/git/allowed.git/../secret.git/info/refs", "", false},
		{"/git/allowed.git/../secret/info/refs", "", false},
	}
	for _, c := range cases {
		repo, ok := repoNameFromPath(c.path)
		if repo != c.wantRepo || ok != c.wantOK {
			t.Errorf("repoNameFromPath(%q) = (%q, %v), want (%q, %v)", c.path, repo, ok, c.wantRepo, c.wantOK)
		}
	}
}
