package mergerequest

import (
	"testing"
)

func TestCreate_PersistsAndReturnsOpenRequest(t *testing.T) {
	store := NewStore(t.TempDir())

	mr, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if mr.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if mr.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", mr.Status, StatusOpen)
	}
	if mr.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreate_RejectsInvalidRepoName(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Create("../escape", "title", "a", "b", "dev-1")
	if err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestGet_ReturnsCreatedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
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

func TestNextCreatedAt_IsStrictlyMonotonicEvenUnderClockTies(t *testing.T) {
	store := NewStore(t.TempDir())

	prev := store.nextCreatedAt()
	for i := 0; i < 1000; i++ {
		next := store.nextCreatedAt()
		if !next.After(prev) {
			t.Fatalf("nextCreatedAt() call %d = %v, want strictly after %v", i, next, prev)
		}
		prev = next
	}
}

func TestList_ReturnsAllRequestsNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Create("intranet-backend", "First", "a", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("intranet-backend", "Second", "b", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	mrs, err := store.List("intranet-backend")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(mrs) != 2 {
		t.Fatalf("got %d merge requests, want 2", len(mrs))
	}
	if mrs[0].ID != second.ID || mrs[1].ID != first.ID {
		t.Errorf("expected newest-first order [%s, %s], got [%s, %s]",
			second.ID, first.ID, mrs[0].ID, mrs[1].ID)
	}
}

func TestList_ReturnsEmptySliceForRepoWithNoRequests(t *testing.T) {
	store := NewStore(t.TempDir())

	mrs, err := store.List("never-touched")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(mrs) != 0 {
		t.Errorf("got %d merge requests, want 0", len(mrs))
	}
}

func TestSetStatus_TransitionsToRejected(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.SetStatus("intranet-backend", created.ID, StatusRejected)
	if err != nil {
		t.Fatalf("SetStatus returned error: %v", err)
	}
	if updated.Status != StatusRejected {
		t.Errorf("Status = %q, want %q", updated.Status, StatusRejected)
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after SetStatus failed: %v", err)
	}
	if reread.Status != StatusRejected {
		t.Errorf("persisted Status = %q, want %q", reread.Status, StatusRejected)
	}
}

func TestSetStatus_RejectsAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.SetStatus("intranet-backend", created.ID, StatusRejected); err != nil {
		t.Fatalf("first SetStatus failed: %v", err)
	}

	_, err = store.SetStatus("intranet-backend", created.ID, StatusRejected)
	if err != ErrNotOpen {
		t.Fatalf("err = %v, want ErrNotOpen", err)
	}
}

func TestMarkApproved_TransitionsAndRecordsMergedCommit(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.MarkApproved("intranet-backend", created.ID, "deadbeef")
	if err != nil {
		t.Fatalf("MarkApproved returned error: %v", err)
	}
	if updated.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", updated.Status, StatusApproved)
	}
	if updated.MergedCommit != "deadbeef" {
		t.Errorf("MergedCommit = %q, want %q", updated.MergedCommit, "deadbeef")
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after MarkApproved failed: %v", err)
	}
	if reread.Status != StatusApproved || reread.MergedCommit != "deadbeef" {
		t.Errorf("persisted = %+v, want Status=approved MergedCommit=deadbeef", reread)
	}
}

func TestMarkApproved_RejectsAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.MarkApproved("intranet-backend", created.ID, "deadbeef"); err != nil {
		t.Fatalf("first MarkApproved failed: %v", err)
	}

	_, err = store.MarkApproved("intranet-backend", created.ID, "cafebabe")
	if err != ErrNotOpen {
		t.Fatalf("err = %v, want ErrNotOpen", err)
	}
}

func TestSetStatus_RejectsInvalidTargetStatus(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = store.SetStatus("intranet-backend", created.ID, StatusOpen)
	if err != ErrInvalidStatus {
		t.Fatalf("err = %v, want ErrInvalidStatus", err)
	}
}
