package access

import (
	"path/filepath"
	"testing"
)

func TestAllowed_UnrestrictedByDefault(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	repos, restricted, err := store.Allowed("dev-1")
	if err != nil {
		t.Fatalf("Allowed returned error: %v", err)
	}
	if restricted {
		t.Error("expected an unconfigured subject to be unrestricted")
	}
	if len(repos) != 0 {
		t.Errorf("repos = %v, want none for an unconfigured subject", repos)
	}
}

func TestCanAccess_UnrestrictedSubjectCanSeeAnyRepo(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	ok, err := store.CanAccess("dev-1", "intranet-backend")
	if err != nil {
		t.Fatalf("CanAccess returned error: %v", err)
	}
	if !ok {
		t.Error("expected an unrestricted subject to access any repo")
	}
}

func TestCanAccess_NilStoreIsUnrestricted(t *testing.T) {
	var store *Store

	ok, err := store.CanAccess("dev-1", "intranet-backend")
	if err != nil {
		t.Fatalf("CanAccess returned error: %v", err)
	}
	if !ok {
		t.Error("expected a nil Store to behave as unrestricted")
	}
}

func TestSet_RestrictsSubjectToExactlyTheGivenRepos(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	allowed, err := store.CanAccess("dev-1", "intranet-backend")
	if err != nil || !allowed {
		t.Errorf("CanAccess(intranet-backend) = %v, %v, want true, nil", allowed, err)
	}

	denied, err := store.CanAccess("dev-1", "intranet-frontend")
	if err != nil || denied {
		t.Errorf("CanAccess(intranet-frontend) = %v, %v, want false, nil", denied, err)
	}
}

func TestSet_EmptyRepoListRestrictsToNothing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	if err := store.Set("dev-1", []string{}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	ok, err := store.CanAccess("dev-1", "intranet-backend")
	if err != nil {
		t.Fatalf("CanAccess returned error: %v", err)
	}
	if ok {
		t.Error("expected an empty allow-list to deny every repo")
	}

	_, restricted, err := store.Allowed("dev-1")
	if err != nil {
		t.Fatalf("Allowed returned error: %v", err)
	}
	if !restricted {
		t.Error("expected Set with an empty slice to still count as restricted")
	}
}

func TestSet_RejectsEmptySubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	err := store.Set("", []string{"intranet-backend"})
	if err != ErrInvalidSubject {
		t.Fatalf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestClear_ReturnsSubjectToUnrestricted(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Clear("dev-1"); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}

	ok, err := store.CanAccess("dev-1", "intranet-frontend")
	if err != nil {
		t.Fatalf("CanAccess returned error: %v", err)
	}
	if !ok {
		t.Error("expected Clear to remove the restriction entirely")
	}
}

func TestClear_OnAnUnrestrictedSubjectIsANoop(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	if err := store.Clear("never-restricted"); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}
}

func TestList_OnlyIncludesRestrictedSubjects(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	registry, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(registry) != 1 {
		t.Fatalf("registry = %v, want exactly 1 entry", registry)
	}
	if got := registry["dev-1"]; len(got) != 1 || got[0] != "intranet-backend" {
		t.Errorf("registry[dev-1] = %v, want [intranet-backend]", got)
	}
	if _, ok := registry["dev-2"]; ok {
		t.Error("expected an unrestricted subject to be absent from List")
	}
}

func TestList_OnNilStoreReturnsEmptyMap(t *testing.T) {
	var store *Store

	registry, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(registry) != 0 {
		t.Errorf("registry = %v, want empty", registry)
	}
}

func TestFilterRepos_ReturnsEveryRepoForAnUnrestrictedSubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))

	got, err := store.FilterRepos("dev-1", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("FilterRepos returned error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %v, want all 3 repos", got)
	}
}

func TestFilterRepos_NarrowsToTheAllowListForARestrictedSubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "access.json"))
	if err := store.Set("dev-1", []string{"b"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got, err := store.FilterRepos("dev-1", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("FilterRepos returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("got %v, want [b]", got)
	}
}

func TestFilterRepos_OnNilStoreReturnsEveryRepoUnmodified(t *testing.T) {
	var store *Store

	got, err := store.FilterRepos("dev-1", []string{"a", "b"})
	if err != nil {
		t.Fatalf("FilterRepos returned error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want both repos", got)
	}
}

func TestSet_PersistsAcrossStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")

	first := NewStore(path)
	if err := first.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	second := NewStore(path)
	ok, err := second.CanAccess("dev-1", "intranet-backend")
	if err != nil {
		t.Fatalf("CanAccess returned error: %v", err)
	}
	if !ok {
		t.Error("expected a restriction written by one Store instance to be visible from another")
	}
}
