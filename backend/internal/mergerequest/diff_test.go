package mergerequest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDiff_ReportsAddedLines(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repoStore := repostore.New(dataDir)
	repoPath, err := repoStore.Create("sample")
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

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	result, err := Diff(repo, "main", "feature-x")
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}

	if !strings.Contains(result.UnifiedDiff, "+line two") {
		t.Errorf("unified diff missing added line, got:\n%s", result.UnifiedDiff)
	}
	if len(result.Stats) != 1 || result.Stats[0].Name != "README.md" {
		t.Fatalf("Stats = %+v, want a single README.md entry", result.Stats)
	}
	if result.Stats[0].Addition != 1 {
		t.Errorf("Addition = %d, want 1", result.Stats[0].Addition)
	}
}

func TestDiff_ReturnsErrBranchNotFoundForMissingBranch(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repoStore := repostore.New(dataDir)
	repoPath, err := repoStore.Create("sample")
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

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = Diff(repo, "main", "does-not-exist")
	if !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("err = %v, want ErrBranchNotFound", err)
	}
}

func TestDiff_ShowsEverythingAddedWhenTargetDoesNotExistYet(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repoStore := repostore.New(dataDir)
	repoPath, err := repoStore.Create("sample")
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

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	result, err := Diff(repo, "main", "feature-x")
	if err != nil {
		t.Fatalf("Diff returned error for nonexistent target branch: %v", err)
	}
	if !strings.Contains(result.UnifiedDiff, "+line one") {
		t.Errorf("unified diff missing added line, got:\n%s", result.UnifiedDiff)
	}
	if len(result.Stats) != 1 || result.Stats[0].Name != "README.md" || result.Stats[0].Addition != 1 {
		t.Errorf("Stats = %+v, want a single README.md entry with 1 addition", result.Stats)
	}
}
