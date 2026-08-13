package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CommandRunner executes a named program with fixed arguments and returns
// its combined output. Abstracted behind an interface so IISSwapper's
// argument-building logic can be tested without invoking the real
// appcmd.exe, which requires Administrator privileges.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// RealCommandRunner is the production CommandRunner: it actually executes
// the given program via os/exec, never a shell.
type RealCommandRunner struct{}

func (RealCommandRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("deploy: %s %v failed: %w\n%s", name, args, err, out)
	}
	return out, nil
}

// AppcmdPath returns the absolute path to appcmd.exe. Installing the IIS
// Windows feature does not add appcmd.exe's directory to PATH, so it can't
// be invoked by bare name — it must always be resolved from its fixed,
// well-known system location, %SystemRoot%\System32\inetsrv\appcmd.exe.
//
// Exported so internal/iishelper's request validator can independently
// compute the same path and compare it against an incoming request's Name
// field, rather than trusting that field as-is.
func AppcmdPath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows` // SystemRoot is always set on real Windows machines; this fallback only matters for unusual environments
	}
	return filepath.Join(systemRoot, "System32", "inetsrv", "appcmd.exe")
}

// IISSwapper points an IIS site at a new physical path via appcmd.exe.
type IISSwapper struct {
	runner CommandRunner
}

// NewIISSwapper returns an IISSwapper that executes commands via runner.
// Production callers pass &RealCommandRunner{}; tests pass a fake.
func NewIISSwapper(runner CommandRunner) *IISSwapper {
	return &IISSwapper{runner: runner}
}

// SetPhysicalPath points siteName's root virtual directory at path. Both
// arguments must already be validated/trusted by the caller (e.g.
// siteName resolved from a fixed admin-configured mapping, path built by
// VersionStore) — this function does not itself validate them beyond
// passing them as separate argv entries (never concatenated into a single
// string), which is what actually matters for command-injection safety:
// appcmd receives siteName and the physicalPath value as distinct,
// unparsed arguments, exactly as os/exec passes them to the OS, with no
// shell in between to reinterpret special characters.
func (s *IISSwapper) SetPhysicalPath(siteName, path string) error {
	// IIS requires an absolute physical path. A relative path doesn't
	// resolve against anything meaningful — the IIS worker process has its
	// own working directory, unrelated to wherever the path was computed
	// from — so appcmd would "succeed" while leaving the site serving a
	// 404 with no indication why. Reject it here so any caller (not just
	// this package's own tests or deploydemo) gets a clear, actionable
	// error immediately instead of a mysterious IIS-side failure
	// discovered only by a human checking a browser.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("deploy: physical path %q must be absolute", path)
	}

	_, err := s.runner.Run(AppcmdPath(), "set", "vdir", siteName+"/", "/physicalPath:"+path)
	if err != nil {
		return fmt.Errorf("deploy: failed to set physical path for site %q: %w", siteName, err)
	}
	return nil
}
