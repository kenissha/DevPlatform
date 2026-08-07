package repostore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate_MakesABareRepo(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	path, err := store.Create("intranet-backend")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	want := filepath.Join(dir, "intranet-backend.git")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		t.Errorf("expected bare repo HEAD file to exist: %v", err)
	}
}

func TestCreate_RejectsInvalidName(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("../escape")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsNameWithSlash(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("sub/dir")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsDuplicateName(t *testing.T) {
	store := New(t.TempDir())

	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	_, err := store.Create("intranet-backend")
	if err != ErrAlreadyExists {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestList_ReturnsCreatedRepoNames(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.Create("intranet-backend")
	store.Create("intranet-frontend")

	names, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %v", len(names), names)
	}
}

func TestList_ReturnsEmptySliceWhenDirMissing(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "does-not-exist"))

	names, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("got %d names, want 0", len(names))
	}
}
