package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRename_ReplacesTheTarget(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "new.tmp")
	to := filepath.Join(dir, "target.json")

	if err := os.WriteFile(to, []byte("eski"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(from, []byte("yeni"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := Rename(from, to); err != nil {
		t.Fatalf("Rename returned error: %v", err)
	}

	got, err := os.ReadFile(to)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "yeni" {
		t.Errorf("target content = %q, want %q", got, "yeni")
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Errorf("source still exists after rename: %v", err)
	}
}

// A genuine failure must still be reported. The retry loop exists to
// absorb a transient lock, not to turn a broken write into a silent
// success — a store that believed a failed write had landed would report
// data as saved that isn't.
func TestRename_ReportsAFailureAfterRetrying(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "yok", "olmayan.tmp")

	if err := Rename(missing, filepath.Join(dir, "target.json")); err == nil {
		t.Fatal("Rename returned nil for a source that does not exist")
	}
}
