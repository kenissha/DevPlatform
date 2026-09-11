package taskboard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func linkFor(hash, subject string, at time.Time) CommitLink {
	return CommitLink{Hash: hash, Subject: subject, Author: "Test Person", At: at}
}

const hashA = "0123456789abcdef0123456789abcdef01234567"
const hashB = "89abcdef0123456789abcdef0123456789abcdef"

func TestLinkCommitAttachesToTheTaskWithThatKey(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Tarih filtresi", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	linked, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, "tarih filtresi düzeltildi", time.Now()))
	if err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}
	if !linked {
		t.Fatal("linked = false, want true")
	}

	commits, err := store.Commits("deneme", task.ID)
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(commits) != 1 || commits[0].Hash != hashA {
		t.Fatalf("commits = %+v", commits)
	}
}

// A re-push, a mirror, or the same commit arriving on a second branch
// must not make the task look like it was worked on twice.
func TestLinkCommitIsIdempotentByHash(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, "ilk", time.Now())); err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}
	linked, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, "ilk", time.Now()))
	if err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}
	if linked {
		t.Error("linked = true on a repeat, want false")
	}

	commits, _ := store.Commits("deneme", task.ID)
	if len(commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(commits))
	}
}

// People mistype keys and mention tasks that were since deleted. Neither
// is a reason to fail — this runs inside somebody's push.
func TestLinkCommitIgnoresAnUnknownKey(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Create("deneme", "Görev", "", "", "dev-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	linked, err := store.LinkCommit("deneme", "DEN-999", linkFor(hashA, "hayalet", time.Now()))
	if err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}
	if linked {
		t.Error("linked = true for an unknown key")
	}
}

func TestLinkCommitRejectsAMalformedHash(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, bad := range []string{"", "abc123", "ZZZZ56789abcdef0123456789abcdef01234567"} {
		if _, err := store.LinkCommit("deneme", task.Key, linkFor(bad, "x", time.Now())); !errors.Is(err, ErrInvalidCommit) {
			t.Errorf("hash %q: err = %v, want ErrInvalidCommit", bad, err)
		}
	}
}

func TestCommitsAreNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, "eski", old)); err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}
	if _, err := store.LinkCommit("deneme", task.Key, linkFor(hashB, "yeni", recent)); err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}

	commits, _ := store.Commits("deneme", task.ID)
	if len(commits) != 2 || commits[0].Hash != hashB {
		t.Fatalf("commits = %+v, want the newest first", commits)
	}
}

// A commit page shows a one-line summary; storing whole message bodies
// would put an unbounded amount of text in a file read on every view.
func TestLinkCommitKeepsOnlyTheFirstLineOfTheMessage(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	message := "başlık satırı\n\nuzun bir gövde\nikinci satır"
	if _, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, message, time.Now())); err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}

	commits, _ := store.Commits("deneme", task.ID)
	if commits[0].Subject != "başlık satırı" {
		t.Fatalf("subject = %q", commits[0].Subject)
	}
}

// The whole feature, end to end: what the git server hands over is a
// message, and what comes out is a link on the right task.
func TestLinkCommitMessageResolvesKeysItself(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("deneme", "Birinci", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := store.Create("deneme", "İkinci", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	message := first.Key + " ve " + second.Key + " birlikte düzeltildi"
	if err := store.LinkCommitMessage("deneme", hashA, message, "Test Person", time.Now()); err != nil {
		t.Fatalf("LinkCommitMessage: %v", err)
	}

	for _, task := range []Task{first, second} {
		commits, err := store.Commits("deneme", task.ID)
		if err != nil {
			t.Fatalf("Commits(%s): %v", task.Key, err)
		}
		if len(commits) != 1 {
			t.Errorf("%s got %d commits, want 1", task.Key, len(commits))
		}
	}
}

func TestLinkCommitMessageIgnoresAMessageWithNoKey(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.LinkCommitMessage("deneme", hashA, "ufak temizlik", "Test Person", time.Now()); err != nil {
		t.Fatalf("LinkCommitMessage: %v", err)
	}

	commits, _ := store.Commits("deneme", task.ID)
	if len(commits) != 0 {
		t.Fatalf("commits = %+v, want none", commits)
	}
}

// A repository that has never had a task has no prefix, so there is
// nothing a message could be matched against.
func TestLinkCommitMessageOnARepoWithNoTasksIsANoOp(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.LinkCommitMessage("bos", hashA, "DEN-1 bir iş", "Test Person", time.Now()); err != nil {
		t.Fatalf("LinkCommitMessage: %v", err)
	}
}

// The same constraint comments.go documents: List decodes every *.json in
// a repository's directory as a Task, so this file must not land beside
// the task files.
func TestCommitLinksLiveInASubdirectoryAndDoNotBreakTheBoard(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.LinkCommit("deneme", task.Key, linkFor(hashA, "iş", time.Now())); err != nil {
		t.Fatalf("LinkCommit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "deneme", "commits", task.ID+".json")); err != nil {
		t.Fatalf("commit file is not in the commits/ subdirectory: %v", err)
	}

	tasks, err := store.List("deneme")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("List returned %d tasks, want 1", len(tasks))
	}
}

func TestCommitsForAnUnknownTaskIs404(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Commits("deneme", "00112233445566aa"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
