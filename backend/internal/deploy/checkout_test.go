package deploy

import (
	"errors"
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

func TestCheckout_MaterializesNestedFilesToDisk(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(work, "assets", "img"), 0o755); err != nil {
		t.Fatalf("failed to make nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "assets", "img", "logo.png"), []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "push", "origin", "main")

	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	destDir := t.TempDir()
	hash, err := Checkout(repo, "main", destDir)
	if err != nil {
		t.Fatalf("Checkout returned error: %v", err)
	}
	if hash.IsZero() {
		t.Error("expected a non-zero commit hash")
	}

	readme, err := os.ReadFile(filepath.Join(destDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read checked-out README.md: %v", err)
	}
	if string(readme) != "hello\n" {
		t.Errorf("README.md content = %q, want %q", readme, "hello\n")
	}

	nested, err := os.ReadFile(filepath.Join(destDir, "assets", "img", "logo.png"))
	if err != nil {
		t.Fatalf("failed to read checked-out nested file: %v", err)
	}
	if len(nested) != 4 {
		t.Errorf("nested file length = %d, want 4 (binary content must round-trip byte-for-byte)", len(nested))
	}
}

func TestCheckout_ReturnsErrBranchNotFoundForMissingBranch(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "push", "origin", "main")

	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = Checkout(repo, "does-not-exist", t.TempDir())
	if !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("err = %v, want ErrBranchNotFound", err)
	}
}

func TestCheckout_RejectsTreeEntryEscapingDestDir(t *testing.T) {
	// writeTreeFile's guard is exercised directly: crafting a real git tree
	// entry containing ".." requires low-level plumbing surgery the git CLI
	// itself refuses to produce, so the escape check is tested at the unit
	// level instead of via an end-to-end repo.
	if isWithin(`C:\dest`, `C:\dest\..\elsewhere`) {
		t.Error("isWithin(dest, dest/../elsewhere) = true, want false")
	}
	if !isWithin(`C:\dest`, filepath.Join(`C:\dest`, "assets", "img", "logo.png")) {
		t.Error("isWithin(dest, dest/assets/img/logo.png) = false, want true")
	}
}
