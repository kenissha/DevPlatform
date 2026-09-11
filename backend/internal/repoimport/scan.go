package repoimport

import (
	"errors"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/kenissha/DevPlatform/backend/internal/secretscan"
)

// An import writes straight into DataDir, so nothing it brings in passes
// the push-time secret scanner. That is the right call — the scanner is
// there to stop somebody committing a credential today, and an imported
// history was published elsewhere long before it arrived here — but it
// must not happen quietly. Every import scans what it brought and says so.
//
// The result is information, not a verdict: the import has already
// succeeded by the time this runs. Refusing an import over a secret in a
// five-year-old commit would only mean the repository lives somewhere
// else, which protects nobody.

// maxScannedBlobSize matches gitserver's own cap. Secrets are textual and
// small; buffering multi-megabyte binaries to search them costs memory for
// no realistic benefit.
const maxScannedBlobSize = 1 << 20

// maxFindings caps the reported list. A repository with hundreds of hits
// has a problem no list length will change, and the panel has to render
// this.
const maxFindings = 50

// Finding is one secret found in an imported history.
type Finding struct {
	// Pattern is the detector's name — never the matched text. Echoing the
	// secret back into the panel, the audit log and the browser's history
	// would spread it further than leaving it in the commit did.
	Pattern string `json:"pattern"`
	// Path is where the blob sat when it was written. A file deleted years
	// ago still reports the path it had.
	Path string `json:"path"`
}

// ScanHistory reports the secrets reachable anywhere in the repository at
// dir.
//
// Failures return no findings rather than an error: this runs after a
// successful import, and a repository that cannot be walked is not a
// reason to tell somebody their import failed when it did not. The
// alternative — reporting "temiz" — would be worse, so a walk that breaks
// partway keeps whatever it found.
func ScanHistory(dir string) []Finding {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil
	}
	blobs, err := repo.BlobObjects()
	if err != nil {
		return nil
	}
	defer blobs.Close()

	// Deduplicated by pattern+path: a file edited thirty times is one
	// problem, not thirty. Paths come from the trees, walked separately,
	// because a blob does not know its own name.
	names := blobPaths(repo)
	seen := map[Finding]bool{}
	var found []Finding

	_ = blobs.ForEach(func(b *object.Blob) error {
		if b.Size > maxScannedBlobSize {
			return nil
		}
		reader, err := b.Reader()
		if err != nil {
			return nil
		}
		content, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return nil
		}
		pattern, ok := secretscan.Scan(content)
		if !ok {
			return nil
		}
		path := names[b.Hash.String()]
		if path == "" {
			path = "(bilinmeyen dosya)"
		}
		f := Finding{Pattern: pattern, Path: path}
		if seen[f] {
			return nil
		}
		seen[f] = true
		found = append(found, f)
		if len(found) >= maxFindings {
			return errStopScan
		}
		return nil
	})

	sort.Slice(found, func(i, j int) bool {
		if found[i].Pattern != found[j].Pattern {
			return found[i].Pattern < found[j].Pattern
		}
		return found[i].Path < found[j].Path
	})
	return found
}

// errStopScan ends ForEach early once maxFindings is reached. ForEach
// treats any non-nil error as "stop", and this one is never surfaced.
var errStopScan = errors.New("repoimport: finding limit reached")

// blobPaths maps blob hashes to the path they were last seen at, by
// walking every commit's tree.
//
// A blob object carries no name — the name lives in the tree that points
// at it — so finding out what a flagged blob was called means walking the
// history. Worth it: "e2e/.auth/ik.json" tells somebody what to do,
// "blob 281cc72" tells them nothing.
func blobPaths(repo *git.Repository) map[string]string {
	paths := map[string]string{}

	commits, err := repo.CommitObjects()
	if err != nil {
		return paths
	}
	defer commits.Close()

	// Shared across commits so a tree reached from two commits — which is
	// most of them, since a commit reuses its parent's unchanged
	// subtrees — is walked once. Without this a 600-commit history walks
	// the same directories hundreds of times.
	seenTrees := map[plumbing.Hash]bool{}
	_ = commits.ForEach(func(c *object.Commit) error {
		tree, err := c.Tree()
		if err != nil {
			return nil
		}
		if seenTrees[tree.Hash] {
			return nil
		}

		walker := object.NewTreeWalker(tree, true, seenTrees)
		defer walker.Close()
		for {
			name, entry, err := walker.Next()
			if err != nil {
				return nil
			}
			if _, exists := paths[entry.Hash.String()]; !exists {
				paths[entry.Hash.String()] = name
			}
		}
	})
	return paths
}

// validateSource decides whether a URL may be handed to git clone.
//
// This is the security boundary of the whole package. The string reaches
// a command line, and git's remote helpers make that far more dangerous
// than it looks:
//
//   - "ext::sh -c whoami" is a documented git transport that executes a
//     shell command. Cloning it is remote code execution.
//   - a value starting with "-" is read by git as an option, not a URL;
//     "--upload-pack=..." runs an arbitrary program.
//   - "file:///" and bare local paths would let an admin pull any
//     directory on the server into a repository other people can read.
//
// So this allows exactly two schemes and refuses everything else, rather
// than trying to enumerate what is dangerous. Being admin-only is not a
// reason to skip it: an admin account is the one most worth stealing, and
// nothing here needs the extra reach.
func validateSource(raw string) error {
	if raw == "" {
		return ErrInvalidSource
	}
	if strings.HasPrefix(raw, "-") {
		return ErrInvalidSource
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidSource
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ErrInvalidSource
	}
	if u.Host == "" {
		return ErrInvalidSource
	}
	return nil
}
