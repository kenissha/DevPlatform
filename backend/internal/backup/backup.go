// Package backup copies every bare git repository this platform hosts to a
// separate destination directory — the design doc's "gecelik yedek"
// requirement: real GitHub sync only happens occasionally, so between syncs
// the internal repo store is the only copy of a team's work, and this
// exists to make sure a server-disk failure doesn't mean losing it.
package backup

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Result reports the outcome of one Run: which repositories were copied
// successfully, and which failed (keyed by repo name). A failure copying
// one repository does not stop the rest from being attempted — one bad
// repo shouldn't turn a night's backup of everything else into nothing.
type Result struct {
	ReposCopied []string
	Errors      map[string]error
}

// Run copies every repository in repos into destDir, one <name>.git
// subdirectory at a time. destDir is created if it doesn't exist yet.
func Run(repos *repostore.Store, destDir string) (Result, error) {
	names, err := repos.List()
	if err != nil {
		return Result{}, fmt.Errorf("backup: listing repositories: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return Result{}, fmt.Errorf("backup: creating destination %q: %w", destDir, err)
	}

	result := Result{Errors: map[string]error{}}
	for _, name := range names {
		src := filepath.Join(repos.RootDir(), name+".git")
		if err := backupOne(src, destDir, name); err != nil {
			result.Errors[name] = err
			continue
		}
		result.ReposCopied = append(result.ReposCopied, name)
	}
	return result, nil
}

// backupOne copies src (a single bare repo) into destDir/<name>.git via a
// temp-directory-then-rename sequence. If the copy fails partway through,
// the previous night's good backup (if any) is left untouched instead of
// being replaced by a truncated one that would look complete but isn't.
//
// Finalizing is a three-step rename swap (final -> old, tmp -> final, then
// remove old) rather than a remove-then-rename. That way final — the path
// restores actually read from — is never in a "gone but not yet replaced"
// state: at every point during finalize, either the previous backup is
// still reachable (as final or old) or the new one already is. A crash
// between any two of those steps is recovered from at the start of the
// next run instead of silently discarding whichever copy survived.
func backupOne(src, destDir, name string) error {
	final := filepath.Join(destDir, name+".git")
	tmp := filepath.Join(destDir, name+".git.tmp")
	old := filepath.Join(destDir, name+".git.old")

	// Recover from a crash that happened after a previous run renamed
	// final -> old but before it renamed tmp -> final: final is missing
	// and old holds the last known-good backup. Restore it before doing
	// anything else so final is never left absent while a good copy sits
	// on disk under another name — including if this run's own copy then
	// fails too.
	if _, err := os.Stat(final); os.IsNotExist(err) {
		if _, oldErr := os.Stat(old); oldErr == nil {
			if err := os.Rename(old, final); err != nil {
				return fmt.Errorf("backup: recovering previous backup of %q: %w", name, err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("backup: checking previous backup of %q: %w", name, err)
	}

	// A leftover tmp is only ever an incomplete copy from an earlier run
	// (this run recreates it from scratch below), never a finalized
	// backup, so discarding it here is always safe.
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("backup: clearing stale temp dir for %q: %w", name, err)
	}
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("backup: copying %q: %w", name, err)
	}

	if _, err := os.Stat(final); err == nil {
		if err := os.RemoveAll(old); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("backup: clearing stale previous-backup dir for %q: %w", name, err)
		}
		if err := os.Rename(final, old); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("backup: preserving previous backup of %q: %w", name, err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("backup: checking previous backup of %q: %w", name, err)
	}

	if err := os.Rename(tmp, final); err != nil {
		// final is now missing (or never existed) and the new copy failed
		// to take its place — restore the previous backup from old, if any,
		// rather than leaving nothing at final at all.
		if _, statErr := os.Stat(final); os.IsNotExist(statErr) {
			_ = os.Rename(old, final)
		}
		return fmt.Errorf("backup: finalizing %q: %w", name, err)
	}
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("backup: removing superseded backup of %q: %w", name, err)
	}
	return nil
}

// PathsOverlap reports whether a and b resolve to the same directory, or
// whether either is a filesystem subdirectory of the other. backupOne
// operates on its destination with RemoveAll and Rename; if that
// destination coincided with (or contained, or was contained in) the live
// repository store's root, those calls would run against real repositories
// instead of a backup copy. Callers should refuse to enable backup rather
// than start it when this returns true.
func PathsOverlap(a, b string) (bool, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, fmt.Errorf("backup: resolving %q: %w", a, err)
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false, fmt.Errorf("backup: resolving %q: %w", b, err)
	}
	absA = filepath.Clean(absA)
	absB = filepath.Clean(absB)

	if strings.EqualFold(absA, absB) {
		return true, nil
	}
	return isSubPath(absA, absB) || isSubPath(absB, absA), nil
}

// isSubPath reports whether child is a filesystem subdirectory of parent.
// Both must already be absolute and clean. A trailing separator is added to
// parent before the prefix comparison so that e.g. "/data/backup2" is not
// mistaken for being inside "/data/backup".
func isSubPath(parent, child string) bool {
	prefix := parent + string(filepath.Separator)
	if len(child) <= len(prefix) {
		return false
	}
	return strings.EqualFold(child[:len(prefix)], prefix)
}

// readFile is copyDir's file-read step, indirected through a variable so
// tests can force a failure partway through a multi-file copy (simulating a
// crash or I/O error mid-backup) without relying on OS-specific permission
// tricks that don't behave consistently across platforms. Production code
// always leaves this as os.ReadFile.
var readFile = os.ReadFile

// copyDir recursively copies the contents of src into dst, preserving the
// relative directory structure — the same nested-directory concern
// internal/deploy's own copyDir was written for (a bare repo's objects/
// directory is fanned out into two-character subdirectories).
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := readFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o640); err != nil {
			return err
		}
	}
	return nil
}

// NextRun returns the next time at or after now that a backup scheduled
// for hour:minute (in now's location) should run: later today if that time
// hasn't passed yet, tomorrow otherwise. now being exactly at hour:minute
// counts as "already passed" — the run for that moment is either already
// happening or was just missed, either way the next one to wait for is
// tomorrow's.
func NextRun(now time.Time, hour, minute int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunNightly blocks until ctx is cancelled, running Run against repos and
// destDir once every day at hour:minute server-local time. A failed run is
// logged, not fatal — the next scheduled run is still attempted the
// following day.
func RunNightly(ctx context.Context, repos *repostore.Store, destDir string, hour, minute int) {
	for {
		wait := time.Until(NextRun(time.Now(), hour, minute))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		result, err := Run(repos, destDir)
		if err != nil {
			log.Printf("backup: nightly run failed: %v", err)
			continue
		}
		log.Printf("backup: nightly run copied %d repositories to %q (%d failed)", len(result.ReposCopied), destDir, len(result.Errors))
		for name, err := range result.Errors {
			log.Printf("backup: %q failed: %v", name, err)
		}
	}
}
