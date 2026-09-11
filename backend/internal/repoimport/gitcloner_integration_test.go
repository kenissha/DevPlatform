package repoimport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git bulunamadı")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// buildRepo makes a repository shaped like the ones this feature exists
// for: a credential committed early, removed later, and still sitting in
// the history where a push-time scan would reject the whole import.
func buildRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "dev@localhost")
	runGit(t, dir, "config", "user.name", "Test Person")
	runGit(t, dir, "config", "core.autocrlf", "false")

	write := func(name, body string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	write("README.md", "proje\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "ilk commit")

	write("appsettings.Production.json",
		`{"ConnectionStrings":{"Default":"Server=sql01;Database=X;User=sa;Password=P@ssw0rd;"}}`)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "ayarlar eklendi")

	// Removed — but the blob stays reachable from every later commit,
	// which is exactly why pushing this history gets rejected.
	runGit(t, dir, "rm", "-q", "appsettings.Production.json")
	runGit(t, dir, "commit", "-m", "ayarlari repodan cikar")

	runGit(t, dir, "branch", "feature/bir-sey")
	return dir
}

func TestGitCloner_BringsTheHistoryAndDropsTheRemote(t *testing.T) {
	requireGit(t)

	source := buildRepo(t)
	dest := filepath.Join(t.TempDir(), "imported.git")

	if err := (GitCloner{}).Clone(context.Background(), source, "", dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	commits, branches := countRefs(dest)
	if commits != 3 {
		t.Errorf("commit = %d, want 3", commits)
	}
	if branches != 2 {
		t.Errorf("dal = %d, want 2", branches)
	}

	// A bare repository on this server must not point back at where it
	// came from — see GitCloner.Clone.
	out, err := exec.Command("git", "-C", dest, "remote").Output()
	if err != nil {
		t.Fatalf("git remote: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("remote kaldı: %q", out)
	}
}

// The whole point of scanning after an import: the secret is gone from
// the working tree but still in the history, and the person needs to be
// told rather than left to assume the import was clean.
func TestScanHistory_FindsASecretThatWasAlreadyDeleted(t *testing.T) {
	requireGit(t)

	source := buildRepo(t)
	dest := filepath.Join(t.TempDir(), "imported.git")
	if err := (GitCloner{}).Clone(context.Background(), source, "", dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	findings := ScanHistory(dest)
	if len(findings) != 1 {
		t.Fatalf("bulgu = %+v, want 1", findings)
	}
	if findings[0].Pattern != "connection-string-password" {
		t.Errorf("pattern = %q", findings[0].Pattern)
	}
	// The path makes the finding actionable; a blob hash would not.
	if findings[0].Path != "appsettings.Production.json" {
		t.Errorf("path = %q, want appsettings.Production.json", findings[0].Path)
	}
}

func TestScanHistory_CleanRepositoryReportsNothing(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "dev@localhost")
	runGit(t, dir, "config", "user.name", "Test Person")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("temiz\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "ilk")

	dest := filepath.Join(t.TempDir(), "imported.git")
	if err := (GitCloner{}).Clone(context.Background(), dir, "", dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if findings := ScanHistory(dest); len(findings) != 0 {
		t.Fatalf("bulgu = %+v, want none", findings)
	}
}

// A source that does not exist must fail the job, not leave a
// half-repository behind that repostore.List reports as real.
func TestImport_LeavesNothingBehindWhenTheCloneFails(t *testing.T) {
	requireGit(t)

	root := t.TempDir()
	s := NewStore(root, GitCloner{})
	// Start validates the URL, so this reaches git and fails there.
	job, err := s.Start("yok", "https://localhost:1/olmayan-depo.git", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := waitFor(t, s, job.ID)

	if done.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", done.Status)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("geride kaldı: %s", e.Name())
	}
}

// racingCloner clones for real, then creates the destination — reproducing
// the one case Start's own pre-check cannot catch: minutes pass between
// claiming a name and finishing the clone, and somebody can create that
// repository from the panel in between.
type racingCloner struct {
	source      string
	destination string
}

func (c racingCloner) Clone(ctx context.Context, _, token, destDir string) error {
	if err := (GitCloner{}).Clone(ctx, c.source, token, destDir); err != nil {
		return err
	}
	return os.MkdirAll(c.destination, 0o750)
}

// The clone is the expensive part — potentially hundreds of megabytes
// already pulled over the network. A rename that needs retrying by hand
// must not cost that download a second time.
func TestImport_KeepsTheDownloadWhenTheMoveFails(t *testing.T) {
	requireGit(t)

	root := t.TempDir()
	source := buildRepo(t)
	s := NewStore(root, racingCloner{source: source, destination: filepath.Join(root, "cakisan.git")})

	job, err := s.Start("cakisan", "https://github.com/x/y.git", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := waitFor(t, s, job.ID)
	if done.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", done.Status)
	}

	var staged string
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".import-") {
			staged = e.Name()
		}
	}
	if staged == "" {
		t.Fatal("indirilen kopya silinmiş — yeniden indirmek gerekirdi")
	}
	// And the message has to say where it is, or keeping it helps nobody.
	if !strings.Contains(done.Error, staged) {
		t.Fatalf("hata mesajı kopyanın yerini söylemiyor: %s", done.Error)
	}
}

// The mirror of the above: a successful import leaves no staging
// directory behind.
func TestImport_LeavesNoStagingDirectoryOnSuccess(t *testing.T) {
	requireGit(t)

	root := t.TempDir()
	source := buildRepo(t)
	s := NewStore(root, localCloner{source: source})

	job, err := s.Start("basarili", "https://github.com/x/y.git", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := waitFor(t, s, job.ID)
	if done.Status != StatusDone {
		t.Fatalf("status = %q (%s)", done.Status, done.Error)
	}

	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".import-") {
			t.Errorf("geride kaldı: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "basarili.git")); err != nil {
		t.Fatalf("depo yerine konmamış: %v", err)
	}
}

// localCloner ignores the URL and clones a local path, so a Store test can
// exercise the real GitCloner without a network.
type localCloner struct{ source string }

func (c localCloner) Clone(ctx context.Context, _, token, destDir string) error {
	return (GitCloner{}).Clone(ctx, c.source, token, destDir)
}
