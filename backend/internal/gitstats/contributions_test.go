package gitstats

import (
	"testing"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// seedRepo (gitstats_test.go) puts two commits on one@example.com and one
// on two@example.com, all authored today.

func TestActivityByAuthors_CountsOnlyThatAuthorsCommits(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, []string{"one@example.com"}, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthor returned error: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	if counts[today] != 2 {
		t.Errorf("counts[%s] = %d, want 2 (the other author's commit must not be counted)", today, counts[today])
	}

	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

// Git author emails are compared case-insensitively: the same person
// committing as "One@Example.com" from a second machine is still them,
// and an address's domain (and, in practice, its mailbox) is not
// case-sensitive.
func TestActivityByAuthors_MatchesTheEmailCaseInsensitively(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, []string{"ONE@EXAMPLE.COM"}, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthor returned error: %v", err)
	}

	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 for a differently-cased spelling of the same address", total)
	}
}

// The whole reason this takes a set: one person's commits are split
// across the address their platform account uses and whatever their git
// config was set to on each machine. Passing both must count both.
func TestActivityByAuthors_CountsEveryAddressInTheSet(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, []string{"one@example.com", "two@example.com"}, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthors returned error: %v", err)
	}

	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (two commits from one author plus one from the other)", total)
	}
}

func TestActivityByAuthors_EmptySetCountsNothing(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, nil, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthors returned error: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty — no address means no match, not every match", counts)
	}
}

func TestActivityByAuthors_UnknownEmailIsEmptyNotAnError(t *testing.T) {
	repos := seedRepo(t)
	repo, err := repos.Open("sample")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, []string{"nobody@example.com"}, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthor returned error: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty for an address with no commits", counts)
	}
}

// A repository with no commits at all is the normal state of a
// just-created repo, so it must read as "no contributions" rather than
// failing the whole dashboard.
func TestActivityByAuthors_EmptyRepoIsNotAnError(t *testing.T) {
	requireGit(t)

	repos := repostore.New(t.TempDir())
	if _, err := repos.Create("empty"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	repo, err := repos.Open("empty")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	counts, err := ActivityByAuthors(repo, []string{"one@example.com"}, 30)
	if err != nil {
		t.Fatalf("ActivityByAuthor returned error: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty", counts)
	}
}

func TestFillDays_ReturnsOneEntryPerDayOldestFirst(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	days := fillDays(map[string]int{today: 4}, 7)

	if len(days) != 7 {
		t.Fatalf("got %d days, want 7", len(days))
	}
	if days[len(days)-1].Date != today {
		t.Errorf("last entry = %s, want today (%s) — the range must end today", days[len(days)-1].Date, today)
	}
	if days[len(days)-1].Commits != 4 {
		t.Errorf("today's count = %d, want 4", days[len(days)-1].Commits)
	}
	// Every other day must still be present, at zero — a heatmap needs a
	// continuous axis, not just the days that had activity.
	for _, d := range days[:len(days)-1] {
		if d.Commits != 0 {
			t.Errorf("day %s = %d, want 0", d.Date, d.Commits)
		}
	}
}
