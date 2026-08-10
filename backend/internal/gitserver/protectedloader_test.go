package gitserver

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

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
