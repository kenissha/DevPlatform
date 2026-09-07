package taskboard

import (
	"errors"
	"testing"
)

func TestCreate_PersistsAndReturnsATodoTask(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("intranet-backend", "Fix login bug", "Repro steps...", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if task.Status != StatusTodo {
		t.Errorf("Status = %q, want %q — a task starts written-down, not started", task.Status, StatusTodo)
	}
	if task.Urgent {
		t.Error("expected Urgent to default to false")
	}
	if task.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreate_RejectsInvalidRepoName(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Create("../escape", "title", "desc", "dev-1", "dev-1")
	if err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestGet_ReturnsCreatedTask(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "desc", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != created {
		t.Errorf("got %+v, want %+v", got, created)
	}
}

func TestGet_ReturnsErrNotFoundForMissingID(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Get("intranet-backend", "0123456789abcdef")
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGet_RejectsInvalidID(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Get("intranet-backend", "../../escape")
	if err != ErrInvalidID {
		t.Fatalf("err = %v, want ErrInvalidID", err)
	}
}

func TestList_ReturnsAllTasksNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Create("intranet-backend", "First", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("intranet-backend", "Second", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tasks, err := store.List("intranet-backend")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != second.ID || tasks[1].ID != first.ID {
		t.Errorf("expected newest-first order [%s, %s], got [%s, %s]",
			second.ID, first.ID, tasks[0].ID, tasks[1].ID)
	}
}

func TestList_ReturnsEmptySliceForRepoWithNoTasks(t *testing.T) {
	store := NewStore(t.TempDir())

	tasks, err := store.List("never-touched")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("got %d tasks, want 0", len(tasks))
	}
}

func TestUpdate_ChangesOnlyProvidedFields(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "desc", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	awaitingTest := StatusAwaitingTest
	updated, err := store.Update("intranet-backend", created.ID, Changes{Status: &awaitingTest})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Status != StatusAwaitingTest {
		t.Errorf("Status = %q, want %q", updated.Status, StatusAwaitingTest)
	}
	if updated.AssignedTo != "dev-1" {
		t.Errorf("AssignedTo = %q, want unchanged %q", updated.AssignedTo, "dev-1")
	}

	urgent := true
	newAssignee := "dev-2"
	updated2, err := store.Update("intranet-backend", created.ID, Changes{Urgent: &urgent, AssignedTo: &newAssignee})
	if err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}
	if !updated2.Urgent {
		t.Error("expected Urgent = true")
	}
	if updated2.AssignedTo != "dev-2" {
		t.Errorf("AssignedTo = %q, want %q", updated2.AssignedTo, "dev-2")
	}
	if updated2.Status != StatusAwaitingTest {
		t.Errorf("Status = %q, want unchanged %q", updated2.Status, StatusAwaitingTest)
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after Update failed: %v", err)
	}
	if reread != updated2 {
		t.Errorf("persisted = %+v, want %+v", reread, updated2)
	}
}

func TestUpdate_RejectsInvalidStatus(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "desc", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	bogus := Status("bogus")
	_, err = store.Update("intranet-backend", created.ID, Changes{Status: &bogus})
	if err != ErrInvalidStatus {
		t.Fatalf("err = %v, want ErrInvalidStatus", err)
	}
}

// Every status the API accepts has to be storable, or a board column
// exists that nothing can ever be dragged into.
func TestUpdate_AcceptsEveryValidStatus(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, status := range []Status{StatusTodo, StatusInProgress, StatusAwaitingTest, StatusDone} {
		s := status
		updated, err := store.Update("sample", task.ID, Changes{Status: &s})
		if err != nil {
			t.Fatalf("Update to %q failed: %v", status, err)
		}
		if updated.Status != status {
			t.Errorf("Status = %q, want %q", updated.Status, status)
		}
	}
}

func TestUpdate_RejectsAnUnknownStatus(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	bogus := Status("backlog")
	if _, err := store.Update("sample", task.ID, Changes{Status: &bogus}); err == nil {
		t.Error("Update accepted an unknown status")
	}
}

func TestUpdate_EditsTitleAndDescription(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Eski başlık", "eski açıklama", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	title, description := "Yeni başlık", "yeni açıklama"
	updated, err := store.Update("sample", task.ID, Changes{Title: &title, Description: &description})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Title != "Yeni başlık" || updated.Description != "yeni açıklama" {
		t.Errorf("got %q / %q, want the new title and description", updated.Title, updated.Description)
	}

	// The edit has to survive the round trip to disk, not just the return
	// value — that is the difference between an edit and a mirage.
	reread, err := store.Get("sample", task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Title != "Yeni başlık" {
		t.Errorf("re-read title = %q, want the new one", reread.Title)
	}
}

func TestUpdate_TrimsEditedText(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Başlık", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	title := "  boşluklu başlık  "
	updated, err := store.Update("sample", task.ID, Changes{Title: &title})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Title != "boşluklu başlık" {
		t.Errorf("title = %q, want it trimmed", updated.Title)
	}
}

// The title is the only thing a board card renders, so a blank one is a
// card nobody can identify.
func TestUpdate_RejectsAnEmptyTitle(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Başlık", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, blank := range []string{"", "   "} {
		title := blank
		if _, err := store.Update("sample", task.ID, Changes{Title: &title}); !errors.Is(err, ErrEmptyTitle) {
			t.Errorf("Update with title %q = %v, want ErrEmptyTitle", blank, err)
		}
	}

	// And the stored task must be untouched by the rejected attempt.
	reread, err := store.Get("sample", task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Title != "Başlık" {
		t.Errorf("title = %q, want the original after a rejected edit", reread.Title)
	}
}

// An omitted field must be left alone, or a UI that only meant to rename
// something would blank out its description.
func TestUpdate_LeavesOmittedFieldsAlone(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Başlık", "açıklama", "dev-2", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	title := "Yeni başlık"
	updated, err := store.Update("sample", task.ID, Changes{Title: &title})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Description != "açıklama" {
		t.Errorf("description = %q, want it untouched", updated.Description)
	}
	if updated.AssignedTo != "dev-2" {
		t.Errorf("assignedTo = %q, want it untouched", updated.AssignedTo)
	}
	if updated.Status != StatusTodo {
		t.Errorf("status = %q, want it untouched", updated.Status)
	}
}

func TestDelete_RemovesTheTask(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("sample", "Silinecek", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.Delete("sample", task.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := store.Get("sample", task.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}

	tasks, err := store.List("sample")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("List = %v, want empty after the only task was deleted", tasks)
	}
}

// Deleting something already gone reports it rather than succeeding, so a
// caller working from a stale board learns their view is out of date.
func TestDelete_ReportsAMissingTask(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Create("sample", "Var olan", "", "", "dev-1"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.Delete("sample", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete of a missing task = %v, want ErrNotFound", err)
	}
}

func TestDelete_RejectsPathTraversalAttempts(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Delete("sample", "../../etc/passwd"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("Delete with a traversal id = %v, want ErrInvalidID", err)
	}
	if err := store.Delete("../escape", "0123456789abcdef"); !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("Delete with a traversal repo = %v, want ErrInvalidRepo", err)
	}
}
