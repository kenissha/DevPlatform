package gitserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, authors), "dev-1"))
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
	srv := httptest.NewServer(NewHandler(dataDir, authors))
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

	srv := httptest.NewServer(withSubjectContext(NewHandler(dataDir, nil), "dev-1"))
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
