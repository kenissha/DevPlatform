package gitserver

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

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

func TestClone_AfterInitialPush(t *testing.T) {
	requireGit(t)

	// Deliberately NOT testing a clone of a genuinely empty (zero-ref)
	// repo here: go-git v6's server-side advertisement path for that case
	// wasn't independently confirmed during planning (see Global
	// Constraints), so this test pushes one commit first to stay on
	// well-understood ground — "can a client clone a repo that has
	// content" — and isolates the zero-ref edge case out of this task's
	// pass/fail bar. If you want to explore the zero-ref case, do it as
	// an experiment outside this test, not as a change to its assertion.
	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("hello"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	seed := t.TempDir()
	runGit(t, seed, "init", "-b", "main")
	runGit(t, seed, "config", "user.email", "test@example.com")
	runGit(t, seed, "config", "user.name", "Test")
	// Windows Git commonly defaults core.autocrlf to true, which would
	// rewrite the committed LF line ending to CRLF on checkout and make
	// this test about local git config rather than the server. Pin it off
	// so the byte content round-trips exactly.
	runGit(t, seed, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, seed, "add", "README.md")
	runGit(t, seed, "commit", "-m", "seed commit")
	runGit(t, seed, "remote", "add", "origin", srv.URL+"/hello.git")
	runGit(t, seed, "push", "origin", "main")

	cloneDir := t.TempDir()
	cloneTarget := filepath.Join(cloneDir, "hello")
	runGit(t, cloneDir, "clone", srv.URL+"/hello.git", cloneTarget)

	if _, err := os.Stat(filepath.Join(cloneTarget, "README.md")); err != nil {
		t.Errorf("expected cloned README.md to exist: %v", err)
	}
}

func TestPushAndClone_RoundTrip(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("roundtrip"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	// Prepare a local repo with one commit, on a non-default branch name
	// (this task doesn't test branch protection yet — that's Task 4 — but
	// pushing to "main" here would be testing behavior this task doesn't
	// implement any opinion about yet, so use a feature branch name to
	// keep this test's scope to "does push/clone work at all").
	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	// See the matching comment in TestClone_AfterInitialPush: pin off
	// Windows Git's default core.autocrlf so the byte content round-trips
	// exactly instead of the test asserting on local git config.
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/roundtrip.git")
	runGit(t, work, "push", "origin", "feature-x")

	// Clone into a second directory and confirm the pushed content arrived.
	cloneDir := t.TempDir()
	cloneTarget := filepath.Join(cloneDir, "roundtrip")
	// -c core.autocrlf=false must be set on the clone invocation itself
	// (not after) since checkout happens as part of clone, before any
	// post-hoc `git config` in the new repo would take effect.
	runGit(t, cloneDir, "-c", "core.autocrlf=false", "clone", "--branch", "feature-x", srv.URL+"/roundtrip.git", cloneTarget)

	content, err := os.ReadFile(filepath.Join(cloneTarget, "README.md"))
	if err != nil {
		t.Fatalf("failed to read cloned file: %v", err)
	}
	if string(content) != "hello\n" {
		t.Errorf("cloned README.md = %q, want %q", content, "hello\n")
	}
}
