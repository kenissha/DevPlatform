// Package repoimport brings an existing git repository onto the platform
// with its history intact.
//
// This exists because the panel could only ever create empty repositories,
// and the only way to fill one was to push — which runs through the secret
// scanner (see internal/gitserver). For a repository that already exists
// somewhere else, that is the wrong gate: the scanner's job is to stop
// somebody committing a credential today, and a years-old commit that once
// held one has already been published wherever the repository lived. In
// practice it made importing a real project impossible, because a single
// bad blob anywhere in 600 commits rejected the whole push.
//
// So an import writes the repository directly into DataDir instead of
// pushing it. That deliberately bypasses the scanner — and deliberately
// does not do so silently: every import scans the history it brought in
// and reports what it found (see scan.go). Bypassing a check is
// defensible; hiding that you bypassed it is not.
//
// Everyday pushes are unaffected. A push only carries objects the server
// does not already have, so once a history is imported the scanner sees
// only new work — which is precisely what it should be looking at.
package repoimport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidName   = errors.New("repoimport: invalid repository name")
	ErrInvalidSource = errors.New("repoimport: invalid source URL")
	ErrAlreadyExists = errors.New("repoimport: repository already exists")
	ErrNotFound      = errors.New("repoimport: import not found")
)

// Timeout bounds a single clone. Generous because the point of comparison
// is a person watching a progress line, not a web request: a few hundred
// megabytes over a corporate link can genuinely take minutes, and failing
// at four would just mean doing it again.
const Timeout = 30 * time.Minute

// validName mirrors repostore's own rule. Duplicated rather than imported
// so this package's path-building is safe against traversal on its own —
// the same reasoning taskboard uses for its copy.
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Status is where an import has got to. Imports are slow enough that the
// panel has to show progress, so the states exist to be rendered, not just
// to be recorded.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Job is one import, as the panel sees it.
type Job struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Source never contains credentials — see sanitiseURL. It is shown in
	// the panel and written to the audit log.
	Source    string    `json:"source"`
	Status    Status    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	// Finished is zero while the job runs.
	Finished time.Time `json:"finishedAt,omitempty"`
	Commits  int       `json:"commits,omitempty"`
	Branches int       `json:"branches,omitempty"`
	// Findings lists secrets discovered in the imported history. Empty is
	// the good case; non-empty is information, not failure — the import
	// has already succeeded by the time this is filled in.
	Findings []Finding `json:"findings,omitempty"`
	// Error is a message safe to show: scrubbed of any credential before
	// it is ever stored.
	Error string `json:"error,omitempty"`
}

// Running reports whether the job is still working.
func (j Job) Running() bool { return j.Status == StatusRunning }

// Cloner runs the actual git clone. Real imports use GitCloner; tests
// substitute their own so the package can be tested without a network.
type Cloner interface {
	// Clone copies source into destDir as a bare repository. token may be
	// empty for a public repository. Implementations must not let token
	// reach the process arguments, the cloned config, or the returned
	// error.
	Clone(ctx context.Context, source, token, destDir string) error
}

// GitCloner shells out to the real git binary.
//
// This is the only place the server process does. Everything else talks to
// repositories through go-git — internal/deploy checks out that way
// deliberately — so before imports existed, running this platform required
// no git installation at all. Choosing the binary here gives that up, and
// it was first written down as "git is already a dependency anyway", which
// was simply wrong: production said so, with
// "exec: git: executable file not found in %PATH%".
//
// The choice still stands, for a reason that survives being checked: an
// import copies somebody's real history once, and git is the
// implementation every forge is actually tested against. go-git's own
// clone would have to be trusted to bring every branch and tag of a
// several-hundred-megabyte private repository faithfully, on the one
// occasion where getting it wrong is expensive and hard to notice.
//
// What the mistake cost was the error message, so that is what got fixed:
// see gitpath.go, which finds git where it really is and, failing that,
// says what to install or set.
type GitCloner struct{}

