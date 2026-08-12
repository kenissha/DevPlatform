package deployment

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreate_OmitsDecidedAtFromJSONWhilePending(t *testing.T) {
	// Regression test: DecidedAt used to be a bare time.Time, and
	// encoding/json's omitempty never treats a struct type as "empty"
	// regardless of its value — so a pending request's zero DecidedAt
	// serialized as "0001-01-01T00:00:00Z" and the frontend's `d.decidedAt
	// && ...` check rendered a nonsense "karar: 01 Oca 1" line for every
	// request that had never been decided.
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if strings.Contains(string(encoded), "decidedAt") {
		t.Errorf("encoded pending request contains \"decidedAt\": %s", encoded)
	}
}

func TestCreate_PersistsAndReturnsPendingRequest(t *testing.T) {
	store := NewStore(t.TempDir())

	req, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if req.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if req.Status != StatusPending {
		t.Errorf("Status = %q, want %q", req.Status, StatusPending)
	}
	if req.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGet_ReturnsCreatedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "dev-1")
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

func TestList_ReturnsNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Create("intranet-backend", "test", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := store.List("intranet-backend")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d requests, want 2", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("expected newest-first order [%s, %s], got [%s, %s]",
			second.ID, first.ID, list[0].ID, list[1].ID)
	}
}

func TestDecide_TransitionsToDeployedAndRecordsReleaseDir(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.Decide("intranet-backend", created.ID, StatusDeployed, "/releases/123", "")
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if updated.Status != StatusDeployed || updated.ReleaseDir != "/releases/123" {
		t.Errorf("updated = %+v, unexpected fields", updated)
	}
	if updated.DecidedAt.IsZero() {
		t.Error("expected DecidedAt to be set")
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after Decide failed: %v", err)
	}
	if reread.Status != StatusDeployed || reread.ReleaseDir != "/releases/123" {
		t.Errorf("persisted = %+v, unexpected fields", reread)
	}
}

func TestDecide_RecordsFailureReason(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.Decide("intranet-backend", created.ID, StatusFailed, "", "build failed: exit status 1")
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if updated.Status != StatusFailed || updated.FailureReason != "build failed: exit status 1" {
		t.Errorf("updated = %+v, unexpected fields", updated)
	}
}

func TestDecide_RejectsAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.Decide("intranet-backend", created.ID, StatusRejected, "", ""); err != nil {
		t.Fatalf("first Decide failed: %v", err)
	}

	_, err = store.Decide("intranet-backend", created.ID, StatusDeployed, "/x", "")
	if err != ErrNotPending {
		t.Fatalf("err = %v, want ErrNotPending", err)
	}
}
