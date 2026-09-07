package gitemails

import (
	"errors"
	"os"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir() + "/git-emails.json")
}

func TestClaim_ThenClaimed_ReturnsTheAddress(t *testing.T) {
	s := newStore(t)

	if err := s.Claim("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	got, err := s.Claimed("dev-1")
	if err != nil {
		t.Fatalf("Claimed returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "dev@gmail.com" {
		t.Errorf("Claimed = %v, want [dev@gmail.com]", got)
	}
}

// Addresses are stored lowercased and trimmed because that is how
// ActivityByAuthors compares them — normalising once on the way in
// means every later comparison is a plain string match.
func TestClaim_NormalisesCaseAndSurroundingSpace(t *testing.T) {
	s := newStore(t)

	if err := s.Claim("dev-1", "  Dev@GMail.com  "); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	got, _ := s.Claimed("dev-1")
	if len(got) != 1 || got[0] != "dev@gmail.com" {
		t.Errorf("Claimed = %v, want [dev@gmail.com]", got)
	}
}

func TestClaim_IsIdempotent(t *testing.T) {
	s := newStore(t)

	for _, addr := range []string{"dev@gmail.com", "DEV@gmail.com", "dev@gmail.com"} {
		if err := s.Claim("dev-1", addr); err != nil {
			t.Fatalf("Claim(%q) returned error: %v", addr, err)
		}
	}

	got, _ := s.Claimed("dev-1")
	if len(got) != 1 {
		t.Errorf("Claimed = %v, want a single entry", got)
	}
}

func TestClaim_RejectsSomethingThatIsNotAnAddress(t *testing.T) {
	s := newStore(t)

	for _, bad := range []string{"", "   ", "not-an-email", "@nolocal.com", "no-domain@", "two @spaces.com"} {
		if err := s.Claim("dev-1", bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("Claim(%q) error = %v, want ErrInvalidEmail", bad, err)
		}
	}

	got, _ := s.Claimed("dev-1")
	if len(got) != 0 {
		t.Errorf("Claimed = %v, want empty", got)
	}
}

func TestClaim_RejectsEmptySubject(t *testing.T) {
	s := newStore(t)

	if err := s.Claim("", "dev@gmail.com"); !errors.Is(err, ErrInvalidSubject) {
		t.Errorf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestClaimed_KeepsEachPersonsAddressesSeparate(t *testing.T) {
	s := newStore(t)
	if err := s.Claim("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}
	if err := s.Claim("dev-2", "two@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	got, _ := s.Claimed("dev-1")
	if len(got) != 1 || got[0] != "one@gmail.com" {
		t.Errorf("dev-1's list = %v, want only their own address", got)
	}
}

func TestClaimed_UnknownSubjectIsEmptyNotAnError(t *testing.T) {
	s := newStore(t)

	got, err := s.Claimed("nobody")
	if err != nil {
		t.Fatalf("Claimed returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Claimed = %v, want empty", got)
	}
}

func TestUnclaim_DropsOnlyThatAddress(t *testing.T) {
	s := newStore(t)
	if err := s.Claim("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}
	if err := s.Claim("dev-1", "two@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	if err := s.Unclaim("dev-1", "ONE@gmail.com"); err != nil {
		t.Fatalf("Unclaim returned error: %v", err)
	}

	got, _ := s.Claimed("dev-1")
	if len(got) != 1 || got[0] != "two@gmail.com" {
		t.Errorf("Claimed = %v, want [two@gmail.com]", got)
	}
}

func TestUnclaim_UnknownAddressIsNotAnError(t *testing.T) {
	s := newStore(t)

	if err := s.Unclaim("dev-1", "nope@gmail.com"); err != nil {
		t.Errorf("Unclaim returned error: %v", err)
	}
}

// --- suggestions ------------------------------------------------------

// RecordSeen is what the git server calls during a push. Its whole point
// is that the person never types an address: the platform reports what
// it saw them push, and they confirm it.
func TestRecordSeen_ThenSuggestions_OffersTheAddress(t *testing.T) {
	s := newStore(t)

	if err := s.RecordSeen("dev-1", "Dev@GMail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	got, err := s.Suggestions("dev-1")
	if err != nil {
		t.Fatalf("Suggestions returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "dev@gmail.com" {
		t.Errorf("Suggestions = %v, want [dev@gmail.com] (normalised)", got)
	}
}

func TestRecordSeen_IsIdempotent(t *testing.T) {
	s := newStore(t)

	for i := 0; i < 3; i++ {
		if err := s.RecordSeen("dev-1", "dev@gmail.com"); err != nil {
			t.Fatalf("RecordSeen returned error: %v", err)
		}
	}

	got, _ := s.Suggestions("dev-1")
	if len(got) != 1 {
		t.Errorf("Suggestions = %v, want one entry — a push repeats the same address constantly", got)
	}
}

// Claiming a suggestion must take it out of the suggestion list, or the
// panel would keep asking about an address the person already confirmed.
func TestClaim_RemovesTheAddressFromSuggestions(t *testing.T) {
	s := newStore(t)
	if err := s.RecordSeen("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	if err := s.Claim("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	suggestions, _ := s.Suggestions("dev-1")
	if len(suggestions) != 0 {
		t.Errorf("Suggestions = %v, want empty after claiming", suggestions)
	}
	claimed, _ := s.Claimed("dev-1")
	if len(claimed) != 1 {
		t.Errorf("Claimed = %v, want the address to have moved here", claimed)
	}
}

// "Not me" has to stick: without it the next push would surface the same
// address again and the prompt would nag forever.
func TestDismiss_RemovesFromSuggestionsAndSurvivesAnotherPush(t *testing.T) {
	s := newStore(t)
	if err := s.RecordSeen("dev-1", "someone-else@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	if err := s.Dismiss("dev-1", "someone-else@gmail.com"); err != nil {
		t.Fatalf("Dismiss returned error: %v", err)
	}
	if got, _ := s.Suggestions("dev-1"); len(got) != 0 {
		t.Fatalf("Suggestions = %v, want empty after dismissing", got)
	}

	if err := s.RecordSeen("dev-1", "someone-else@gmail.com"); err != nil {
		t.Fatalf("second RecordSeen returned error: %v", err)
	}
	if got, _ := s.Suggestions("dev-1"); len(got) != 0 {
		t.Errorf("Suggestions = %v, want still empty — a dismissed address must not come back", got)
	}
}

// An address the person already claimed must not also be offered as a
// suggestion when they push again.
func TestRecordSeen_IgnoresAnAlreadyClaimedAddress(t *testing.T) {
	s := newStore(t)
	if err := s.Claim("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}

	if err := s.RecordSeen("dev-1", "dev@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	if got, _ := s.Suggestions("dev-1"); len(got) != 0 {
		t.Errorf("Suggestions = %v, want empty", got)
	}
}

func TestRecordSeen_KeepsEachPersonsSuggestionsSeparate(t *testing.T) {
	s := newStore(t)
	if err := s.RecordSeen("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}
	if err := s.RecordSeen("dev-2", "two@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	got, _ := s.Suggestions("dev-1")
	if len(got) != 1 || got[0] != "one@gmail.com" {
		t.Errorf("dev-1's suggestions = %v, want only what they pushed", got)
	}
}

// Junk must not reach the suggestion list either — a malformed author
// line in a commit shouldn't turn into a prompt.
func TestRecordSeen_IgnoresSomethingThatIsNotAnAddress(t *testing.T) {
	s := newStore(t)

	if err := s.RecordSeen("dev-1", "not-an-email"); err != nil {
		t.Errorf("RecordSeen returned error: %v — junk should be ignored, not fail the push", err)
	}
	if got, _ := s.Suggestions("dev-1"); len(got) != 0 {
		t.Errorf("Suggestions = %v, want empty", got)
	}
}

// RecordSeen runs on the push path, where a failure would reject
// someone's push. A nil Store must therefore be a silent no-op.
func TestRecordSeen_OnANilStoreDoesNothing(t *testing.T) {
	var s *Store

	if err := s.RecordSeen("dev-1", "dev@gmail.com"); err != nil {
		t.Errorf("RecordSeen on a nil Store returned error: %v", err)
	}
}

func TestClaimed_OnANilStoreIsEmpty(t *testing.T) {
	var s *Store

	got, err := s.Claimed("dev-1")
	if err != nil {
		t.Fatalf("Claimed on a nil Store returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Claimed = %v, want empty", got)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-emails.json"
	if err := NewStore(path).Claim("dev-1", "one@gmail.com"); err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}
	if err := NewStore(path).RecordSeen("dev-1", "two@gmail.com"); err != nil {
		t.Fatalf("RecordSeen returned error: %v", err)
	}

	claimed, err := NewStore(path).Claimed("dev-1")
	if err != nil {
		t.Fatalf("Claimed returned error: %v", err)
	}
	if len(claimed) != 1 || claimed[0] != "one@gmail.com" {
		t.Errorf("Claimed from a fresh Store = %v, want [one@gmail.com]", claimed)
	}
	suggestions, _ := NewStore(path).Suggestions("dev-1")
	if len(suggestions) != 1 || suggestions[0] != "two@gmail.com" {
		t.Errorf("Suggestions from a fresh Store = %v, want [two@gmail.com]", suggestions)
	}
}

// The first shipped version of this file was a bare subject→addresses
// map, before suggestions existed. Reading one must not lose those
// claims — the same backward-compatibility trap that broke git tokens
// in production on 2026-09-03 (see internal/gittoken.Store.load).
func TestLoad_UpgradesTheOriginalFlatFileFormat(t *testing.T) {
	path := t.TempDir() + "/git-emails.json"
	if err := os.WriteFile(path, []byte(`{"dev-1":["old@gmail.com"]}`), 0o600); err != nil {
		t.Fatalf("failed to write legacy fixture: %v", err)
	}

	got, err := NewStore(path).Claimed("dev-1")
	if err != nil {
		t.Fatalf("Claimed returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "old@gmail.com" {
		t.Errorf("Claimed = %v, want [old@gmail.com] carried over from the old format", got)
	}
}
