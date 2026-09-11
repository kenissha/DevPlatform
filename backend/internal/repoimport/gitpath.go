package repoimport

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Finding the git binary is its own problem on the server this runs on.
//
// Every other part of the platform talks to repositories through go-git,
// so until imports existed the server needed no git installation at all —
// a property worth having and one this package broke by accident. Imports
// keep shelling out to real git on purpose (see GitCloner: for a one-off
// copy of somebody's actual history, "behaves exactly like git" beats one
// less dependency), which means the binary has to be found reliably.
//
// Under IIS it usually is not. httpPlatformHandler starts the process with
// its own environment, and the app pool identity's PATH routinely lacks
// the entry Git for Windows adds for interactive users — so exec.Command
// fails with the least helpful error in the world: "executable file not
// found in %PATH%".

// ErrGitMissing is returned when no git binary can be found. Its message
// says what to do, because the raw exec error does not.
var ErrGitMissing = errors.New(
	"git bulunamadı. Sunucuda Git for Windows kurulu değilse kur, kuruluysa " +
		"DEVPLATFORM_GIT_PATH ortam değişkenine git.exe'nin tam yolunu ver " +
		`(örn. C:\Program Files\Git\cmd\git.exe)`)

// GitPathEnv names the override. Set it when git is installed somewhere
// the service's PATH does not reach, which under IIS is the normal case
// rather than the exception.
const GitPathEnv = "DEVPLATFORM_GIT_PATH"

// wellKnownGitPaths are the default install locations Git for Windows
// uses. Checked after PATH, so a deliberate PATH entry still wins, and
// before giving up — finding git where it actually is beats telling
// somebody to configure what we could have worked out.
var wellKnownGitPaths = []string{
	`C:\Program Files\Git\cmd\git.exe`,
	`C:\Program Files (x86)\Git\cmd\git.exe`,
	`C:\Program Files\Git\bin\git.exe`,
	`/usr/bin/git`,
	`/usr/local/bin/git`,
}

var (
	gitOnce sync.Once
	gitPath string
	gitErr  error
)

// GitPath returns the git binary to run, resolved once per process.
//
// Cached because the answer cannot change while the service runs, and
// because it is wanted at startup (to warn) as well as per import.
func GitPath() (string, error) {
	gitOnce.Do(func() { gitPath, gitErr = findGit() })
	return gitPath, gitErr
}

func findGit() (string, error) {
	// An explicit setting wins outright: somebody who set it knows
	// something about this machine that no search will discover.
	if configured := os.Getenv(GitPathEnv); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		// Pointing at a path that is not there is a mistake worth naming,
		// rather than quietly searching on and using a different git than
		// the one that was asked for.
		return "", errors.New(GitPathEnv + " ayarlı ama o yolda bir dosya yok: " + configured)
	}

	if found, err := exec.LookPath("git"); err == nil {
		return found, nil
	}

	for _, candidate := range wellKnownGitPaths {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}

	return "", ErrGitMissing
}
