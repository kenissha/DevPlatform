package mergerequest

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

// ErrNotFastForward is returned by FastForwardMerge when targetBranch has
// commits of its own that sourceBranch doesn't contain — a plain ref
// update would silently discard them, so the merge is refused instead.
var ErrNotFastForward = errors.New("mergerequest: target branch has diverged; fast-forward merge not possible")

// FastForwardMerge moves targetBranch's ref to sourceBranch's tip commit,
// provided targetBranch's current tip is an ancestor of it (i.e. every
// commit on targetBranch is already reachable from sourceBranch). This is
// the only merge strategy go-git v6-alpha.5 has real support for (see
// Repository.Merge, which is itself restricted to FastForwardMerge) — a
// true three-way merge that fabricates a merge commit isn't implemented by
// the pinned go-git version, so a genuinely diverged target has to be
// merged by an operator via the git CLI instead of through this API for
// now.
//
// This writes directly to repo's storer, bypassing gitserver's
// protectingLoader entirely (that guard only wraps the git smart-HTTP
// transport, not go-git library calls against a repostore.Open'd
// repository) — that is intentional: protectedloader.go's own doc comment
// describes protected refs as needing "a review/merge flow in a later
// plan" instead of a direct push, and this is that flow. Callers must only
// invoke this after an Admin has approved the merge request (see
// Handlers.Approve) — FastForwardMerge itself performs no authorization
// check of its own.
func FastForwardMerge(repo *git.Repository, targetBranch, sourceBranch string) (plumbing.Hash, error) {
	targetCommit, err := resolveBranchTip(repo, targetBranch)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	sourceCommit, err := resolveBranchTip(repo, sourceBranch)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	if targetCommit.Hash == sourceCommit.Hash {
		return sourceCommit.Hash, nil
	}

	isAncestor, err := targetCommit.IsAncestor(sourceCommit)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("mergerequest: checking ancestry: %w", err)
	}
	if !isAncestor {
		return plumbing.ZeroHash, ErrNotFastForward
	}

	newRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName(targetBranch), sourceCommit.Hash)
	if err := repo.Storer.SetReference(newRef); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("mergerequest: updating %q: %w", targetBranch, err)
	}
	return sourceCommit.Hash, nil
}
