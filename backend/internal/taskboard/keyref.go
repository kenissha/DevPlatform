package taskboard

import (
	"strings"
)

// KeyRefsIn finds the task keys a commit message mentions, given the
// prefix that repository's keys use.
//
// Only that repository's own prefix is matched, deliberately. Keys are
// per-repository, and so is access: linking a commit pushed to one repo
// onto a task in another would show people work in a repository they may
// not be allowed to see. It also removes the "did DEN-14 mean their DEN
// or ours?" ambiguity entirely.
//
// Matching is case-insensitive because people type "den-14" as readily as
// "DEN-14", and results are deduplicated in first-seen order — a message
// that says "DEN-14" three times describes one task.
func KeyRefsIn(message, prefix string) []string {
	if prefix == "" || message == "" {
		return nil
	}

	upper := strings.ToUpper(message)
	upperPrefix := strings.ToUpper(prefix)

	var found []string
	seen := map[string]bool{}

	for i := 0; i+len(upperPrefix) < len(upper); i++ {
		if !strings.HasPrefix(upper[i:], upperPrefix) {
			continue
		}
		// The character before must not be alphanumeric, or "GARDEN-14"
		// would read as a reference to DEN-14.
		if i > 0 && isKeyChar(upper[i-1]) {
			continue
		}
		rest := upper[i+len(upperPrefix):]
		if len(rest) == 0 || rest[0] != '-' {
			continue
		}
		digits := 0
		for digits < len(rest)-1 && isDigit(rest[digits+1]) {
			digits++
		}
		if digits == 0 {
			continue
		}
		// A trailing letter or digit means this is not the whole token:
		// "DEN-14b" is something else, and guessing which task it meant is
		// worse than not linking it.
		after := 1 + digits
		if after < len(rest) && isKeyChar(rest[after]) {
			continue
		}

		key := upperPrefix + "-" + rest[1:1+digits]
		if !seen[key] {
			seen[key] = true
			found = append(found, key)
		}
		i += len(upperPrefix) + digits
	}
	return found
}

// PrefixFor returns the key prefix repo's tasks use, or "" when the
// repository has never had a task and therefore has no keys to match.
//
// Read from the registry rather than recomputed: resolvePrefix may have
// lengthened a prefix to avoid a collision, and recomputing here would
// silently match the wrong repository's keys.
func (s *Store) PrefixFor(repo string) (string, error) {
	if !validRepoName.MatchString(repo) {
		return "", ErrInvalidRepo
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.loadKeys()
	if err != nil {
		return "", err
	}
	return reg.Prefixes[repo], nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isKeyChar(b byte) bool {
	return isDigit(b) || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
