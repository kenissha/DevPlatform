package repostore

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestCreate_RejectsEmptyName(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsNameWithBackslash(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("back\\slash")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsNameWithNullByte(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("null\x00byte")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsWindowsReservedNames(t *testing.T) {
	store := New(t.TempDir())

	for _, name := range []string{
		"CON", "con", "Con", "PRN", "AUX", "NUL",
		"COM1", "COM9", "com5", "LPT1", "LPT9", "lpt3",
	} {
		_, err := store.Create(name)
		if err != ErrInvalidName {
			t.Errorf("Create(%q) err = %v, want ErrInvalidName", name, err)
		}
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

// TestCreate_CleansUpDirectoryWhenPlainInitFails forces git.PlainInit to
// fail after os.Mkdir has already created the target directory, by denying
// "create files / write data" rights (inherited) on the store root. NTFS
// treats "create folders" (needed by os.Mkdir) and "create files" (needed
// by PlainInit's internal writes) as distinct rights, so os.Mkdir still
// succeeds while PlainInit's writes inside the new directory fail — exactly
// the scenario the fix in Create addresses. It then verifies the orphaned
// directory was removed and the name can be reused.
func TestCreate_CleansUpDirectoryWhenPlainInitFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ACL-based PlainInit-failure simulation is Windows-specific")
	}

	dir := t.TempDir()
	store := New(dir)

	principal := os.Getenv("USERDOMAIN") + "\\" + os.Getenv("USERNAME")

	denyCmd := exec.Command("icacls", dir, "/deny", principal+":(OI)(CI)(WD)")
	if out, err := denyCmd.CombinedOutput(); err != nil {
		t.Fatalf("icacls deny setup failed: %v: %s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("icacls", dir, "/remove:d", principal).Run() //nolint:errcheck // best-effort cleanup
	})

	path, err := store.Create("intranet-backend")
	if err == nil {
		t.Fatalf("expected Create to fail when PlainInit cannot write, got path %q", path)
	}
	if err == ErrAlreadyExists {
		t.Fatalf("Create returned ErrAlreadyExists instead of the underlying PlainInit failure")
	}

	wantPath := filepath.Join(dir, "intranet-backend.git")
	if _, statErr := os.Stat(wantPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected orphaned directory to be cleaned up after PlainInit failure, os.Stat err = %v", statErr)
	}

	removeCmd := exec.Command("icacls", dir, "/remove:d", principal)
	if out, err := removeCmd.CombinedOutput(); err != nil {
		t.Fatalf("icacls remove deny failed: %v: %s", err, out)
	}

	retryPath, err := store.Create("intranet-backend")
	if err != nil {
		t.Fatalf("retry Create failed; name should be reusable after cleanup: %v", err)
	}
	if retryPath != wantPath {
		t.Errorf("retry path = %q, want %q", retryPath, wantPath)
	}
}

func TestCreate_SetsMainAsDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	path, err := store.Create("branch-check")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	headBytes, err := os.ReadFile(filepath.Join(path, "HEAD"))
	if err != nil {
		t.Fatalf("failed to read HEAD: %v", err)
	}

	head := strings.TrimSpace(string(headBytes))
	if head != "ref: refs/heads/main" {
		t.Errorf("HEAD = %q, want %q", head, "ref: refs/heads/main")
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

func TestList_SkipsBareDotGitDirectory(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// A directory literally named ".git" (not "something.git") can't be
	// produced via Create, but simulate it being created some other way to
	// exercise List's defensive skip of the resulting empty stripped name.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatalf("failed to create bare .git directory: %v", err)
	}

	names, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	for _, n := range names {
		if n == "" {
			t.Fatalf("List returned an empty name entry: %v", names)
		}
	}
	if len(names) != 1 || names[0] != "intranet-backend" {
		t.Fatalf("names = %v, want [intranet-backend]", names)
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
