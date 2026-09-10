package taskboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kenissha/DevPlatform/backend/internal/atomicfile"
)

// Task keys — "DEN-14" — exist so a task can be named out loud, written
// in a commit message, or pasted into a chat. An opaque 16-hex id cannot
// do any of that.
//
// The prefix is derived from the repository name rather than configured:
// a field somebody has to fill in is a field that gets left blank, and
// the derived answer is right almost every time.

// keyPrefix turns a repository name into a short uppercase prefix.
//
// Only ASCII letters and digits survive, and Turkish letters are folded
// to their ASCII neighbours first (ç→C, ş→S, ı→I …): a key is meant to be
// typed on any keyboard and to travel through git commit messages and
// URLs unharmed.
func keyPrefix(repo string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(repo) {
		switch r {
		case 'Ç':
			b.WriteRune('C')
		case 'Ğ':
			b.WriteRune('G')
		case 'İ', 'I':
			b.WriteRune('I')
		case 'Ö':
			b.WriteRune('O')
		case 'Ş':
			b.WriteRune('S')
		case 'Ü':
			b.WriteRune('U')
		default:
			if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				b.WriteRune(r)
			}
		}
		if b.Len() >= 3 {
			break
		}
	}
	prefix := b.String()
	if prefix == "" {
		// Nothing usable in the name (all punctuation, or a script with no
		// ASCII fold). A generic prefix keeps keys well-formed; the number
		// still makes them unique.
		return "TSK"
	}
	return prefix
}

// resolvePrefix returns repo's prefix, avoiding one already taken by a
// different repository.
//
// Two repos can easily start with the same three letters
// ("intranet-servis", "intranet-frontend"). The first one to be given a
// key keeps the short prefix; later ones lengthen — INT, INTR, INTRA —
// until they are unique. Prefixes are stored, never recomputed, so a repo
// created later can never take a prefix that is already in use and no
// existing task's key changes meaning.
func resolvePrefix(repo string, taken map[string]string) string {
	if existing, ok := taken[repo]; ok {
		return existing
	}

	inUse := map[string]bool{}
	for _, p := range taken {
		inUse[p] = true
	}

	base := keyPrefix(repo)
	if !inUse[base] {
		return base
	}

	// Taken. Lengthen using the repo's own letters first — INTR, INTRA —
	// because a longer slice of the real name still reads as that repo.
	for n := len(base) + 1; n <= 8; n++ {
		longer := prefixOfLength(repo, n)
		if longer == base {
			break // the name has no more usable characters
		}
		if !inUse[longer] {
			return longer
		}
	}

	// The name is exhausted (two repos differing only past the eighth
	// character, or after punctuation was stripped). Fall back to a
	// numeric suffix, which is ugly but always available.
	for n := 2; n < 1000; n++ {
		numbered := fmt.Sprintf("%s%d", base, n)
		if !inUse[numbered] {
			return numbered
		}
	}
	return base
}

// prefixOfLength is keyPrefix with a different cut-off.
func prefixOfLength(repo string, n int) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(repo) {
		switch r {
		case 'Ç':
			b.WriteRune('C')
		case 'Ğ':
			b.WriteRune('G')
		case 'İ', 'I':
			b.WriteRune('I')
		case 'Ö':
			b.WriteRune('O')
		case 'Ş':
			b.WriteRune('S')
		case 'Ü':
			b.WriteRune('U')
		default:
			if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				b.WriteRune(r)
			}
		}
		if b.Len() >= n {
			break
		}
	}
	return b.String()
}

// keyRegistry is the on-disk record behind key allocation: the prefix
// each repository was given, and the highest number handed out for it.
type keyRegistry struct {
	Prefixes map[string]string `json:"prefixes"`
	Counters map[string]int    `json:"counters"`
}

// nextKey allocates the next key for repo and persists the allocation
// before returning it.
//
// Written first, used second, on purpose. If task creation then fails,
// the number is burned and the sequence has a gap — which is harmless and
// already expected, since deleting a task leaves a gap too. The
// alternative (write the task, then the counter) can hand the same key to
// two tasks after a crash, and two tasks sharing a name is a real
// problem where a missing number is not.
//
// Callers must hold s.mu.
func (s *Store) nextKey(repo string) (string, error) {
	reg, err := s.loadKeys()
	if err != nil {
		return "", err
	}

	prefix := resolvePrefix(repo, reg.Prefixes)
	reg.Prefixes[repo] = prefix
	reg.Counters[repo]++
	number := reg.Counters[repo]

	if err := s.saveKeys(reg); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", prefix, number), nil
}

func (s *Store) keysPath() string {
	return filepath.Join(s.rootDir, "task-keys.json")
}

func (s *Store) loadKeys() (keyRegistry, error) {
	reg := keyRegistry{Prefixes: map[string]string{}, Counters: map[string]int{}}

	data, err := os.ReadFile(s.keysPath())
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return keyRegistry{}, err
	}
	if len(data) == 0 {
		return reg, nil
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return keyRegistry{}, err
	}
	if reg.Prefixes == nil {
		reg.Prefixes = map[string]string{}
	}
	if reg.Counters == nil {
		reg.Counters = map[string]int{}
	}
	return reg, nil
}

func (s *Store) saveKeys(reg keyRegistry) error {
	if err := os.MkdirAll(s.rootDir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.rootDir, ".task-keys-*.tmp")
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
	// See internal/atomicfile for why this is not os.Rename.
	return atomicfile.Rename(tmpName, s.keysPath())
}
