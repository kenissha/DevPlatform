package taskboard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if task.Priority != PriorityNormal {
		t.Errorf("Priority = %q, want %q — a new task is not special until somebody says so", task.Priority, PriorityNormal)
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
	// reflect.DeepEqual, not ==: Task carries a Subtasks slice now, and a
	// struct containing a slice is not comparable.
	if !reflect.DeepEqual(got, created) {
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

	high := PriorityHigh
	newAssignee := "dev-2"
	updated2, err := store.Update("intranet-backend", created.ID, Changes{Priority: &high, AssignedTo: &newAssignee})
	if err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}
	if updated2.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q", updated2.Priority, PriorityHigh)
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
	if !reflect.DeepEqual(reread, updated2) {
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

// ----------------------------------------------------- keys & priority

func TestCreate_AssignsAReadableKey(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("deneme", "Bir", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("deneme", "İki", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if first.Key != "DEN-1" {
		t.Errorf("first key = %q, want %q", first.Key, "DEN-1")
	}
	if second.Key != "DEN-2" {
		t.Errorf("second key = %q, want %q", second.Key, "DEN-2")
	}
}

// Each repository counts on its own, so one busy project does not push
// another project's numbers into the hundreds.
func TestCreate_CountsPerRepository(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Create("deneme", "Bir", "", "", "dev-1"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.Create("deneme", "İki", "", "", "dev-1"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	other, err := store.Create("oasrapor", "Bir", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if other.Key != "OAS-1" {
		t.Errorf("other repo's first key = %q, want %q", other.Key, "OAS-1")
	}
}

// A deleted task's number is never handed out again: somebody who wrote
// "DEN-3" in a commit message must not find it pointing at different work
// a month later.
func TestCreate_DoesNotReuseADeletedNumber(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("deneme", "Silinecek", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := store.Delete("deneme", first.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	next, err := store.Create("deneme", "Yeni", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if next.Key == first.Key {
		t.Errorf("key %q was reused after the task was deleted", next.Key)
	}
	if next.Key != "DEN-2" {
		t.Errorf("key = %q, want DEN-2 — the sequence should continue, leaving a gap", next.Key)
	}
}

func TestKeyPrefix(t *testing.T) {
	cases := []struct {
		repo string
		want string
	}{
		{"deneme", "DEN"},
		{"oasrapor-frontend", "OAS"},
		{"intranet-servis", "INT"},
		// Turkish letters fold to ASCII: a key gets typed on any keyboard
		// and travels through commit messages and URLs.
		{"çalışma", "CAL"},
		{"ürün-takip", "URU"},
		{"ışık", "ISI"},
		// Punctuation and separators are skipped, not counted.
		{"a-b-c-d", "ABC"},
		{"x1-y2", "X1Y"},
		// Nothing usable at all still produces a well-formed key.
		{"---", "TSK"},
		{"", "TSK"},
	}

	for _, tc := range cases {
		if got := keyPrefix(tc.repo); got != tc.want {
			t.Errorf("keyPrefix(%q) = %q, want %q", tc.repo, got, tc.want)
		}
	}
}

// Two repositories starting with the same letters must not share a
// prefix, or DEN-4 would be ambiguous. The first one keeps the short
// form; the second lengthens.
func TestResolvePrefix_AvoidsCollisions(t *testing.T) {
	taken := map[string]string{"intranet-servis": "INT"}

	got := resolvePrefix("intranet-frontend", taken)
	if got == "INT" {
		t.Fatal("second repo took the prefix already in use")
	}
	if got != "INTR" {
		t.Errorf("prefix = %q, want INTR — lengthen from the repo's own letters first", got)
	}
}

func TestResolvePrefix_IsStableForAKnownRepo(t *testing.T) {
	// A repo that already has a prefix keeps it, whatever else exists —
	// recomputing could silently renumber tasks that are already written
	// down somewhere.
	taken := map[string]string{"deneme": "DEN", "denetim": "DENE"}
	if got := resolvePrefix("deneme", taken); got != "DEN" {
		t.Errorf("prefix = %q, want the stored DEN", got)
	}
}

func TestUpdate_SetsPriorityAndDueDate(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	critical, due := PriorityCritical, "2026-12-31"
	updated, err := store.Update("deneme", task.ID, Changes{Priority: &critical, DueDate: &due})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Priority != PriorityCritical || updated.DueDate != "2026-12-31" {
		t.Errorf("got %q / %q, want critical / 2026-12-31", updated.Priority, updated.DueDate)
	}

	// An empty string clears the date; nil would have left it alone.
	cleared := ""
	updated, err = store.Update("deneme", task.ID, Changes{DueDate: &cleared})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.DueDate != "" {
		t.Errorf("dueDate = %q, want it cleared", updated.DueDate)
	}
	if updated.Priority != PriorityCritical {
		t.Errorf("priority = %q, want it untouched by a due-date-only update", updated.Priority)
	}
}

func TestUpdate_RejectsABadPriorityOrDate(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	bogus := Priority("acil")
	if _, err := store.Update("deneme", task.ID, Changes{Priority: &bogus}); !errors.Is(err, ErrInvalidPriority) {
		t.Errorf("Update with a bad priority = %v, want ErrInvalidPriority", err)
	}

	// A date that parses as text but is not a real day must not slip
	// through and become a silently normalised 2026-03-03.
	for _, bad := range []string{"31.12.2026", "2026-13-01", "2026-02-31", "yarın"} {
		v := bad
		if _, err := store.Update("deneme", task.ID, Changes{DueDate: &v}); !errors.Is(err, ErrInvalidDueDate) {
			t.Errorf("Update with dueDate %q = %v, want ErrInvalidDueDate", bad, err)
		}
	}
}

func TestOverdue(t *testing.T) {
	cases := []struct {
		name  string
		task  Task
		today string
		want  bool
	}{
		{"tarihsiz görev gecikmez", Task{Status: StatusTodo}, "2026-09-10", false},
		{"gelecekteki tarih", Task{Status: StatusTodo, DueDate: "2026-09-20"}, "2026-09-10", false},
		{"bugün henüz gecikme değil", Task{Status: StatusTodo, DueDate: "2026-09-10"}, "2026-09-10", false},
		{"geçmiş tarih", Task{Status: StatusTodo, DueDate: "2026-09-01"}, "2026-09-10", true},
		// Finished work is never overdue — chasing something already
		// delivered is noise, and the board would show a permanent red
		// column of completed tasks.
		{"biten görev gecikmez", Task{Status: StatusDone, DueDate: "2026-09-01"}, "2026-09-10", false},
	}

	for _, tc := range cases {
		if got := tc.task.Overdue(tc.today); got != tc.want {
			t.Errorf("%s: Overdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Tasks written before Priority existed carry the old boolean. They must
// read back with a usable priority without anything on disk being
// rewritten.
func TestGet_UpgradesTheOldUrgentFlag(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	task, err := store.Create("deneme", "Eski görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Rewrite the file the way the previous version would have.
	path := filepath.Join(dir, "deneme", task.ID+".json")
	legacy := `{"id":"` + task.ID + `","repo":"deneme","title":"Eski görev",` +
		`"status":"in_progress","urgent":true,"createdAt":"2026-08-01T10:00:00Z"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("failed to write the legacy file: %v", err)
	}

	reread, err := store.Get("deneme", task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Priority != PriorityHigh {
		t.Errorf("priority = %q, want %q for a task saved with urgent=true", reread.Priority, PriorityHigh)
	}

	// And one saved without the flag becomes normal, not empty.
	plain := `{"id":"` + task.ID + `","repo":"deneme","title":"Eski görev",` +
		`"status":"todo","createdAt":"2026-08-01T10:00:00Z"}`
	if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
		t.Fatalf("failed to write the legacy file: %v", err)
	}
	reread, err = store.Get("deneme", task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Priority != PriorityNormal {
		t.Errorf("priority = %q, want %q", reread.Priority, PriorityNormal)
	}
}

// ------------------------------------------------------------ comments

func TestComments_StartEmptyAndAppendInOrder(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := store.Comments("deneme", task.ID)
	if err != nil {
		t.Fatalf("Comments failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a new task has %d comments, want 0", len(list))
	}

	if _, err := store.AddComment("deneme", task.ID, "dev-1", "  İlk yorum  "); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if _, err := store.AddComment("deneme", task.ID, "dev-2", "İkinci yorum"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	list, err = store.Comments("deneme", task.ID)
	if err != nil {
		t.Fatalf("Comments failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d comments, want 2", len(list))
	}
	// Oldest first: a conversation reads top to bottom.
	if list[0].Body != "İlk yorum" {
		t.Errorf("first comment = %q, want the oldest, trimmed", list[0].Body)
	}
	if list[1].Author != "dev-2" {
		t.Errorf("second author = %q, want dev-2", list[1].Author)
	}
}

func TestAddComment_RejectsEmptyAndOverlongBodies(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, blank := range []string{"", "   ", "\n\t "} {
		if _, err := store.AddComment("deneme", task.ID, "dev-1", blank); !errors.Is(err, ErrEmptyComment) {
			t.Errorf("AddComment(%q) = %v, want ErrEmptyComment", blank, err)
		}
	}

	// The cap counts runes: Turkish is multi-byte, and a byte limit would
	// silently allow half as much text.
	atLimit := strings.Repeat("ğ", MaxCommentLength)
	if _, err := store.AddComment("deneme", task.ID, "dev-1", atLimit); err != nil {
		t.Errorf("AddComment at exactly MaxCommentLength runes = %v, want nil", err)
	}
	over := strings.Repeat("ğ", MaxCommentLength+1)
	if _, err := store.AddComment("deneme", task.ID, "dev-1", over); !errors.Is(err, ErrCommentTooLong) {
		t.Errorf("AddComment past the cap = %v, want ErrCommentTooLong", err)
	}
}

// A comment on a task that does not exist would be a file nothing can
// ever reach.
func TestAddComment_RequiresTheTaskToExist(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.AddComment("deneme", "0123456789abcdef", "dev-1", "merhaba"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddComment on a missing task = %v, want ErrNotFound", err)
	}
}

func TestEditComment_OnlyByItsAuthorAndStamped(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	comment, err := store.AddComment("deneme", task.ID, "dev-1", "ilk hâli")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if comment.EditedAt != nil {
		t.Error("a fresh comment is already marked edited")
	}

	if _, err := store.EditComment("deneme", task.ID, comment.ID, "dev-2", "başkası yazdı"); !errors.Is(err, ErrCommentNotOurs) {
		t.Errorf("edit by another person = %v, want ErrCommentNotOurs", err)
	}

	edited, err := store.EditComment("deneme", task.ID, comment.ID, "dev-1", "düzeltilmiş hâli")
	if err != nil {
		t.Fatalf("EditComment failed: %v", err)
	}
	if edited.Body != "düzeltilmiş hâli" {
		t.Errorf("body = %q, want the new text", edited.Body)
	}
	// Stamped, so a conversation cannot be silently rewritten.
	if edited.EditedAt == nil {
		t.Error("editedAt is nil after an edit")
	}

	list, _ := store.Comments("deneme", task.ID)
	if len(list) != 1 || list[0].Body != "düzeltilmiş hâli" {
		t.Errorf("stored comments = %+v, want the edit persisted", list)
	}
}

func TestDeleteComment_AuthorOrAdminOnly(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	comment, err := store.AddComment("deneme", task.ID, "dev-1", "silinecek")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	if err := store.DeleteComment("deneme", task.ID, comment.ID, "dev-2", false); !errors.Is(err, ErrCommentNotOurs) {
		t.Errorf("delete by another developer = %v, want ErrCommentNotOurs", err)
	}
	// Still there after the refusal.
	if list, _ := store.Comments("deneme", task.ID); len(list) != 1 {
		t.Fatalf("comment was removed despite the refusal")
	}

	// allowAny is what the handler passes for an admin.
	if err := store.DeleteComment("deneme", task.ID, comment.ID, "dev-2", true); err != nil {
		t.Fatalf("admin delete failed: %v", err)
	}
	if list, _ := store.Comments("deneme", task.ID); len(list) != 0 {
		t.Errorf("comment survived an admin delete")
	}
}

// Deleting a task takes its conversation with it — otherwise the file
// lingers with nothing able to reach it.
func TestDelete_RemovesTheTasksComments(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.AddComment("deneme", task.ID, "dev-1", "bir şey"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	commentFile := filepath.Join(dir, "deneme", "comments", task.ID+".json")
	if _, err := os.Stat(commentFile); err != nil {
		t.Fatalf("comment file missing before delete: %v", err)
	}

	if err := store.Delete("deneme", task.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(commentFile); !os.IsNotExist(err) {
		t.Errorf("comment file survived the task: %v", err)
	}
}

// Comments live in a subdirectory precisely so List's *.json scan cannot
// pick them up and try to decode a comment array as a task.
func TestList_IsUnaffectedByComments(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.AddComment("deneme", task.ID, "dev-1", "yorum"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	tasks, err := store.List("deneme")
	if err != nil {
		t.Fatalf("List failed after a comment was added: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("List returned %d tasks, want 1", len(tasks))
	}
}

func TestComments_RejectsPathTraversal(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Comments("deneme", "../../etc/passwd"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("Comments with a traversal id = %v, want ErrInvalidID", err)
	}
	if _, err := store.AddComment("../escape", "0123456789abcdef", "dev-1", "x"); !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("AddComment with a traversal repo = %v, want ErrInvalidRepo", err)
	}
}

// ----------------------------------------------------------- subtasks

func TestSubtasks_AddTickRenameRemove(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Büyük iş", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if done, total := task.SubtaskProgress(); done != 0 || total != 0 {
		t.Fatalf("a new task has %d/%d subtasks, want 0/0", done, total)
	}

	task, err = store.AddSubtask("deneme", task.ID, "  Tasarımı çiz  ")
	if err != nil {
		t.Fatalf("AddSubtask failed: %v", err)
	}
	if len(task.Subtasks) != 1 || task.Subtasks[0].Title != "Tasarımı çiz" {
		t.Fatalf("subtasks = %+v, want one trimmed item", task.Subtasks)
	}
	if task.Subtasks[0].Done {
		t.Error("a new subtask starts ticked")
	}

	task, err = store.AddSubtask("deneme", task.ID, "Testleri yaz")
	if err != nil {
		t.Fatalf("AddSubtask failed: %v", err)
	}

	first := task.Subtasks[0].ID
	task, err = store.SetSubtaskDone("deneme", task.ID, first, true)
	if err != nil {
		t.Fatalf("SetSubtaskDone failed: %v", err)
	}
	if done, total := task.SubtaskProgress(); done != 1 || total != 2 {
		t.Errorf("progress = %d/%d, want 1/2", done, total)
	}

	task, err = store.RenameSubtask("deneme", task.ID, first, "Tasarımı bitir")
	if err != nil {
		t.Fatalf("RenameSubtask failed: %v", err)
	}
	if task.Subtasks[0].Title != "Tasarımı bitir" {
		t.Errorf("title = %q, want the new one", task.Subtasks[0].Title)
	}
	// Renaming must not untick it.
	if !task.Subtasks[0].Done {
		t.Error("renaming a subtask cleared its tick")
	}

	task, err = store.RemoveSubtask("deneme", task.ID, first)
	if err != nil {
		t.Fatalf("RemoveSubtask failed: %v", err)
	}
	if len(task.Subtasks) != 1 || task.Subtasks[0].Title != "Testleri yaz" {
		t.Errorf("subtasks = %+v, want only the second one", task.Subtasks)
	}

	// And it all survived the round trip to disk.
	reread, err := store.Get("deneme", task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !reflect.DeepEqual(reread.Subtasks, task.Subtasks) {
		t.Errorf("re-read subtasks = %+v, want %+v", reread.Subtasks, task.Subtasks)
	}
}

func TestSubtasks_RejectEmptyTitles(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, blank := range []string{"", "   "} {
		if _, err := store.AddSubtask("deneme", task.ID, blank); !errors.Is(err, ErrEmptySubtask) {
			t.Errorf("AddSubtask(%q) = %v, want ErrEmptySubtask", blank, err)
		}
	}

	task, err = store.AddSubtask("deneme", task.ID, "gerçek bir madde")
	if err != nil {
		t.Fatalf("AddSubtask failed: %v", err)
	}
	if _, err := store.RenameSubtask("deneme", task.ID, task.Subtasks[0].ID, "  "); !errors.Is(err, ErrEmptySubtask) {
		t.Errorf("RenameSubtask with a blank title = %v, want ErrEmptySubtask", err)
	}
}

// Past the cap the thing being described is a project, not a task.
func TestSubtasks_AreCapped(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < MaxSubtasks; i++ {
		if task, err = store.AddSubtask("deneme", task.ID, "madde"); err != nil {
			t.Fatalf("AddSubtask %d failed: %v", i, err)
		}
	}
	if _, err := store.AddSubtask("deneme", task.ID, "bir tane daha"); !errors.Is(err, ErrTooManySubtasks) {
		t.Errorf("AddSubtask past the cap = %v, want ErrTooManySubtasks", err)
	}
}

func TestSubtasks_UnknownItemIsNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := store.SetSubtaskDone("deneme", task.ID, "0123456789abcdef", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetSubtaskDone on a missing item = %v, want ErrNotFound", err)
	}
	if _, err := store.RemoveSubtask("deneme", task.ID, "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveSubtask on a missing item = %v, want ErrNotFound", err)
	}
}
