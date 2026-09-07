package repodesc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "repo-descriptions.json"))
}

func TestGet_ReturnsEmptyForAnUnknownRepo(t *testing.T) {
	s := newTestStore(t)
	if got := s.Get("deneme"); got != "" {
		t.Errorf("Get on an empty store = %q, want %q", got, "")
	}
}

func TestSetThenGet(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("deneme", "Hakem raporları için deneme deposu"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got := s.Get("deneme"); got != "Hakem raporları için deneme deposu" {
		t.Errorf("Get = %q, want the description just set", got)
	}
}

func TestSet_TrimsSurroundingWhitespace(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("deneme", "  boşluklu  "); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got := s.Get("deneme"); got != "boşluklu" {
		t.Errorf("Get = %q, want the trimmed text", got)
	}
}

// An empty description has to delete the entry rather than store "",
// otherwise "cleared" and "never set" become two states on disk that both
// render as no description — and List would hand the frontend a map full
// of blanks to filter out.
func TestSet_EmptyClearsTheEntry(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("deneme", "bir açıklama"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := s.Set("deneme", "   "); err != nil {
		t.Fatalf("clearing Set returned error: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if _, present := all["deneme"]; present {
		t.Errorf("List still contains the cleared repo: %v", all)
	}
}

func TestSet_RejectsAnEmptyRepoName(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("", "açıklama"); !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("Set with an empty repo = %v, want ErrInvalidRepo", err)
	}
}

// The cap counts runes, not bytes: Turkish text is multi-byte in UTF-8,
// so a byte-based limit would silently allow roughly half as many
// characters for the people this platform is actually for.
func TestSet_RejectsATooLongDescriptionByRunesNotBytes(t *testing.T) {
	s := newTestStore(t)

	atLimit := strings.Repeat("ğ", MaxLength)
	if err := s.Set("deneme", atLimit); err != nil {
		t.Errorf("Set at exactly MaxLength runes returned %v, want nil (%d bytes)", err, len(atLimit))
	}

	overLimit := strings.Repeat("ğ", MaxLength+1)
	if err := s.Set("deneme", overLimit); !errors.Is(err, ErrTooLong) {
		t.Errorf("Set past MaxLength = %v, want ErrTooLong", err)
	}
}

func TestList_ReturnsEveryDescribedRepo(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("bir", "birinci"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := s.Set("iki", "ikinci"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(all) != 2 || all["bir"] != "birinci" || all["iki"] != "ikinci" {
		t.Errorf("List = %v, want both descriptions", all)
	}
}

// A nil Store is the "not configured" case internal/server passes when no
// descriptions file is set up. It must read as "no descriptions", never
// panic — the same posture displaynames.Store takes.
func TestNilStore_BehavesAsUnconfigured(t *testing.T) {
	var s *Store
	if got := s.Get("deneme"); got != "" {
		t.Errorf("nil Store Get = %q, want %q", got, "")
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("nil Store List returned error: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("nil Store List = %v, want empty", all)
	}
	// Writing has to report rather than panic: repoapi.Create calls Set on
	// whatever Store it was handed and only logs the outcome, so a panic
	// here would take down a repo creation that had already succeeded.
	if err := s.Set("deneme", "açıklama"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil Store Set = %v, want ErrNotConfigured", err)
	}
}

func TestSurvivesAnEmptyFileOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo-descriptions.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("failed to write the empty file: %v", err)
	}
	s := NewStore(path)

	if got := s.Get("deneme"); got != "" {
		t.Errorf("Get against an empty file = %q, want %q", got, "")
	}
	if err := s.Set("deneme", "açıklama"); err != nil {
		t.Errorf("Set against an empty file returned %v, want nil", err)
	}
}
