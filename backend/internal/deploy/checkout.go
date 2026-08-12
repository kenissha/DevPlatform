package deploy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// ErrBranchNotFound is returned by Checkout when branch does not exist in
// the repository.
var ErrBranchNotFound = errors.New("deploy: branch not found")

// Checkout writes every file in branch's tip commit into destDir (created
// if it doesn't exist yet) and returns the commit that was checked out —
// the source materialization step Pipeline.Deploy needs before it can run
// a build recipe against a real directory.
//
// This does not go through go-git's own Clone/Worktree machinery. repo is
// always a bare repository here (repostore never creates a working tree),
// and go-git's clone path resolves a local source through
// transport.NewEndpoint, which parses the given string as a URL — a
// Windows path like `C:\data\foo.git` is ambiguous with a URL scheme ("C"
// read as the scheme), a known class of bug on this OS. Walking the
// resolved commit's tree directly and writing each blob to disk sidesteps
// URL parsing of a filesystem path entirely.
func Checkout(repo *git.Repository, branch, destDir string) (plumbing.Hash, error) {
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return plumbing.ZeroHash, fmt.Errorf("%w: %q", ErrBranchNotFound, branch)
		}
		return plumbing.ZeroHash, err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("deploy: resolving tip of %q: %w", branch, err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("deploy: resolving tree of %q: %w", branch, err)
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return plumbing.ZeroHash, err
	}

	files := tree.Files()
	defer files.Close()
	err = files.ForEach(func(f *object.File) error {
		return writeTreeFile(destDir, f)
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("deploy: checking out %q: %w", branch, err)
	}

	return commit.Hash, nil
}

func writeTreeFile(destDir string, f *object.File) error {
	switch f.Mode {
	case filemode.Regular, filemode.Executable:
		// handled below
	default:
		// Symlinks, submodules (gitlinks), and anything else this
		// codebase's own repositories have never contained: recreating a
		// symlink from tree content needs privileges this process doesn't
		// assume it has on Windows, and a submodule points at content
		// outside this tree entirely. Skipped rather than erroring, so an
		// otherwise-deployable repo that happens to contain one elsewhere
		// isn't blocked by it.
		return nil
	}

	// f.Name uses "/" as git's tree separator regardless of OS. Clean +
	// prefix-check guards against a maliciously crafted tree entry (e.g.
	// containing "../") ever writing outside destDir — the same discipline
	// repostore and taskboard already apply to any attacker-influenced
	// string before it's joined into a filesystem path.
	target := filepath.Join(destDir, filepath.FromSlash(f.Name))
	if !isWithin(destDir, target) {
		return fmt.Errorf("deploy: tree entry %q escapes checkout directory", f.Name)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}

	mode := os.FileMode(0o640)
	if f.Mode == filemode.Executable {
		mode = 0o750
	}

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	r, err := f.Blob.Reader()
	if err != nil {
		return err
	}
	defer r.Close()

	_, err = io.Copy(out, r)
	return err
}

func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
