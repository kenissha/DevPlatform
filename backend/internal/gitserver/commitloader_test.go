package gitserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// recordingAuthors stands in for gitemails.Store, capturing what the
// push path reported instead of persisting it.
type recordingAuthors struct {
	mu   sync.Mutex
	seen map[string][]string
}

func (r *recordingAuthors) RecordSeen(subject, email string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string][]string{}
	}
	for _, existing := range r.seen[subject] {
		if existing == email {
			return nil
		}
	}
	r.seen[subject] = append(r.seen[subject], email)
	return nil
}

func (r *recordingAuthors) forSubject(subject string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen[subject]...)
}

// recordingLinker stands in for taskboard.Store, capturing the commits
// the push path reported instead of resolving task keys against a board.
type recordingLinker struct {
	mu   sync.Mutex
	seen []linkedCommit
}

type linkedCommit struct {
	repo    string
	hash    string
	message string
	author  string
	at      time.Time
}

func (l *recordingLinker) LinkCommitMessage(repo, hash, message, author string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, linkedCommit{repo, hash, message, author, at})
	return nil
}

func (l *recordingLinker) commits() []linkedCommit {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]linkedCommit(nil), l.seen...)
}

// withSubjectContext wraps h so every request carries WithSubject, the
// way gittoken.RequireTokenAndAccess does once it has authenticated the
// pusher.
func withSubjectContext(h http.Handler, subject string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(WithSubject(r.Context(), subject)))
	})
}

// pushOneCommit seeds a working copy whose single commit is authored by
// email, and pushes it to repoName on srv.
func pushOneCommit(t *testing.T, srvURL, repoName, email string) {
	t.Helper()
	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", email)
	runGit(t, work, "config", "user.name", "Test Person")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srvURL+"/"+repoName+".git")
	runGit(t, work, "push", "origin", "feature-x")
}

// TestPush_RecordsTheCommitAuthorAgainstThePusher is the whole point of
// this loader: the platform can only learn which git signature belongs to
// whom at the moment someone authenticated pushes commits carrying it.
func TestPush_RecordsTheCommitAuthorAgainstThePusher(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("authors"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	authors := &recordingAuthors{}
	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, authors, nil), "dev-1"))
	defer srv.Close()

	pushOneCommit(t, srv.URL, "authors", "personal@gmail.com")

	got := authors.forSubject("dev-1")
	if len(got) != 1 || got[0] != "personal@gmail.com" {
		t.Errorf("recorded = %v, want [personal@gmail.com]", got)
	}
}

// Without an authenticated subject there is nobody to attribute the
// signature to, so nothing may be recorded — this is what keeps an
// unwrapped handler (this package's own other tests) from inventing
// attributions.
func TestPush_RecordsNothingWhenThePusherIsUnknown(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("anon"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	authors := &recordingAuthors{}
	srv := httptest.NewServer(NewHandler(dataDir, authors, nil))
	defer srv.Close()

	pushOneCommit(t, srv.URL, "anon", "personal@gmail.com")

	if got := authors.forSubject(""); len(got) != 0 {
		t.Errorf("recorded = %v, want nothing for an unauthenticated push", got)
	}
}

// A nil recorder must leave pushing completely unaffected — the git
// server has to work on a deployment that never wired this up.
func TestPush_StillSucceedsWithoutARecorder(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("norecorder"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil, nil), "dev-1"))
	defer srv.Close()

	// runGit fails the test if the push is rejected.
	pushOneCommit(t, srv.URL, "norecorder", "personal@gmail.com")
}

func TestAuthorEmail_ReadsTheAddressOutOfACommitObject(t *testing.T) {
	raw := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
		"parent 1234567890123456789012345678901234567890\n" +
		"author Rifat Öztürk <personal@gmail.com> 1757000000 +0300\n" +
		"committer Rifat Öztürk <other@example.com> 1757000000 +0300\n" +
		"\n" +
		"commit mesajı\n")

	got, ok := authorEmail(raw)
	if !ok {
		t.Fatal("authorEmail reported no address")
	}
	// The author, not the committer: the graph credits who wrote the
	// change, matching how gitstats counts commits.
	if got != "personal@gmail.com" {
		t.Errorf("authorEmail = %q, want %q", got, "personal@gmail.com")
	}
}

