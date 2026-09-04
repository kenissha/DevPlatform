package gitstats

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// seedBranchedRepo creates a bare repo named "sample" with two commits on
// "main", then a "feature" branch forked from there with two more commits
// of its own — the shape CommitsAhead is meant to be exercised against:
// a branch with real history main doesn't have yet.
func seedBranchedRepo(t *testing.T) *repostore.Store {
	t.Helper()
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("sample")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main", "-q")
	runGit(t, work, "config", "user.email", "dev@example.com")
	runGit(t, work, "config", "user.name", "Dev")
	runGit(t, work, "remote", "add", "origin", repoPath)

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
		runGit(t, work, "add", name)
	}

	write("a.txt", "one\n")
	runGit(t, work, "commit", "-q", "-m", "on main 1")
	write("b.txt", "two\n")
	runGit(t, work, "commit", "-q", "-m", "on main 2")
	runGit(t, work, "push", "-q", "origin", "main")

	runGit(t, work, "checkout", "-q", "-b", "feature")
	write("c.txt", "three\n")
	runGit(t, work, "commit", "-q", "-m", "feature commit 1")
	write("d.txt", "four\n")
	runGit(t, work, "commit", "-q", "-m", "feature commit 2")
	runGit(t, work, "push", "-q", "origin", "feature")

	return repos
}

func TestCommitsAhead_ReturnsOnlyCommitsNotOnBase(t *testing.T) {
	repos := seedBranchedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := CommitsAhead(repo, "feature", "main", 10)
	if err != nil {
		t.Fatalf("CommitsAhead returned error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2 (main's own commits must be excluded)", len(commits))
	}
	if commits[0].Message != "feature commit 2\n" {
		t.Errorf("newest commit message = %q, want %q", commits[0].Message, "feature commit 2\n")
	}
	if commits[1].Message != "feature commit 1\n" {
		t.Errorf("oldest-of-the-two commit message = %q, want %q", commits[1].Message, "feature commit 1\n")
	}
}

func TestCommitsAhead_RespectsLimit(t *testing.T) {
	repos := seedBranchedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := CommitsAhead(repo, "feature", "main", 1)
	if err != nil {
		t.Fatalf("CommitsAhead returned error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	if commits[0].Message != "feature commit 2\n" {
		t.Errorf("message = %q, want the newest commit", commits[0].Message)
	}
}

func TestCommitsAhead_ReturnsEmptyWhenBranchEqualsBase(t *testing.T) {
	repos := seedBranchedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := CommitsAhead(repo, "main", "main", 10)
	if err != nil {
		t.Fatalf("CommitsAhead returned error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("got %d commits, want 0 (a branch has nothing ahead of itself)", len(commits))
	}
}

// TestCommitsAhead_BaseDoesNotExistYetReturnsEverything covers a brand
// new repo whose "main" has no commits at all (see
// backend/internal/mergerequest's Diff for the same "target doesn't
// exist yet" case) — every commit on branch counts as "ahead".
func TestCommitsAhead_BaseDoesNotExistYetReturnsEverything(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("empty-base")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature", "-q")
	runGit(t, work, "config", "user.email", "dev@example.com")
	runGit(t, work, "config", "user.name", "Dev")
	runGit(t, work, "remote", "add", "origin", repoPath)
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("failed to write a.txt: %v", err)
	}
	runGit(t, work, "add", "a.txt")
	runGit(t, work, "commit", "-q", "-m", "only commit")
	runGit(t, work, "push", "-q", "origin", "feature")

	repo, err := repos.Open("empty-base")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := CommitsAhead(repo, "feature", "main", 10)
	if err != nil {
		t.Fatalf("CommitsAhead returned error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1 (main doesn't exist yet, so everything on feature is ahead)", len(commits))
	}
}

func TestCommitsAhead_UnknownBranchReturnsErrBranchNotFound(t *testing.T) {
	repos := seedBranchedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = CommitsAhead(repo, "does-not-exist", "main", 10)
	if err != ErrBranchNotFound {
		t.Fatalf("err = %v, want ErrBranchNotFound", err)
	}
}
