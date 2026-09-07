// Package gitemails records which git author addresses belong to whom.
//
// A git commit carries no account — only whatever `git config user.email`
// was set to on the machine that made it, written into the commit like a
// signature and never verified. That address is very often not the one
// the platform knows someone by (theirs arrives from the SSO JWT, see
// internal/auth), so without a mapping the panel's contribution graph
// silently shows nothing for them. This is the same problem GitHub
// solves by letting an account list several emails.
//
// Rather than asking people to type addresses in, the git server reports
// what it saw them push (RecordSeen, called from internal/gitserver on an
// authenticated push) and the person confirms with one click. Confirming
// is deliberately not automatic: a push routinely carries commits
// authored by someone else — merging a colleague's branch into main is
// the normal case here, not the exception — and nothing in the push
// itself distinguishes their signature from yours. Only the person can.
//
// Not a security boundary: an entry only widens which commits someone
// sees counted in their own graph. It grants no access and reveals
// nothing new — who commits, and when, is already visible to any
// authenticated caller through the contributors and audit views — so
// addresses are taken at their word rather than verified by a
// round-trip email.
package gitemails

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidSubject = errors.New("gitemails: subject must not be empty")
	ErrInvalidEmail   = errors.New("gitemails: not a valid email address")
)

// registry is the on-disk shape: three per-subject lists.
//
//   - claimed:   confirmed as this person's own; counted in their graph.
//   - seen:      observed on a push by them, awaiting confirmation.
//   - dismissed: answered "not me"; never offered again.
//
// dismissed exists so the answer sticks. Without it, the very next push
// would re-record the address and the panel would ask again forever.
type registry struct {
	Claimed   map[string][]string `json:"claimed"`
	Seen      map[string][]string `json:"seen"`
	Dismissed map[string][]string `json:"dismissed"`
}