func TestAuthorEmail_IgnoresSomethingThatIsNotACommit(t *testing.T) {
	for _, raw := range []string{
		"",
		"just some blob content\n",
		"author with no angle brackets 123 +0300\n",
		"author Broken <unclosed 123 +0300\n",
	} {
		if got, ok := authorEmail([]byte(raw)); ok {
			t.Errorf("authorEmail(%q) = %q, want no address", raw, got)
		}
	}
}

// pushCommitWithMessage is pushOneCommit with a message the caller
// chooses — the message is the whole subject of the tests below.
func pushCommitWithMessage(t *testing.T, srvURL, repoName, message string) {
	t.Helper()
	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "person@example.com")
	runGit(t, work, "config", "user.name", "Test Person")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", message)
	runGit(t, work, "remote", "add", "origin", srvURL+"/"+repoName+".git")
	runGit(t, work, "push", "origin", "feature-x")
}

// The linker gets the real commit: the name git computed, the message
// somebody actually typed, and the author line as written. Everything the
// task board does with a commit depends on these being right, so this
// checks them against git's own answer rather than against a fixture.
func TestPush_ReportsEachCommitToTheLinker(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("linked"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	linker := &recordingLinker{}
	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil, linker), "dev-1"))
	defer srv.Close()

	pushCommitWithMessage(t, srv.URL, "linked", "DEN-14 tarih filtresi düzeltildi")

	got := linker.commits()
	if len(got) != 1 {
		t.Fatalf("linked %d commits, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.repo != "linked" {
		t.Errorf("repo = %q, want %q — the name must lose its .git suffix", c.repo, "linked")
	}
	if c.message != "DEN-14 tarih filtresi düzeltildi" {
		t.Errorf("message = %q", c.message)
	}
	if c.author != "Test Person" {
		t.Errorf("author = %q, want %q", c.author, "Test Person")
	}
	if c.at.IsZero() {
		t.Error("at is zero — the commit timestamp was not parsed")
	}
}

// The hash is computed from the object payload rather than read off
// go-git, so it has to match what git itself named the commit. If this
// ever drifts, every link points at a commit nobody can find.
func TestPush_ComputesTheSameHashGitDid(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("hashes"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	linker := &recordingLinker{}
	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil, linker), "dev-1"))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "person@example.com")
	runGit(t, work, "config", "user.name", "Test Person")
	runGit(t, work, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "HAS-1 bir iş")
	// runGit returns git's raw output, newline and all.
	wantHash := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	runGit(t, work, "remote", "add", "origin", srv.URL+"/hashes.git")
	runGit(t, work, "push", "origin", "feature-x")

	got := linker.commits()
	if len(got) != 1 {
		t.Fatalf("linked %d commits, want 1", len(got))
	}
	if got[0].hash != wantHash {
		t.Errorf("hash = %q, want %q (git rev-parse)", got[0].hash, wantHash)
	}
}

// A push carrying several commits must report every one of them: people
// push a branch's worth of work at once, and only linking the tip would
// silently drop most of it.
func TestPush_ReportsEveryCommitInThePush(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("many"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	linker := &recordingLinker{}
	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil, linker), "dev-1"))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "person@example.com")
	runGit(t, work, "config", "user.name", "Test Person")
	runGit(t, work, "config", "core.autocrlf", "false")
	for i, message := range []string{"MAN-1 ilk", "MAN-2 ikinci", "üçüncü, anahtarsız"} {
		name := filepath.Join(work, "file"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		runGit(t, work, "add", ".")
		runGit(t, work, "commit", "-m", message)
	}
	runGit(t, work, "remote", "add", "origin", srv.URL+"/many.git")
	runGit(t, work, "push", "origin", "feature-x")

	if got := linker.commits(); len(got) != 3 {
		t.Fatalf("linked %d commits, want 3: %+v", len(got), got)
	}
}

// A nil linker must leave pushing completely unaffected, the same way a
// nil recorder does — the git server has to work on a deployment that
// never wired the task board up.
func TestPush_StillSucceedsWithoutALinker(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("nolinker"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil, nil), "dev-1"))
	defer srv.Close()

	pushCommitWithMessage(t, srv.URL, "nolinker", "DEN-1 bir iş")
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/deneme.git", "deneme"},
		{"/deneme.git/", "deneme"},
		{"/git/deneme.git", "deneme"},
		{"/deneme", "deneme"},
		{"/", ""},
		{"", ""},
	}
	for _, tt := range tests {
		u, err := url.Parse("http://x" + tt.path)
		if err != nil {
			t.Fatalf("bad test URL: %v", err)
		}
		if got := repoNameFromURL(u); got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
