// Package secretscan detects known secret patterns in blob content. It is
// used at push time (see internal/gitserver's scanningLoader) to reject
// pushes that would introduce a credential into the repository, matching
// the "push anında secret taraması" requirement in the design doc.
package secretscan

import "regexp"

// pattern pairs a human-readable name (used in rejection messages — never
// the matched text itself, to avoid echoing the secret back into git's
// error output / the pusher's terminal history) with its detector.
type pattern struct {
	name string
	re   *regexp.Regexp
}

var patterns = []pattern{
	{"private-key-block", regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`)},
	{"aws-access-key-id", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"aws-secret-access-key", regexp.MustCompile(`(?i)aws_secret_access_key\s*=\s*[A-Za-z0-9/+=]{40}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
	{"connection-string-password", regexp.MustCompile(`(?is)(Server|Data Source)\s*=.{0,200}?(Password|Pwd)\s*=|(Password|Pwd)\s*=.{0,200}?(Server|Data Source)\s*=`)},
}

// Scan checks content against every known secret pattern and returns the
// name of the first one that matches, or ok=false if none do.
func Scan(content []byte) (name string, ok bool) {
	for _, p := range patterns {
		if p.re.Match(content) {
			return p.name, true
		}
	}
	return "", false
}
