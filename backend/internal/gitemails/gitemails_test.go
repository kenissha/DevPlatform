package gitemails

import (
	"errors"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir() + "/git-emails.json")
}

func TestAdd_ThenList_ReturnsTheAddress(t *testing.T) {
	s := newStore(t)

	if err := s.Add("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	got, err := s.List("dev-1")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "dev@gmail.com" {
		t.Errorf("List = %v, want [dev@gmail.com]", got)
	}
}

// Addresses are stored lowercased and trimmed because that is how
// ActivityByAuthors compares them — normalising once on the way in
// means every later comparison is a plain string match.
func TestAdd_NormalisesCaseAndSurroundingSpace(t *testing.T) {
	s := newStore(t)

	if err := s.Add("dev-1", "  Dev@GMail.com  "); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	got, _ := s.List("dev-1")
	if len(got) != 1 || got[0] != "dev@gmail.com" {
		t.Errorf("List = %v, want [dev@gmail.com]", got)
	}
}

func TestAdd_IsIdempotent(t *testing.T) {
	s := newStore(t)

	for _, addr := range []string{"dev@gmail.com", "DEV@gmail.com", "dev@gmail.com"} {
		if err := s.Add("dev-1", addr); err != nil {
			t.Fatalf("Add(%q) returned error: %v", addr, err)
		}
	}

	got, _ := s.List("dev-1")
	if len(got) != 1 {
		t.Errorf("List = %v, want a single entry — the same address must not stack up", got)
	}
}

func TestAdd_RejectsSomethingThatIsNotAnAddress(t *testing.T) {
	s := newStore(t)

	for _, bad := range []string{"", "   ", "not-an-email", "@nolocal.com", "no-domain@", "two @spaces.com"} {
		if err := s.Add("dev-1", bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("Add(%q) error = %v, want ErrInvalidEmail", bad, err)
		}
	}

	got, _ := s.List("dev-1")
	if len(got) != 0 {
		t.Errorf("List = %v, want empty — no invalid address should have been stored", got)
	}
}

func TestAdd_RejectsEmptySubject(t *testing.T) {
	s := newStore(t)

	if err := s.Add("", "dev@gmail.com"); !errors.Is(err, ErrInvalidSubject) {
		t.Errorf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestList_KeepsEachPersonsAddressesSeparate(t *testing.T) {
	s := newStore(t)
	if err := s.Add("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := s.Add("dev-2", "two@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	got, _ := s.List("dev-1")
	if len(got) != 1 || got[0] != "one@gmail.com" {
		t.Errorf("dev-1's list = %v, want only their own address", got)
	}
}

func TestList_UnknownSubjectIsEmptyNotAnError(t *testing.T) {
	s := newStore(t)

	got, err := s.List("nobody")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestRemove_DropsOnlyThatAddress(t *testing.T) {
	s := newStore(t)
	if err := s.Add("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := s.Add("dev-1", "two@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	// Removal normalises the same way Add does, so the case someone
	// types it back in doesn't matter.
	if err := s.Remove("dev-1", "ONE@gmail.com"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	got, _ := s.List("dev-1")
	if len(got) != 1 || got[0] != "two@gmail.com" {
		t.Errorf("List = %v, want [two@gmail.com]", got)
	}
}

// Removing something that isn't there is not an error — the caller's
// intent ("this address should not be listed") is already satisfied,
// the same convention access.Store.Clear uses.
func TestRemove_UnknownAddressIsNotAnError(t *testing.T) {
	s := newStore(t)
	if err := s.Add("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	if err := s.Remove("dev-1", "nope@gmail.com"); err != nil {
		t.Errorf("Remove returned error: %v", err)
	}
	if err := s.Remove("nobody", "nope@gmail.com"); err != nil {
		t.Errorf("Remove for an unknown subject returned error: %v", err)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-emails.json"
	if err := NewStore(path).Add("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	got, err := NewStore(path).List("dev-1")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "one@gmail.com" {
		t.Errorf("List from a fresh Store = %v, want [one@gmail.com]", got)
	}
}

// A nil Store means "nobody registered anything" rather than a panic,
// matching displaynames.Store — it keeps the contributions endpoint
// working on a deployment that never wired this up.
func TestList_OnANilStoreIsEmpty(t *testing.T) {
	var s *Store

	got, err := s.List("dev-1")
	if err != nil {
		t.Fatalf("List on a nil Store returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}
