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
func backupOne(src, destDir, name string) error {
	final := filepath.Join(destDir, name+".git")
	tmp := filepath.Join(destDir, name+".git.tmp")

	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("backup: clearing stale temp dir for %q: %w", name, err)
	}
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("backup: copying %q: %w", name, err)
	}
	if err := os.RemoveAll(final); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("backup: removing previous backup of %q: %w", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("backup: finalizing %q: %w", name, err)
	}
	return nil
}

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
		data, err := os.ReadFile(srcPath)
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
