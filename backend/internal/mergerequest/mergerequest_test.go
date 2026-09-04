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

func TestSetStatus_TransitionsToRejectedWithANote(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.SetStatus("intranet-backend", created.ID, StatusRejected, "şunu düzelt, tekrar aç")
	if err != nil {
		t.Fatalf("SetStatus returned error: %v", err)
	}
	if updated.Status != StatusRejected {
		t.Errorf("Status = %q, want %q", updated.Status, StatusRejected)
	}
	if updated.Note != "şunu düzelt, tekrar aç" {
		t.Errorf("Note = %q, want %q", updated.Note, "şunu düzelt, tekrar aç")
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after SetStatus failed: %v", err)
	}
	if reread.Status != StatusRejected || reread.Note != "şunu düzelt, tekrar aç" {
		t.Errorf("persisted = %+v, want Status=rejected Note=\"şunu düzelt, tekrar aç\"", reread)
	}
}

func TestSetStatus_RejectsAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.SetStatus("intranet-backend", created.ID, StatusRejected, ""); err != nil {
		t.Fatalf("first SetStatus failed: %v", err)
	}

	_, err = store.SetStatus("intranet-backend", created.ID, StatusRejected, "")
	if err != ErrNotOpen {
		t.Fatalf("err = %v, want ErrNotOpen", err)
	}
}

// TestSetStatus_TransitionsToApproved covers what Handlers.Approve now
// does: a pure status+note record, no git operation of any kind (see
// that handler's doc comment for why — main only ever advances via an
// Admin's own direct push, per gitserver.WithAdmin).
func TestSetStatus_TransitionsToApproved(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.SetStatus("intranet-backend", created.ID, StatusApproved, "")
	if err != nil {
		t.Fatalf("SetStatus returned error: %v", err)
	}
	if updated.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", updated.Status, StatusApproved)
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after SetStatus failed: %v", err)
	}
	if reread.Status != StatusApproved {
		t.Errorf("persisted Status = %q, want %q", reread.Status, StatusApproved)
	}
}

func TestSetStatus_RejectsApprovingAnAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.SetStatus("intranet-backend", created.ID, StatusApproved, ""); err != nil {
		t.Fatalf("first SetStatus failed: %v", err)
	}

	_, err = store.SetStatus("intranet-backend", created.ID, StatusApproved, "")
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

	_, err = store.SetStatus("intranet-backend", created.ID, StatusOpen, "")
	if err != ErrInvalidStatus {
		t.Fatalf("err = %v, want ErrInvalidStatus", err)
	}
}

// TestCreate_AllowsANewRequestForTheSameBranchPairAfterRejection is the
// mechanism that replaces "re-opening" a rejected request (see
// Handlers.Reject's doc comment): there's no branch-pair uniqueness
// constraint, so a developer who fixes what a rejection note asked for
// just opens a fresh request on the same branches.
func TestCreate_AllowsANewRequestForTheSameBranchPairAfterRejection(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Create("intranet-backend", "Fix login bug", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	if _, err := store.SetStatus("intranet-backend", first.ID, StatusRejected, "eksik test var"); err != nil {
		t.Fatalf("SetStatus(rejected) failed: %v", err)
	}

	second, err := store.Create("intranet-backend", "Fix login bug (v2)", "feature-x", "main", "dev-1")
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("second Create returned the same ID as the first")
	}
	if second.Status != StatusOpen {
		t.Errorf("second.Status = %q, want %q", second.Status, StatusOpen)
	}

	all, err := store.List("intranet-backend")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d requests, want 2 (the rejected one plus the new one)", len(all))
	}
}
