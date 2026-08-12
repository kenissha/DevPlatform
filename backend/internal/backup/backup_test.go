package backup

import (
	"context"
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
