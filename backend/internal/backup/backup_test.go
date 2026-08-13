package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func TestRun_CopiesAllRepositoriesToDestDir(t *testing.T) {
	srcRoot := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "backups")
	repos := repostore.New(srcRoot)

	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := repos.Create("intranet-frontend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := Run(repos, destDir)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	if len(result.ReposCopied) != 2 {
		t.Fatalf("ReposCopied = %v, want 2 entries", result.ReposCopied)
	}

	for _, name := range []string{"intranet-backend", "intranet-frontend"} {
		if _, err := os.Stat(filepath.Join(destDir, name+".git", "HEAD")); err != nil {
			t.Errorf("expected backed-up HEAD file for %q: %v", name, err)
		}
	}
}

func TestRun_CreatesDestDirIfMissing(t *testing.T) {
	srcRoot := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "nested", "backups")
	repos := repostore.New(srcRoot)

	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if _, err := os.Stat(destDir); err != nil {
		t.Errorf("expected destDir to be created: %v", err)
	}
}

func TestRun_ReturnsEmptyResultWhenNoRepositories(t *testing.T) {
	repos := repostore.New(t.TempDir())

	result, err := Run(repos, t.TempDir())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.ReposCopied) != 0 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want empty", result)
	}
}

func TestRun_ReplacesAPreviousBackupOnTheNextRun(t *testing.T) {
	srcRoot := t.TempDir()
	destDir := t.TempDir()
	repos := repostore.New(srcRoot)

	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}

	// Simulate the source repo changing between runs (e.g. new commits
	// pushed) by writing a marker file directly into the bare repo dir.
	marker := filepath.Join(srcRoot, "intranet-backend.git", "marker.txt")
	if err := os.WriteFile(marker, []byte("second run"), 0o640); err != nil {
		t.Fatalf("failed to write marker file: %v", err)
	}

	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "intranet-backend.git", "marker.txt"))
	if err != nil {
		t.Fatalf("expected marker.txt to be present in the refreshed backup: %v", err)
	}
	if string(got) != "second run" {
		t.Errorf("marker.txt = %q, want %q", got, "second run")
	}
}

func TestRun_MidCopyFailurePreservesThePreviousBackup(t *testing.T) {
	srcRoot := t.TempDir()
	destDir := t.TempDir()
	repos := repostore.New(srcRoot)

	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	firstHEAD, err := os.ReadFile(filepath.Join(destDir, "intranet-backend.git", "HEAD"))
	if err != nil {
		t.Fatalf("reading first backup's HEAD: %v", err)
	}

	// Force the second run's copy to fail partway through — after some
	// files have already been copied into the tmp dir but before the copy
	// as a whole completes — simulating a crash or I/O error mid-backup.
	failOn := filepath.Join(srcRoot, "intranet-backend.git", "config")
	original := readFile
	readFile = func(path string) ([]byte, error) {
		if path == failOn {
			return nil, fmt.Errorf("simulated read failure for %q", path)
		}
		return original(path)
	}
	defer func() { readFile = original }()

	result, err := Run(repos, destDir)
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if _, failed := result.Errors["intranet-backend"]; !failed {
		t.Fatalf("expected intranet-backend to be reported as failed, result = %+v", result)
	}

	// The previous good backup must be untouched: same content, still at
	// the path restores would read from.
	gotHEAD, err := os.ReadFile(filepath.Join(destDir, "intranet-backend.git", "HEAD"))
	if err != nil {
		t.Fatalf("expected previous backup's HEAD to still be present: %v", err)
	}
	if string(gotHEAD) != string(firstHEAD) {
		t.Errorf("HEAD = %q, want unchanged %q", gotHEAD, firstHEAD)
	}

	// No leftover tmp or old directory should remain after a failed run.
	if _, err := os.Stat(filepath.Join(destDir, "intranet-backend.git.tmp")); !os.IsNotExist(err) {
		t.Errorf("expected tmp dir to be cleaned up after failure, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "intranet-backend.git.old")); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .old dir after a failed copy, stat err = %v", err)
	}
}

func TestRun_RecoversWhenPreviousRunCrashedBetweenTheTwoFinalizeRenames(t *testing.T) {
	srcRoot := t.TempDir()
	destDir := t.TempDir()
	repos := repostore.New(srcRoot)

	if _, err := repos.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}

	// Simulate a crash between the "final -> old" and "tmp -> final" renames
	// of a previous finalize: final is missing, old holds the last good
	// backup, and a (possibly incomplete) tmp is left behind.
	final := filepath.Join(destDir, "intranet-backend.git")
	old := filepath.Join(destDir, "intranet-backend.git.old")
	tmp := filepath.Join(destDir, "intranet-backend.git.tmp")
	if err := os.Rename(final, old); err != nil {
		t.Fatalf("simulating crash (final -> old) failed: %v", err)
	}
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		t.Fatalf("simulating leftover tmp failed: %v", err)
	}

	if _, err := Run(repos, destDir); err != nil {
		t.Fatalf("recovery Run returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(final, "HEAD")); err != nil {
		t.Errorf("expected final backup to be present after recovery: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected .old dir to be cleaned up after recovery, stat err = %v", err)
	}
}

func TestPathsOverlap(t *testing.T) {
	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repos")
	backupDir := filepath.Join(tmp, "backups")
	nested := filepath.Join(repoRoot, "nested")
	prefixSibling := filepath.Join(tmp, "repos2")

	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical paths", repoRoot, repoRoot, true},
		{"a nested inside b", nested, repoRoot, true},
		{"b nested inside a", repoRoot, nested, true},
		{"unrelated sibling directories", repoRoot, backupDir, false},
		{"sibling sharing a name prefix is not a subdirectory", repoRoot, prefixSibling, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PathsOverlap(tc.a, tc.b)
			if err != nil {
				t.Fatalf("PathsOverlap(%q, %q) returned error: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Errorf("PathsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestNextRun_ReturnsLaterTodayWhenScheduledTimeHasNotPassedYet(t *testing.T) {
	now := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)

	got := NextRun(now, 2, 0)

	want := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextRun = %v, want %v", got, want)
	}
}

func TestNextRun_ReturnsTomorrowWhenScheduledTimeHasAlreadyPassed(t *testing.T) {
	now := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)

	got := NextRun(now, 2, 0)

	want := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextRun = %v, want %v", got, want)
	}
}

func TestNextRun_ReturnsTomorrowWhenNowIsExactlyTheScheduledTime(t *testing.T) {
	now := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)

	got := NextRun(now, 2, 0)

	want := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextRun = %v, want %v", got, want)
	}
}

func TestRunNightly_StopsWhenContextIsCancelled(t *testing.T) {
	repos := repostore.New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		// A scheduled hour far in the future means RunNightly is parked on
		// time.After when cancellation arrives — this proves ctx.Done()
		// actually unblocks it instead of the goroutine leaking forever.
		RunNightly(ctx, repos, t.TempDir(), 23, 59)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunNightly did not return after context cancellation")
	}
}
