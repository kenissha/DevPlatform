package displaynames

import (
	"path/filepath"
	"testing"
)

func TestGet_ReturnsFallbackWhenNoOverrideIsSet(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestGet_NilStoreReturnsFallback(t *testing.T) {
	var store *Store

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get on nil store = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestSet_ThenGet_ReturnsTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "Rifat Öztürk" {
		t.Errorf("Get = %q, want %q", got, "Rifat Öztürk")
	}
}

func TestSet_RejectsEmptySubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Set("", "Rifat Öztürk"); err != ErrInvalidSubject {
		t.Errorf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestClear_RemovesTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if err := store.Clear("dev-1"); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get after Clear = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestClear_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Clear("dev-1"); err != nil {
		t.Errorf("Clear on a subject with no override returned error: %v", err)
	}
}

func TestList_ReturnsEveryOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set("dev-2", "Ayşe Yılmaz"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	registry, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(registry) != 2 || registry["dev-1"] != "Rifat Öztürk" || registry["dev-2"] != "Ayşe Yılmaz" {
		t.Errorf("List = %v, want both overrides", registry)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "display-names.json")
	store1 := NewStore(path)
	if err := store1.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	store2 := NewStore(path)
	got := store2.Get("dev-1", "dev-1@example.com")
	if got != "Rifat Öztürk" {
		t.Errorf("a fresh Store instance backed by the same file: Get = %q, want %q", got, "Rifat Öztürk")
	}
}
