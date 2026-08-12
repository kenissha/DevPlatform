package users

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestUpsert_CreatesThenRefreshesWithoutDuplicating(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	created, err := store.Upsert("dev-1", "dev-1@example.com", "developer")
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	if created.FirstSeen.IsZero() || created.LastSeen.IsZero() {
		t.Errorf("expected FirstSeen and LastSeen to be stamped: %+v", created)
	}

	// A role change in the external identity system must be picked up on
	// the next request rather than needing a sync.
	updated, err := store.Upsert("dev-1", "dev-1@example.com", "admin")
	if err != nil {
		t.Fatalf("second Upsert returned error: %v", err)
	}
	if updated.Role != "admin" {
		t.Errorf("Role = %q, want %q", updated.Role, "admin")
	}
	if !updated.FirstSeen.Equal(created.FirstSeen) {
		t.Errorf("FirstSeen changed on re-upsert: %v → %v", created.FirstSeen, updated.FirstSeen)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d users, want 1 — re-upsert must not duplicate", len(list))
	}
}

func TestGet_ReturnsKnownUser(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))
	if _, err := store.Upsert("dev-1", "dev-1@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	got, ok, err := store.Get("dev-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a known subject")
	}
	if got.Email != "dev-1@example.com" {
		t.Errorf("Email = %q, want %q", got.Email, "dev-1@example.com")
	}
}

func TestGet_ReturnsFalseForUnknownSubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	_, ok, err := store.Get("nobody")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for an unknown subject")
	}
}

func TestUpsert_RejectsEmptySubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	_, err := store.Upsert("", "x@example.com", "developer")
	if err != ErrInvalidSubject {
		t.Fatalf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestList_EmptyBeforeAnyoneLogsIn(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	list, err := store.List()
	if err != nil {
		t.Fatalf("List on a missing file returned error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d users, want 0", len(list))
	}
}

func TestList_ReturnsMostRecentlySeenFirst(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	if _, err := store.Upsert("dev-1", "dev-1@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	if _, err := store.Upsert("dev-2", "dev-2@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	// Touch dev-1 again so it becomes the most recently seen.
	if _, err := store.Upsert("dev-1", "dev-1@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d users, want 2", len(list))
	}
	if list[0].Subject != "dev-1" {
		t.Errorf("first = %q, want dev-1 (most recently seen)", list[0].Subject)
	}
}

func TestUpsert_PersistsAcrossStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	if _, err := NewStore(path).Upsert("dev-1", "dev-1@example.com", "developer"); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	list, err := NewStore(path).List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 || list[0].Subject != "dev-1" {
		t.Fatalf("list = %+v, want a single dev-1 entry", list)
	}
}

func TestUpsert_IsSafeUnderConcurrentCallers(t *testing.T) {
	// Every request that passes auth upserts, so concurrent writes are the
	// normal case, not an edge one. A lost update here would drop a
	// colleague out of the assignee picker.
	store := NewStore(filepath.Join(t.TempDir(), "users.json"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			subject := string(rune('a' + n))
			if _, err := store.Upsert(subject, subject+"@example.com", "developer"); err != nil {
				t.Errorf("Upsert returned error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 10 {
		t.Fatalf("got %d users, want 10 — a concurrent upsert was lost", len(list))
	}
}
