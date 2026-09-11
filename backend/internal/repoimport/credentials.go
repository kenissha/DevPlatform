package repoimport

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Keeping the access token out of everything is most of this file's job.
// A token for a private repository is a real credential, and a clone is a
// short operation with a long list of places the token can end up in
// permanently:
//
//   - the command line, where any user on the box can read it out of the
//     process list for as long as the clone runs
//   - the cloned repository's own config, because `git clone <url>` writes
//     the URL it was given into remote.origin.url — which would leave the
//     token sitting in DataDir forever
//   - git's error output, which echoes the remote URL verbatim on failure,
//     and which we want to show the person who started the import
//
// So the URL git is given never contains the token. It goes into a
// throwaway credential file instead, which git reads and we delete.

// credentialFile writes a git credential-store file for rawURL + token and
// returns its path along with a cleanup function.
//
// The file holds the token in plaintext, so it is created 0600 in a
// per-import temp directory and removed as soon as the clone finishes.
// This is the same format `git credential-store` uses, one URL per line:
//
//	https://user:token@host
func credentialFile(dir, rawURL, token string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("kaynak adresi çözümlenemedi: %w", err)
	}

	// "x-access-token" is what GitHub documents for token auth, and every
	// other forge that accepts a token in the password field ignores the
	// username entirely — so one value works everywhere.
	user := "x-access-token"
	if u.User != nil && u.User.Username() != "" {
		user = u.User.Username()
	}

	line := fmt.Sprintf("%s://%s:%s@%s\n",
		u.Scheme, url.QueryEscape(user), url.QueryEscape(token), u.Host)

	path := filepath.Join(dir, "git-credentials")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// sanitiseURL strips any credentials already embedded in the source URL.
//
// People paste what their terminal had, and that is routinely
// https://token@github.com/... — see the OASRapor remotes. Handing that
// straight to git would write the token into the imported repository's
// config, which is the exact outcome this package exists to avoid.
func sanitiseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

// tokenIn returns the credential embedded in rawURL, if any, so a pasted
// "https://token@github.com/..." still authenticates after sanitiseURL has
// stripped it. Returns "" when the URL carries no password.
func tokenIn(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return ""
	}
	if pw, ok := u.User.Password(); ok && pw != "" {
		return pw
	}
	// "https://ghp_xxx@host" — the whole userinfo is the token.
	return u.User.Username()
}

// scrub removes every trace of secret from text.
//
// Called on everything git writes before it reaches a log, an audit entry
// or the panel. git echoes the remote URL on failure ("fatal: could not
// read Username for 'https://...'"), and a credential helper's contents
// can surface in transfer errors, so the output of a failed clone is
// exactly where a token is most likely to appear.
//
// Empty secrets are skipped rather than replaced: strings.ReplaceAll with
// an empty old string inserts the replacement between every character.
func scrub(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "***")
		// A token travelling inside a URL arrives percent-encoded, so the
		// raw form alone would not match.
		if escaped := url.QueryEscape(secret); escaped != secret {
			text = strings.ReplaceAll(text, escaped, "***")
		}
	}
	return text
}
