package secretsvault

import (
	"testing"
)

func TestPutGet_RoundTrip(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	original := []byte(`{"connectionString": "test-only-value"}`)
	if err := store.Put("sample", "test", original); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	got, err := store.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("Get = %q, want %q", got, original)
	}
}

func TestGet_ReturnsErrNotFoundWhenMissing(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if _, err := store.Get("sample", "test"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPut_OverwritesExisting(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	store.Put("sample", "test", []byte("first version"))
	if err := store.Put("sample", "test", []byte("second version")); err != nil {
		t.Fatalf("second Put returned error: %v", err)
	}

	got, err := store.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "second version" {
		t.Errorf("Get = %q, want %q", got, "second version")
	}
}

func TestPut_RejectsPathTraversalRepo(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if err := store.Put("../escape", "test", []byte("x")); err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestPut_RejectsPathTraversalEnvironment(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if err := store.Put("sample", "../escape", []byte("x")); err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestDifferentRepoEnvironmentPairs_DontCollide(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	store.Put("repo-a", "test", []byte("a-test"))
	store.Put("repo-a", "production", []byte("a-prod"))
	store.Put("repo-b", "test", []byte("b-test"))

	got, err := store.Get("repo-a", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "a-test" {
		t.Errorf("Get(repo-a, test) = %q, want %q", got, "a-test")
	}
}
