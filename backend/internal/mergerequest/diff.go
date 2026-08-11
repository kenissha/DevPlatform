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
	UnifiedDiff string
	Stats       object.FileStats
}

// Diff computes the changes that merging sourceBranch into targetBranch
// would introduce, i.e. "what does source add on top of target" — the same
// direction a GitHub-style base...compare view shows.
func Diff(repo *git.Repository, targetBranch, sourceBranch string) (DiffResult, error) {
	targetCommit, err := resolveBranchTip(repo, targetBranch)
	if err != nil {
		return DiffResult{}, err
	}
	sourceCommit, err := resolveBranchTip(repo, sourceBranch)
	if err != nil {
		return DiffResult{}, err
	}

	patch, err := targetCommit.Patch(sourceCommit)
	if err != nil {
		return DiffResult{}, fmt.Errorf("mergerequest: computing diff: %w", err)
	}

	return DiffResult{
		UnifiedDiff: patch.String(),
		Stats:       patch.Stats(),
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
