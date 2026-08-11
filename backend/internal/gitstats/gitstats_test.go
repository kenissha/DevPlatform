package gitstats

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// seedRepo creates a bare repo named "sample" with three commits pushed to
// a branch: two by Dev One and one by Dev Two.
func seedRepo(t *testing.T) *repostore.Store {
	t.Helper()
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("sample")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature", "-q")
	runGit(t, work, "config", "user.email", "one@example.com")
	runGit(t, work, "config", "user.name", "Dev One")
	runGit(t, work, "remote", "add", "origin", repoPath)

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
		runGit(t, work, "add", name)
	}

	write("a.txt", "one\n")
	runGit(t, work, "commit", "-q", "-m", "first commit")
	write("b.txt", "two\n")
	runGit(t, work, "commit", "-q", "-m", "second commit")

	runGit(t, work, "config", "user.email", "two@example.com")
	runGit(t, work, "config", "user.name", "Dev Two")
	write("c.txt", "three\n")
	runGit(t, work, "commit", "-q", "-m", "third commit")

	runGit(t, work, "push", "-q", "origin", "feature")
	return repos
}

func TestCommits_ReturnsNewestFirst(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := Commits(repo, 10)
	if err != nil {
		t.Fatalf("Commits returned error: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(commits))
	}
	if got := commits[0].Message; got != "third commit\n" {
		t.Errorf("newest commit message = %q, want %q", got, "third commit\n")
	}
	if commits[0].AuthorEmail != "two@example.com" {
		t.Errorf("newest commit author = %q, want two@example.com", commits[0].AuthorEmail)
	}
	if len(commits[0].ShortHash) != 8 {
		t.Errorf("ShortHash = %q, want 8 characters", commits[0].ShortHash)
	}
}

func TestCommits_RespectsLimit(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	commits, err := Commits(repo, 2)
	if err != nil {
		t.Fatalf("Commits returned error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
}

func TestCommits_EmptyRepoIsNotAnError(t *testing.T) {
	requireGit(t)

	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("empty"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	repo, err := repos.Open("empty")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// A repo created through the platform has no refs until its first
	// merge request is approved — that must read as "no history", not as
	// a failure, or every new repo's insight page would show an error.
	commits, err := Commits(repo, 10)
	if err != nil {
		t.Fatalf("Commits on an empty repo returned error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("got %d commits, want 0", len(commits))
	}
}

func TestContributors_AggregatesByEmail(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	contributors, err := Contributors(repo)
	if err != nil {
		t.Fatalf("Contributors returned error: %v", err)
	}
	if len(contributors) != 2 {
		t.Fatalf("got %d contributors, want 2: %+v", len(contributors), contributors)
	}
	// Most commits first: Dev One has two, Dev Two has one.
	if contributors[0].Email != "one@example.com" || contributors[0].Commits != 2 {
		t.Errorf("top contributor = %+v, want one@example.com with 2 commits", contributors[0])
	}
	if contributors[1].Email != "two@example.com" || contributors[1].Commits != 1 {
		t.Errorf("second contributor = %+v, want two@example.com with 1 commit", contributors[1])
	}
}

func TestActivity_CountsTodayAndFillsGaps(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	activity, err := Activity(repo, 7)
	if err != nil {
		t.Fatalf("Activity returned error: %v", err)
	}
	if len(activity) != 7 {
		t.Fatalf("got %d days, want 7 (gaps must be filled, not omitted)", len(activity))
	}

	today := time.Now().UTC().Format("2006-01-02")
	if activity[len(activity)-1].Date != today {
		t.Errorf("last day = %q, want today %q", activity[len(activity)-1].Date, today)
	}

	total := 0
	for _, d := range activity {
		total += d.Commits
	}
	if total != 3 {
		t.Errorf("total commits over window = %d, want 3", total)
	}
}
