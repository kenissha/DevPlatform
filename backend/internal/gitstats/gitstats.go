// Package gitstats derives read-only insight into a repository's history:
// recent commits, who has been contributing, and how much activity there
// has been per day. It is the data behind the platform's "kimin ne
// üzerinde çalıştığını görebilmek" goal from the design doc — visibility
// without handing anyone server access.
//
// Everything here reads through go-git against a repostore-opened
// repository; nothing shells out to the git binary, and nothing writes.
package gitstats

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Commit is one entry of a repository's history, flattened for JSON.
type Commit struct {
	Hash        string    `json:"hash"`
	ShortHash   string    `json:"shortHash"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"authorName"`
	AuthorEmail string    `json:"authorEmail"`
	When        time.Time `json:"when"`
}

// Contributor aggregates one author's commits across the repository.
type Contributor struct {
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Commits int       `json:"commits"`
	LastAt  time.Time `json:"lastAt"`
}

// DayCount is the number of commits authored on one calendar day (UTC),
// the shape a contribution-activity chart consumes.
type DayCount struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Commits int    `json:"commits"`
}

// walk iterates every commit reachable from any ref, newest first, calling
// fn until it returns false or the history is exhausted.
//
// A repository with no commits at all is not an error here: a repo created
// through the platform starts empty (its default branch doesn't exist as a
// ref until the first merge request is approved), and every caller wants
// "no history yet" rather than a failure.
func walk(repo *git.Repository, fn func(*object.Commit) bool) error {
	iter, err := repo.Log(&git.LogOptions{All: true, Order: git.LogOrderCommitterTime})
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil
		}
		return fmt.Errorf("gitstats: reading log: %w", err)
	}
	defer iter.Close()

	err = iter.ForEach(func(c *object.Commit) error {
		if !fn(c) {
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return fmt.Errorf("gitstats: walking log: %w", err)
	}
	return nil
}

// Commits returns up to limit of the most recent commits across all
// branches, newest first.
func Commits(repo *git.Repository, limit int) ([]Commit, error) {
	commits := []Commit{}
	err := walk(repo, func(c *object.Commit) bool {
		commits = append(commits, toCommit(c))
		return len(commits) < limit
	})
	if err != nil {
		return nil, err
	}
	return commits, nil
}

// Contributors aggregates the whole history by author email, most commits
// first. Email is the identity key rather than name: the same person
// committing as "Dev Two" and "dev2" from two machines should still be one
// contributor, and git's own `shortlog -se` groups the same way.
func Contributors(repo *git.Repository) ([]Contributor, error) {
	byEmail := map[string]*Contributor{}
	err := walk(repo, func(c *object.Commit) bool {
		key := c.Author.Email
		entry, ok := byEmail[key]
		if !ok {
			byEmail[key] = &Contributor{
				Name:    c.Author.Name,
				Email:   c.Author.Email,
				Commits: 1,
				LastAt:  c.Author.When,
			}
			return true
		}
		entry.Commits++
		if c.Author.When.After(entry.LastAt) {
			entry.LastAt = c.Author.When
			// Prefer the name attached to their most recent commit, so a
			// renamed author shows up under the name they use now.
			entry.Name = c.Author.Name
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	out := make([]Contributor, 0, len(byEmail))
	for _, c := range byEmail {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Commits != out[j].Commits {
			return out[i].Commits > out[j].Commits
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

// Activity returns a commit count for each of the last `days` calendar days
// (UTC), oldest first, including days with zero commits so a chart can plot
// a continuous axis without filling gaps itself.
func Activity(repo *git.Repository, days int) ([]DayCount, error) {
	counts, err := countByDay(repo, days, func(*object.Commit) bool { return true })
	if err != nil {
		return nil, err
	}
	return fillDays(counts, days), nil
}

// ActivityByAuthor is Activity narrowed to the commits email authored —
// the per-person contribution heatmap's data, aggregated across repos by
// the caller. Returns raw per-day counts (keyed YYYY-MM-DD, UTC) rather
// than a filled range, because a caller summing several repositories
// wants to merge the maps before the gaps are filled in once.
//
// Identity is the commit's author email, compared case-insensitively:
// that's the only identifier a git commit carries, and it's how
// Contributors already groups. A person whose local `git config
// user.email` differs from their platform account's email therefore
// won't see those commits here — the fix is to align the two, since
// nothing in the commit object can bridge them.
func ActivityByAuthor(repo *git.Repository, email string, days int) (map[string]int, error) {
	return countByDay(repo, days, func(c *object.Commit) bool {
		return strings.EqualFold(c.Author.Email, email)
	})
}

// countByDay tallies commits matching include, per calendar day (UTC),
// within the last `days` days.
func countByDay(repo *git.Repository, days int, include func(*object.Commit) bool) (map[string]int, error) {
	if days < 1 {
		days = 1
	}
	start := startOfRange(days)

	counts := map[string]int{}
	err := walk(repo, func(c *object.Commit) bool {
		day := c.Author.When.UTC().Truncate(24 * time.Hour)
		if day.Before(start) {
			// Commits arrive newest-first, but a repo can contain commits
			// whose author date is older than an ancestor's (rebases,
			// cherry-picks, skewed clocks), so keep walking rather than
			// stopping at the first out-of-window commit.
			return true
		}
		if include(c) {
			counts[day.Format("2006-01-02")]++
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}

// fillDays expands per-day counts into one entry for every day in the
// window, oldest first — including the days with nothing on them, so a
// chart or heatmap can plot a continuous axis without filling gaps
// itself.
func fillDays(counts map[string]int, days int) []DayCount {
	if days < 1 {
		days = 1
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := startOfRange(days)

	out := make([]DayCount, 0, days)
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out = append(out, DayCount{Date: key, Commits: counts[key]})
	}
	return out
}

func startOfRange(days int) time.Time {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return today.AddDate(0, 0, -(days - 1))
}

// ErrBranchNotFound is returned by CommitsAhead when branch itself
// doesn't exist — unlike base (see CommitsAhead's doc comment), there's
// no sensible fallback for a branch that isn't there at all.
var ErrBranchNotFound = errors.New("gitstats: branch not found")

// CommitsAhead returns up to limit of branch's commits that base's
// history doesn't already contain, newest first — "what does this
// branch add on top of base", the same direction a GitHub branch
// comparison view shows, and the backing data for the branch detail
// page's commit list (see backend/internal/mergerequest.Diff for the
// same comparison at the file-content level instead of per-commit).
//
// If base doesn't exist yet (a brand new repo's protected default
// branch has no commits until an Admin's first direct push — see
// backend/internal/gitserver.WithAdmin/IsAdmin), every commit on branch
// counts as ahead, mirroring Diff's same-situation behavior.
func CommitsAhead(repo *git.Repository, branch, base string, limit int) ([]Commit, error) {
	branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, ErrBranchNotFound
		}
		return nil, fmt.Errorf("gitstats: resolving %q: %w", branch, err)
	}

	var baseCommit *object.Commit
	baseRef, err := repo.Reference(plumbing.NewBranchReferenceName(base), true)
	if err != nil {
		if !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, fmt.Errorf("gitstats: resolving %q: %w", base, err)
		}
		// base not existing yet is not an error — see doc comment.
	} else {
		baseCommit, err = repo.CommitObject(baseRef.Hash())
		if err != nil {
			return nil, fmt.Errorf("gitstats: reading %q's commit: %w", base, err)
		}
	}

	iter, err := repo.Log(&git.LogOptions{From: branchRef.Hash(), Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, fmt.Errorf("gitstats: reading log: %w", err)
	}
	defer iter.Close()

	commits := []Commit{}
	err = iter.ForEach(func(c *object.Commit) error {
		if baseCommit != nil {
			if c.Hash == baseCommit.Hash {
				return storer.ErrStop
			}
			isAncestorOfBase, aErr := c.IsAncestor(baseCommit)
			if aErr != nil {
				return aErr
			}
			if isAncestorOfBase {
				return storer.ErrStop
			}
		}
		commits = append(commits, toCommit(c))
		if len(commits) >= limit {
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, fmt.Errorf("gitstats: walking log: %w", err)
	}
	return commits, nil
}

func toCommit(c *object.Commit) Commit {
	hash := c.Hash.String()
	short := hash
	if len(short) > 8 {
		short = short[:8]
	}
	return Commit{
		Hash:        hash,
		ShortHash:   short,
		Message:     c.Message,
		AuthorName:  c.Author.Name,
		AuthorEmail: c.Author.Email,
		When:        c.Author.When,
	}
}
