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

// TestRequireTokenAndAccess_AllowsGitReceivePackForGrantedRepo exercises
// the write path end-to-end, not just as a table-row regex assertion:
// git-receive-pack (push) is the route where an authorization bypass
// would let an unauthorized user write to a repo, not merely read it,
// so it's the highest-value case to prove passes through the allow-list
// for a restricted user's own granted repo.
func TestRequireTokenAndAccess_AllowsGitReceivePackForGrantedRepo(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/git/intranet-backend.git/git-receive-pack", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (git-receive-pack for a granted repo must reach next)", rec.Code, http.StatusOK)
	}
}

// TestRequireTokenAndAccess_BlocksGitReceivePackForUngrantedRepo is the
// forbidden-side counterpart: a restricted user pushing to a repo they
// were never granted must be blocked before reaching the write handler.
func TestRequireTokenAndAccess_BlocksGitReceivePackForUngrantedRepo(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/git/intranet-backend.git/git-receive-pack", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (git-receive-pack for an ungranted repo must be blocked)", rec.Code, http.StatusForbidden)
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
		// The backslash-based bypass that got round 2 BLOCKED:
		// strings.Cut(rest, "/") only splits on a forward slash, so
		// "..\\secret.git" was a single opaque segment as far as
		// path.Clean (slash-only) was concerned — it never saw a
		// standalone ".." component to collapse, even though
		// Windows/go-billy's filesystem layer treats backslash as a
		// real separator and resolves this to the sibling repo on
		// disk. The allow-list rejects it because the remainder
		// "..\\secret.git/info/refs" matches none of the fixed
		// suffix patterns.
		{"/git/allowed.git/..\\secret.git/info/refs", "", false},
	}
	for _, c := range cases {
		repo, ok := repoNameFromPath(c.path)
		if repo != c.wantRepo || ok != c.wantOK {
			t.Errorf("repoNameFromPath(%q) = (%q, %v), want (%q, %v)", c.path, repo, ok, c.wantRepo, c.wantOK)
		}
	}
}

// TestRequireTokenAndAccess_RejectsBackslashTraversalPath is the
// end-to-end version of the backslash regression case above: sends the
// exact request shape that round 2's verification proved was a live
// bypass against the real gitserver handler stack (see
// task-2-report.md's "fix round 2" section), authenticated as a user
// granted only "allowed", and asserts it's rejected with 400 before any
// authorization decision is made.
func TestRequireTokenAndAccess_RejectsBackslashTraversalPath(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/git/allowed.git/..\\secret.git/info/refs", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (backslash traversal path must be rejected before authorization)", rec.Code, http.StatusBadRequest)
	}
}

// TestIsKnownSuffix_AllowsEveryRealGoGitRoute proves the allow-list
// accepts a representative remainder for each of the 11 route patterns
// go-git's backend/http.go httpServices table actually recognizes
// (verified against the pinned go-git/v6@v6.0.0-alpha.5 module), so the
// allow-list doesn't accidentally break real git operations by being
// too narrow.
func TestIsKnownSuffix_AllowsEveryRealGoGitRoute(t *testing.T) {
	sha1 := "b1689f4f906338b00adb9c83ff75dec7ed5fb972" // 40 hex chars
	cases := []string{
		"HEAD",
		"info/refs",
		"objects/info/alternates",
		"objects/info/http-alternates",
		"objects/info/packs",
		"objects/b1/689f4f906338b00adb9c83ff75dec7ed5fb972", // 2 + 40 hex chars
		"objects/pack/pack-" + sha1 + ".pack",
		"objects/pack/pack-" + sha1 + ".idx",
		"git-upload-pack",
		"git-receive-pack",
		"git-upload-archive",
		"", // bare "/git/<name>.git" with nothing after
	}
	for _, remainder := range cases {
		if !isKnownSuffix(remainder) {
			t.Errorf("isKnownSuffix(%q) = false, want true", remainder)
		}
	}
}

// TestIsKnownSuffix_RejectsAdversarialRemainders covers a few more
// remainder shapes that must not slip through the allow-list, beyond the
// specific traversal cases already covered above.
func TestIsKnownSuffix_RejectsAdversarialRemainders(t *testing.T) {
	cases := []string{
		"not-a-real-route",              // unrecognized literal
		"info\\refs",                    // backslash-containing
		"info/refs/../../../secret.git", // trailing traversal after a valid-looking prefix
		"objects/pack/pack-nothex.pack", // non-hex where hex is required
		"git-upload-pack/extra.git",     // unexpected extra .git-like suffix
		"HEAD.git",                      // extra .git suffix on an otherwise-valid literal
	}
	for _, remainder := range cases {
		if isKnownSuffix(remainder) {
			t.Errorf("isKnownSuffix(%q) = true, want false", remainder)
		}
	}
}
