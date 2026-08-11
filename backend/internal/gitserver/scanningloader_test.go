package gitserver

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func TestPush_ContainingSecret_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("secrets"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-with-secret")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	secretContent := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"
	if err := os.WriteFile(filepath.Join(work, "config.txt"), []byte(secretContent), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "config.txt")
	runGit(t, work, "commit", "-m", "accidentally commit a secret")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/secrets.git")

	cmd := exec.Command("git", "push", "origin", "feature-with-secret")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected push containing a secret to be rejected, but it succeeded. Output:\n%s", out)
	}
	t.Logf("push containing a secret correctly failed with:\n%s", out)

	// The real assertion: the branch must not exist on the server at all
	// afterward — not just that the client's push command failed.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "secrets")
	cmd = exec.Command("git", "clone", srv.URL+"/secrets.git", cloneTarget)
	cmd.Dir = verifyDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to clone for verification: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/feature-with-secret")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/feature-with-secret exists on the server, but the push containing a secret should have been rejected entirely")
	}
}

func TestPush_WithoutSecret_StillSucceeds(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("clean"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-clean")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("just some ordinary prose, nothing secret here\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "ordinary commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/clean.git")
	runGit(t, work, "push", "origin", "feature-clean")
	// runGit already fails the test via t.Fatalf if this push is rejected.
}