// Store persists the registry as a single JSON file, read fresh on every
// call — same shape and atomic-write discipline as
// internal/displaynames.Store.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// normalise lowercases and trims an address. Every address is stored
// this way so comparing them later is a plain string match — see
// gitstats.ActivityByAuthors, which compares against exactly these.
func normalise(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// valid is a deliberately loose check: exactly one "@", something on
// each side, no spaces. It keeps obvious junk out of the lists; it is
// not trying to decide what a deliverable address is, since nothing is
// ever sent to these — they are only compared against what git stamped
// into a commit.
func valid(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	local, domain, found := strings.Cut(email, "@")
	return found && local != "" && domain != "" && !strings.Contains(domain, "@")
}

// Claimed returns the addresses subject has confirmed as their own.
// Safe on a nil Store, which reads as an empty registry.
func (s *Store) Claimed(subject string) ([]string, error) {
	return s.list(subject, func(r registry) map[string][]string { return r.Claimed })
}

// Suggestions returns addresses seen on subject's pushes that they have
// neither claimed nor dismissed — what the panel offers them to confirm.
func (s *Store) Suggestions(subject string) ([]string, error) {
	return s.list(subject, func(r registry) map[string][]string { return r.Seen })
}

func (s *Store) list(subject string, pick func(registry) map[string][]string) ([]string, error) {
	if s == nil {
		return []string{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.load()
	if err != nil {
		return nil, err
	}
	out := slices.Clone(pick(reg)[subject])
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// Claim confirms email as subject's own, moving it out of their
// suggestions. Claiming an address already claimed succeeds without
// duplicating it.
func (s *Store) Claim(subject, email string) error {
	if subject == "" {
		return ErrInvalidSubject
	}
	addr := normalise(email)
	if !valid(addr) {
		return ErrInvalidEmail
	}

	return s.update(func(reg *registry) bool {
		reg.Seen = remove(reg.Seen, subject, addr)
		reg.Dismissed = remove(reg.Dismissed, subject, addr)
		if slices.Contains(reg.Claimed[subject], addr) {
			// Still write: the two removals above may have changed something.
			return true
		}
		reg.Claimed = add(reg.Claimed, subject, addr)
		return true
	})
}

// Unclaim drops email from subject's confirmed list. An address that
// isn't there is not an error — the caller's intent is already met.
func (s *Store) Unclaim(subject, email string) error {
	if subject == "" {
		return ErrInvalidSubject
	}
	addr := normalise(email)

	return s.update(func(reg *registry) bool {
		if !slices.Contains(reg.Claimed[subject], addr) {
			return false
		}
		reg.Claimed = remove(reg.Claimed, subject, addr)
		return true
	})
}

// Dismiss answers "not me" for email, taking it out of subject's
// suggestions permanently — a later push that carries the same address
// will not offer it again.
func (s *Store) Dismiss(subject, email string) error {
	if subject == "" {
		return ErrInvalidSubject
	}
	addr := normalise(email)
	if !valid(addr) {
		return ErrInvalidEmail
	}

	return s.update(func(reg *registry) bool {
		reg.Seen = remove(reg.Seen, subject, addr)
		if slices.Contains(reg.Dismissed[subject], addr) {
			return true
		}
		reg.Dismissed = add(reg.Dismissed, subject, addr)
		return true
	})
}

// RecordSeen notes that subject pushed a commit signed with email, so
// the panel can offer it for confirmation.
//
// Called from the git server's push path, where returning an error would
// reject someone's push — so anything unusable (a nil Store, a malformed
// address, an address already claimed or dismissed) is silently ignored
// rather than surfaced.
func (s *Store) RecordSeen(subject, email string) error {
	if s == nil || subject == "" {
		return nil
	}
	addr := normalise(email)
	if !valid(addr) {
		return nil
	}

	return s.update(func(reg *registry) bool {
		if slices.Contains(reg.Claimed[subject], addr) ||
			slices.Contains(reg.Dismissed[subject], addr) ||
			slices.Contains(reg.Seen[subject], addr) {
			return false
		}
		reg.Seen = add(reg.Seen, subject, addr)
		return true
	})
}

// update applies mutate under the lock and writes only if it reports a
// change, so the common no-op case (a push repeating an address the
// registry already knows) costs a read rather than a read plus a write.
func (s *Store) update(mutate func(*registry) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.load()
	if err != nil {
		return err
	}
	if !mutate(&reg) {
		return nil
	}
	return s.save(reg)
}

func add(m map[string][]string, subject, addr string) map[string][]string {
	if m == nil {
		m = map[string][]string{}
	}
	m[subject] = append(m[subject], addr)
	return m
}

func remove(m map[string][]string, subject, addr string) map[string][]string {
	if m == nil {
		return nil
	}
	kept := slices.DeleteFunc(slices.Clone(m[subject]), func(e string) bool { return e == addr })
	if len(kept) == 0 {
		delete(m, subject)
	} else {
		m[subject] = kept
	}
	return m
}

func (s *Store) load() (registry, error) {
	empty := registry{
		Claimed:   map[string][]string{},
		Seen:      map[string][]string{},
		Dismissed: map[string][]string{},
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, err
	}
	if len(data) == 0 {
		return empty, nil
	}

	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return empty, err
	}

	// The first shipped version of this file was a bare subject→addresses
	// map, written before suggestions existed. It decodes into the struct
	// above without error (unknown keys are ignored) but leaves every
	// field nil, which is how it's told apart from a real registry —
	// a genuine one always has at least the keys it was saved with.
	// Reading it as claims rather than dropping them avoids repeating the
	// upgrade bug that broke git tokens in production on 2026-09-03.
	if reg.Claimed == nil && reg.Seen == nil && reg.Dismissed == nil {
		legacy := map[string][]string{}
		if err := json.Unmarshal(data, &legacy); err == nil && len(legacy) > 0 {
			empty.Claimed = legacy
			return empty, nil
		}
		return empty, nil
	}

	if reg.Claimed == nil {
		reg.Claimed = map[string][]string{}
	}
	if reg.Seen == nil {
		reg.Seen = map[string][]string{}
	}
	if reg.Dismissed == nil {
		reg.Dismissed = map[string][]string{}
	}
	return reg, nil
}

func (s *Store) save(reg registry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".gitemails-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return renameWithRetry(tmpName, s.path)
}

// renameWithRetry works around a Windows-specific failure this project
// hits in practice: a file that was just created can still be held open
// briefly by another process — an on-access virus scanner is the usual
// culprit, and this machine runs one that was already caught quarantining
// freshly built binaries (see docs/DURUM.md's 2026-09-03 entry). While it
// holds the handle, os.Rename onto the target fails with "Access is
// denied" even though nothing is wrong with either file. The lock clears
// in milliseconds, so a few short retries turn a spurious failure into a
// slight delay.
//
// Deliberately bounded and still returning the last error: a genuine
// permission problem must not be retried into an infinite hang, and must
// still be reported.
func renameWithRetry(from, to string) error {
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(15 * time.Millisecond)
	}
	return err
}
