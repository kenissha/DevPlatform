package repoimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An explicit setting must win outright. Somebody who set it knows
// something about the machine that no search will discover.
func TestFindGit_PrefersTheConfiguredPath(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "git.exe")
	if err := os.WriteFile(fake, []byte("not really git"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv(GitPathEnv, fake)

	got, err := findGit()
	if err != nil {
		t.Fatalf("findGit: %v", err)
	}
	if got != fake {
		t.Fatalf("git = %q, want %q", got, fake)
	}
}

// Pointing at a path that is not there is a mistake worth naming. Falling
// back to a search would silently run a different git than the one that
// was asked for — and on a machine with two installs, quietly using the
// wrong one is worse than refusing.
func TestFindGit_ConfiguredButMissingIsAnError(t *testing.T) {
	t.Setenv(GitPathEnv, filepath.Join(t.TempDir(), "yok.exe"))

	_, err := findGit()
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if !strings.Contains(err.Error(), GitPathEnv) {
		t.Fatalf("hata değişkenin adını söylemiyor: %v", err)
	}
}

// A directory is not a program. Without the IsDir check, a path like
// "C:\Program Files\Git" would be accepted and then fail at exec time
// with the unhelpful error this whole file exists to avoid.
func TestFindGit_ConfiguredDirectoryIsRejected(t *testing.T) {
	t.Setenv(GitPathEnv, t.TempDir())

	if _, err := findGit(); err == nil {
		t.Fatal("dizin kabul edildi, hata bekleniyordu")
	}
}

// The message is the whole point: exec's own "executable file not found
// in %PATH%" is true and useless. This one has to say what to do.
func TestErrGitMissing_SaysWhatToDo(t *testing.T) {
	msg := ErrGitMissing.Error()
	for _, want := range []string{"Git for Windows", GitPathEnv, "git.exe"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mesajda %q geçmiyor: %s", want, msg)
		}
	}
}

// Whatever this machine has, the resolved path must be a real file —
// GitPath's answer is handed straight to exec.
func TestGitPath_ResolvesToARealFileWhenGitExists(t *testing.T) {
	path, err := GitPath()
	if err != nil {
		t.Skipf("bu makinede git yok: %v", err)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("çözülen yol yok: %q (%v)", path, statErr)
	}
	if info.IsDir() {
		t.Fatalf("çözülen yol bir dizin: %q", path)
	}
}
