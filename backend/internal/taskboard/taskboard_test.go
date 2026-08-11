package taskboard

import "testing"

func TestCreate_PersistsAndReturnsInProgressTask(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("intranet-backend", "Fix login bug", "Repro steps...", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if task.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", task.Status, StatusInProgress)
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
	updated, err := store.Update("intranet-backend", created.ID, &awaitingTest, nil, nil)
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
	updated2, err := store.Update("intranet-backend", created.ID, nil, &urgent, &newAssignee)
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
	_, err = store.Update("intranet-backend", created.ID, &bogus, nil, nil)
	if err != ErrInvalidStatus {
		t.Fatalf("err = %v, want ErrInvalidStatus", err)
	}
}
