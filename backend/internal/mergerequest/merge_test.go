package mergerequest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func TestFastForwardMerge_MovesTargetToSourceTip(t *testing.T) {
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
	featureTip := runGit(t, work, "rev-parse", "feature-x")

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	mergedHash, err := FastForwardMerge(repo, "main", "feature-x")
	if err != nil {
		t.Fatalf("FastForwardMerge returned error: %v", err)
	}
	if mergedHash.String()+"\n" != featureTip {
		t.Errorf("mergedHash = %s, want %s", mergedHash.String(), featureTip)
	}

	// Verify main actually moved on disk, not just in the return value.
	mainTip := runGit(t, repoPath, "rev-parse", "main")
	if mergedHash.String()+"\n" != mainTip {
		t.Errorf("main tip after merge = %s, want %s", mainTip, mergedHash.String()+"\n")
	}
}

func TestFastForwardMerge_RejectsDivergedBranches(t *testing.T) {
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

	// feature-x diverges from main: both get their own commit on top of
	// the shared initial commit, so neither is an ancestor of the other.
	runGit(t, work, "checkout", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature work\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "feature.txt")
	runGit(t, work, "commit", "-m", "feature work")
	runGit(t, work, "push", "origin", "feature-x")

	runGit(t, work, "checkout", "main")
	if err := os.WriteFile(filepath.Join(work, "unrelated.txt"), []byte("unrelated change\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "unrelated.txt")
	runGit(t, work, "commit", "-m", "unrelated change on main")
	runGit(t, work, "push", "origin", "main")
	originalMainTip := runGit(t, repoPath, "rev-parse", "main")

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = FastForwardMerge(repo, "main", "feature-x")
	if !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("err = %v, want ErrNotFastForward", err)
	}

	// main must be untouched after a rejected merge.
	mainTip := runGit(t, repoPath, "rev-parse", "main")
	if mainTip != originalMainTip {
		t.Errorf("main tip changed after rejected merge: got %s, want %s", mainTip, originalMainTip)
	}
}

func TestFastForwardMerge_NoOpWhenAlreadyUpToDate(t *testing.T) {
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
	runGit(t, work, "branch", "feature-x")
	runGit(t, work, "push", "origin", "feature-x")

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = FastForwardMerge(repo, "main", "feature-x")
	if err != nil {
		t.Fatalf("FastForwardMerge returned error for already-up-to-date branches: %v", err)
	}
}

func TestFastForwardMerge_CreatesTargetBranchWhenItDoesNotExistYet(t *testing.T) {
	requireGit(t)

	// A freshly created bare repo has zero commits, so its default branch
	// ("main") doesn't exist as a ref at all yet — and protectingLoader
	// rejects every direct push to it unconditionally. This is the only
	// path by which such a repo's first commit can reach "main".
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
	featureTip := runGit(t, work, "rev-parse", "feature-x")

	repo, err := repoStore.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	mergedHash, err := FastForwardMerge(repo, "main", "feature-x")
	if err != nil {
		t.Fatalf("FastForwardMerge returned error: %v", err)
	}
	if mergedHash.String()+"\n" != featureTip {
		t.Errorf("mergedHash = %s, want %s", mergedHash.String(), featureTip)
	}

	mainTip := runGit(t, repoPath, "rev-parse", "main")
	if mergedHash.String()+"\n" != mainTip {
		t.Errorf("main tip after merge = %s, want %s", mainTip, mergedHash.String()+"\n")
	}
}
