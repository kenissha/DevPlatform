package audit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLog_AppendsAndListReturnsNewestFirst(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))

	if err := logger.Log("dev-1", ActionTaskCreated, "repo-a", "t1", "Görev açıldı"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if err := logger.Log("admin-1", ActionMRApproved, "repo-a", "m1", "Merge onaylandı"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	events, err := logger.List(10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Action != ActionMRApproved {
		t.Errorf("newest event action = %q, want %q", events[0].Action, ActionMRApproved)
	}
	if events[0].Actor != "admin-1" || events[0].Repo != "repo-a" || events[0].Target != "m1" {
		t.Errorf("newest event = %+v, unexpected fields", events[0])
	}
	if events[0].At.IsZero() {
		t.Error("expected At to be stamped")
	}
}

func TestLog_NeverRewritesExistingLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger := New(path)

	if err := logger.Log("dev-1", ActionRepoCreated, "repo-a", "repo-a", "ilk"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	if err := logger.Log("dev-1", ActionRepoCreated, "repo-b", "repo-b", "ikinci"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	// The immutability claim is "append-only": whatever was on disk before
	// must still be a byte-identical prefix afterwards.
	if !strings.HasPrefix(string(second), string(first)) {
		t.Fatalf("second write did not preserve the first as a prefix:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestList_RespectsLimit(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))
	for i := 0; i < 5; i++ {
		if err := logger.Log("dev-1", ActionTaskUpdated, "repo-a", "t1", "güncellendi"); err != nil {
			t.Fatalf("Log returned error: %v", err)
		}
	}

	events, err := logger.List(3)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
}

func TestList_EmptyWhenNothingLogged(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))

	events, err := logger.List(10)
	if err != nil {
		t.Fatalf("List on a missing log returned error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestList_SkipsCorruptLinesWithoutHidingLaterOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger := New(path)
	if err := logger.Log("dev-1", ActionTaskCreated, "repo-a", "t1", "ilk"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	if _, err := f.WriteString("this is not json\n"); err != nil {
		t.Fatalf("failed to append garbage: %v", err)
	}
	f.Close()

	if err := logger.Log("dev-1", ActionTaskCreated, "repo-a", "t2", "ikinci"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	events, err := logger.List(10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (the corrupt line must not hide the ones after it)", len(events))
	}
}

func TestLog_NilLoggerIsANoOp(t *testing.T) {
	var logger *Logger

	if err := logger.Log("dev-1", ActionTaskCreated, "repo-a", "t1", "x"); err != nil {
		t.Fatalf("nil Logger.Log returned error: %v", err)
	}
	events, err := logger.List(10)
	if err != nil {
		t.Fatalf("nil Logger.List returned error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestLog_IsSafeUnderConcurrentWriters(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := logger.Log("dev-1", ActionTaskUpdated, "repo-a", "t1", "eşzamanlı"); err != nil {
				t.Errorf("Log returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	events, err := logger.List(100)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 20 {
		t.Fatalf("got %d events, want 20 — a concurrent write was lost or interleaved", len(events))
	}
}

// A task's history is the same append-only file, asked a narrower
// question — this is what makes a per-task timeline possible without a
// second index.
func TestListForTarget_ReturnsOnlyThatEntitysEvents(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))

	writes := []struct {
		actor, repo, target, summary string
	}{
		{"dev-1", "deneme", "task-a", "Görev açıldı"},
		{"dev-2", "deneme", "task-b", "Başka görev açıldı"},
		{"dev-2", "deneme", "task-a", "durum → yapılıyor"},
		{"dev-1", "oasrapor", "task-a", "Aynı id, başka repo"},
	}
	for _, w := range writes {
		if err := logger.Log(w.actor, ActionTaskUpdated, w.repo, w.target, w.summary); err != nil {
			t.Fatalf("Log failed: %v", err)
		}
	}

	events, err := logger.ListForTarget("deneme", "task-a", 50)
	if err != nil {
		t.Fatalf("ListForTarget failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 — %+v", len(events), events)
	}
	// Newest first, like List.
	if events[0].Summary != "durum → yapılıyor" {
		t.Errorf("first event = %q, want the newest one", events[0].Summary)
	}
	// The repo has to be part of the match: two repos can hold entities
	// with the same id, and mixing their histories would be worse than
	// showing none.
	for _, e := range events {
		if e.Repo != "deneme" {
			t.Errorf("event from repo %q leaked into deneme's history", e.Repo)
		}
	}
}

func TestListForTarget_EmptyTargetReturnsNothing(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err := logger.Log("dev-1", ActionTaskUpdated, "deneme", "task-a", "bir şey"); err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	// An empty target would otherwise match every event that has no
	// target at all, which is the opposite of "history of this thing".
	events, err := logger.ListForTarget("deneme", "", 50)
	if err != nil {
		t.Fatalf("ListForTarget failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events for an empty target, want 0", len(events))
	}
}

func TestListForTarget_NilLoggerIsSafe(t *testing.T) {
	var logger *Logger
	events, err := logger.ListForTarget("deneme", "task-a", 50)
	if err != nil || len(events) != 0 {
		t.Errorf("nil logger returned (%v, %v), want (empty, nil)", events, err)
	}
}
