package deploy

import (
	"os"
	"testing"
	"time"
)

func TestNewRelease_CreatesAFreshEmptyDirectory(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	dir, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("NewRelease returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected release dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected release dir to be a directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read release dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected a fresh empty dir, got %d entries", len(entries))
	}
}

func TestNewRelease_SuccessiveCallsGetDifferentDirectories(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	first, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("first NewRelease failed: %v", err)
	}
	// Ensure a distinguishable timestamp even on a fast filesystem/clock.
	time.Sleep(2 * time.Second)
	second, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("second NewRelease failed: %v", err)
	}

	if first == second {
		t.Fatalf("expected distinct release directories, both were %q", first)
	}
}

func TestList_ReturnsReleasesNewestFirst(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	first, _ := vs.NewRelease("sample", "test")
	time.Sleep(2 * time.Second)
	second, _ := vs.NewRelease("sample", "test")

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2", len(releases))
	}
	if releases[0] != second || releases[1] != first {
		t.Errorf("releases = %v, want newest (%q) first then %q", releases, second, first)
	}
}

func TestList_ReturnsEmptySliceWhenNoReleasesYet(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("got %d releases, want 0", len(releases))
	}
}

func TestPrune_KeepsOnlyTheNewestN(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	var made []string
	for i := 0; i < 4; i++ {
		dir, err := vs.NewRelease("sample", "test")
		if err != nil {
			t.Fatalf("NewRelease #%d failed: %v", i, err)
		}
		made = append(made, dir)
		time.Sleep(2 * time.Second)
	}

	if err := vs.Prune("sample", "test", 2); err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases after pruning to 2, want 2", len(releases))
	}
	// The two newest (made[3], made[2]) must survive; the two oldest must be gone from disk.
	for _, old := range made[:2] {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Errorf("expected pruned release dir %q to be deleted from disk", old)
		}
	}
}

func TestNewRelease_RejectsInvalidRepo(t *testing.T) {
	vs := NewVersionStore(t.TempDir())
	if _, err := vs.NewRelease("../escape", "test"); err == nil {
		t.Fatal("expected an error for a path-traversal repo name")
	}
}
