package gitserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// withAdminContext wraps h so every request it serves carries
// WithAdmin(ctx, true) — standing in for what gittoken.RequireTokenAndAccess
// does in production once it's determined the caller is an Admin, without
// pulling that package's full Basic-Auth/token-verification machinery
// into this package's tests (gittoken already imports gitserver for
// Prefix, so the reverse import isn't available here anyway).
func withAdminContext(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(WithAdmin(r.Context(), true)))
	})
}

func TestPush_DirectlyToMain_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("protected"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected.git")

	cmd := exec.Command("git", "push", "origin", "main")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `git push origin main` to fail, but it succeeded. Output:\n%s", out)
	}
	t.Logf("git push to main correctly failed with:\n%s", out)

	// The real assertion: main must NOT exist on the server afterward,
	// regardless of exactly how git's CLI worded the rejection.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "protected")
	cmd = exec.Command("git", "clone", srv.URL+"/protected.git", cloneTarget)
	cmd.Dir = verifyDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to clone for verification: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/main exists on the server, but the push to it should have been rejected")
	}
}

// TestPush_DirectlyToMainCaseVariant_IsRejected guards against a
// case-insensitive-filesystem bypass: go-git's FilesystemLoader storer
// resolves refs as filesystem paths, and on a case-insensitive filesystem
// (this project's target OS, Windows) "refs/heads/Main" and
// "refs/heads/main" resolve to the same on-disk location. An exact-string
// guard would let a push to "Main" sail through un-blocked while still
// colliding with and overwriting "main" on disk. The protection must
// compare ref names case-insensitively to close this off.
func TestPush_DirectlyToMainCaseVariant_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("protected4"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected4.git")

	cmd := exec.Command("git", "push", "origin", "HEAD:refs/heads/Main")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `git push origin HEAD:refs/heads/Main` to fail, but it succeeded. Output:\n%s", out)
	}
	t.Logf("git push to refs/heads/Main correctly failed with:\n%s", out)

	// The real assertion: refs/heads/Main must NOT exist on the server
	// afterward, regardless of exactly how git's CLI worded the rejection.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "protected4")
	cmd = exec.Command("git", "clone", srv.URL+"/protected4.git", cloneTarget)
	cmd.Dir = verifyDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to clone for verification: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/Main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/Main exists on the server, but the push to it should have been rejected")
	}
}

func TestPush_DeleteMain_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	bareRepoPath, err := store.Create("protected3")
	if err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")

	// Seed "main" onto the server by pushing straight to the bare repo's
	// path on disk (git's local file transport), bypassing the HTTP server
	// and its protection entirely. This is test setup, not the behavior
	// under test: this plan has no HTTP-reachable way to get "main" onto
	// the server (that's the point of TestPush_DirectlyToMain_IsRejected),
	// so seeding it locally is the only way to construct a repo state
	// where "delete main" has an existing ref to actually try deleting.
	runGit(t, work, "push", bareRepoPath, "main")

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected3.git")

	cmd := exec.Command("git", "push", "origin", "--delete", "main")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `git push origin --delete main` to fail, but it succeeded. Output:\n%s", out)
	}
	t.Logf("git push --delete main correctly failed with:\n%s", out)

	// Confirm main is still there after the rejected delete attempt.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "protected3")
	runGit(t, verifyDir, "clone", srv.URL+"/protected3.git", cloneTarget)
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err != nil {
		t.Fatal("refs/heads/main no longer exists on the server after the rejected delete attempt")
	}
}

func TestPush_ToFeatureBranch_StillSucceeds(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("protected2"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-y")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected2.git")
	runGit(t, work, "push", "origin", "feature-y")
	// runGit already fails the test via t.Fatalf if this push is rejected.
}

// TestPush_DirectlyToMain_AdminContextAllowsIt proves the other half of
// the protection: a caller whose request context carries WithAdmin(true)
// (what gittoken.RequireTokenAndAccess sets once it's determined the
// caller is an Admin) CAN push straight to main — the whole reason this
// context plumbing exists. Without withAdminContext wrapping NewHandler
// here, this exact push is what TestPush_DirectlyToMain_IsRejected proves
// fails.
func TestPush_DirectlyToMain_AdminContextAllowsIt(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("adminpush"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(withAdminContext(NewHandler(dataDir)))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/adminpush.git")

	// runGit fails the test via t.Fatalf if this push is rejected — the
	// assertion here IS that it doesn't need to fall back to inspecting
	// stderr or re-cloning to verify, unlike the rejection-side tests
	// above, since a clean exit already proves the ref update went
	// through the smart-HTTP protocol's normal success path.
	runGit(t, work, "push", "origin", "main")

	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "adminpush")
	runGit(t, verifyDir, "clone", srv.URL+"/adminpush.git", cloneTarget)
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err != nil {
		t.Fatal("refs/heads/main does not exist on the server after an admin-context push")
	}
}

// TestPush_DeleteMain_AdminContextAllowsIt mirrors
// TestPush_DirectlyToMain_AdminContextAllowsIt for deletion — an Admin
// bypasses RemoveReference's guard the same way SetReference's is
// bypassed, since protectingLoader applies allowProtected uniformly
// across both (see protectedStorer's doc comment).
func TestPush_DeleteMain_AdminContextAllowsIt(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	bareRepoPath, err := store.Create("adminpushdelete")
	if err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")

	// Seed "main" the same way TestPush_DeleteMain_IsRejected does — see
	// that test's comment for why this has to bypass the HTTP server.
	runGit(t, work, "push", bareRepoPath, "main")

	srv := httptest.NewServer(withAdminContext(NewHandler(dataDir)))
	defer srv.Close()
	runGit(t, work, "remote", "add", "origin", srv.URL+"/adminpushdelete.git")

	runGit(t, work, "push", "origin", "--delete", "main")

	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "adminpushdelete")
	runGit(t, verifyDir, "clone", srv.URL+"/adminpushdelete.git", cloneTarget)
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/main still exists on the server after an admin-context delete")
	}
}
