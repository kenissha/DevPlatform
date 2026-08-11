package mergerequest

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// ErrBranchNotFound is returned by Diff when either branch does not exist
// in the repository.
var ErrBranchNotFound = errors.New("mergerequest: branch not found")

// DiffResult is the reviewable content of a merge request: the unified
// diff text (what a Yönetici reads to decide approve/reject, matching the
// design doc's "kör onay yoktur" requirement) plus a per-file summary.
type DiffResult struct {
	UnifiedDiff string     `json:"unifiedDiff"`
	Stats       []FileStat `json:"stats"`
}

// FileStat mirrors go-git's object.FileStat with JSON tags — go-git's own
// type has none, which would otherwise leak PascalCase field names into an
// API whose every other field is camelCase.
type FileStat struct {
	Name     string `json:"name"`
	Addition int    `json:"addition"`
	Deletion int    `json:"deletion"`
}

// Diff computes the changes that merging sourceBranch into targetBranch
// would introduce, i.e. "what does source add on top of target" — the same
// direction a GitHub-style base...compare view shows. If targetBranch
// doesn't exist yet (see FastForwardMerge's doc comment — this is how a
// repo's first commit reaches its default branch), the diff is computed
// against an empty tree, so the review screen shows every file in
// sourceBranch as newly added rather than erroring out.
func Diff(repo *git.Repository, targetBranch, sourceBranch string) (DiffResult, error) {
	sourceCommit, err := resolveBranchTip(repo, sourceBranch)
	if err != nil {
		return DiffResult{}, err
	}

	var targetTree *object.Tree
	targetCommit, err := resolveBranchTip(repo, targetBranch)
	switch {
	case err == nil:
		targetTree, err = targetCommit.Tree()
		if err != nil {
			return DiffResult{}, fmt.Errorf("mergerequest: resolving tree of %q: %w", targetBranch, err)
		}
	case errors.Is(err, ErrBranchNotFound):
		targetTree = nil
	default:
		return DiffResult{}, err
	}

	sourceTree, err := sourceCommit.Tree()
	if err != nil {
		return DiffResult{}, fmt.Errorf("mergerequest: resolving tree of %q: %w", sourceBranch, err)
	}

	patch, err := targetTree.Patch(sourceTree)
	if err != nil {
		return DiffResult{}, fmt.Errorf("mergerequest: computing diff: %w", err)
	}

	goGitStats := patch.Stats()
	stats := make([]FileStat, len(goGitStats))
	for i, s := range goGitStats {
		stats[i] = FileStat{Name: s.Name, Addition: s.Addition, Deletion: s.Deletion}
	}

	return DiffResult{
		UnifiedDiff: patch.String(),
		Stats:       stats,
	}, nil
}

func resolveBranchTip(repo *git.Repository, branch string) (*object.Commit, error) {
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrBranchNotFound, branch)
		}
		return nil, err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("mergerequest: resolving tip of %q: %w", branch, err)
	}
	return commit, nil
}