func (GitCloner) Clone(ctx context.Context, source, token, destDir string) error {
	// Resolved before anything else so a missing git is reported as the
	// one sentence that fixes it, rather than as exec's
	// "executable file not found in %PATH%" — which is true, unhelpful,
	// and the first thing this feature hit in production.
	gitBin, err := GitPath()
	if err != nil {
		return err
	}

	clean := sanitiseURL(source)

	// A per-clone temp directory so the credential file cannot collide
	// with a concurrent import's.
	credDir, mkErr := os.MkdirTemp("", "devplatform-import-")
	if mkErr != nil {
		return mkErr
	}
	defer os.RemoveAll(credDir)

	args := []string{
		// Disable inherited helpers first. On Windows the Git Credential
		// Manager is usually configured globally, and it would otherwise
		// pop a GUI prompt on the server — which no one is there to
		// answer, so the clone would hang until Timeout.
		"-c", "credential.helper=",
	}
	if token != "" {
		credPath, err := credentialFile(credDir, clean, token)
		if err != nil {
			return err
		}
		// The path goes in the arguments; the token stays in the file.
		args = append(args, "-c", "credential.helper=store --file="+filepath.ToSlash(credPath))
	}
	// Never wait on a prompt: without this git asks for a username on
	// stdin when credentials are missing or wrong, and a server process
	// has no stdin to answer with.
	args = append(args, "-c", "core.askPass=")
	args = append(args, "clone", "--bare", "--quiet", clean, destDir)

	// The resolved binary and the credential-free URL, so the log shows
	// exactly what ran. The token is in a file, never in these arguments.
	log.Printf("repoimport: git klonlanıyor — %s %s (anahtar: %s)",
		gitBin, clean, map[bool]string{true: "var", false: "yok"}[token != ""])

	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		// Git's own credential prompts are disabled above; this stops the
		// Windows helper being picked up from the system config too.
		"GCM_INTERACTIVE=never",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(scrub(string(out), token, tokenIn(source)))
		if msg == "" {
			msg = err.Error()
		}
		if ctx.Err() != nil {
			return fmt.Errorf("klonlama zaman aşımına uğradı (%s): %s", Timeout, msg)
		}
		return errors.New(msg)
	}

	// `git clone` records the URL it was given. Even credential-free, a
	// bare repository on this server pointing back at GitHub invites a
	// later `git fetch` that nobody asked for — and if the person pasted a
	// URL with a token in it, sanitiseURL is the only thing that kept the
	// token out of this file. Removing the remote closes both.
	rm := exec.Command(gitBin, "-C", destDir, "remote", "remove", "origin")
	if out, err := rm.CombinedOutput(); err != nil {
		return fmt.Errorf("kaynak bağlantısı kaldırılamadı: %s",
			strings.TrimSpace(scrub(string(out), token, tokenIn(source))))
	}
	return nil
}

// countRefs reports how many commits and branches an imported repository
// holds, for the panel's "621 commit, 9 dal" line.
func countRefs(dir string) (commits, branches int) {
	gitBin, err := GitPath()
	if err != nil {
		return 0, 0
	}
	commits = countLines(exec.Command(gitBin, "-C", dir, "rev-list", "--count", "--all"), true)
	branches = countLines(exec.Command(gitBin, "-C", dir, "for-each-ref", "--format=%(refname)", "refs/heads"), false)
	return commits, branches
}

// countLines runs cmd and either parses its single-number output
// (single=true) or counts the lines it produced. A failure reports zero:
// these numbers are decoration on a successful import, and none of them is
// worth failing it for.
func countLines(cmd *exec.Cmd, single bool) int {
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0
	}
	if single {
		n := 0
		if _, err := fmt.Sscanf(text, "%d", &n); err != nil {
			return 0
		}
		return n
	}
	return len(strings.Split(text, "\n"))
}
