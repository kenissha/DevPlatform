package iishelper

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidRequest indicates a Request that does not exactly match the
// one operation this helper is willing to perform. Wrapped with
// fmt.Errorf("%w: ...") in every rejection below so callers can test for
// it with errors.Is regardless of the specific reason.
var ErrInvalidRequest = errors.New("iishelper: request does not match the only allowed operation")

// ValidateRequest is the actual security boundary of this package: it
// never trusts req as coming from a well-behaved devplatform.exe. It
// independently re-derives what a legitimate request must look like —
// appcmdPath is the caller's own computation of deploy.AppcmdPath()
// (passed in rather than imported directly so tests can use a fixed
// value), allowedSites is the set of IIS site names this deploy server
// is actually configured to manage (see LoadAllowedSites), and
// releasesRoot is the one directory tree a physical path is ever allowed
// to point into (see cmd/iishelper's DEVPLATFORM_RELEASES_ROOT) — and
// rejects anything that
// deviates in any way from
// appcmd.exe set vdir "<one of allowedSites>/" /physicalPath:<path under releasesRoot>
//
// The releasesRoot check exists because devplatform.exe is the only
// caller that ever constructs a physical path, and it always builds one
// from its own VersionStore — but a request reaching this pipe is not
// proof devplatform.exe sent it in good faith. If devplatform.exe were
// ever compromised, the site allowlist above stops an attacker from
// touching a site this deploy server doesn't manage, but without this
// check they could still repoint an allowed site's virtual directory at
// any absolute path on disk (e.g. a folder holding unrelated data),
// exposing it over that site's URL without ever running a command.
// Confining the physical path to releasesRoot closes that.
func ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool, releasesRoot string) error {
	if req.Name != appcmdPath {
		return fmt.Errorf("%w: unexpected program %q", ErrInvalidRequest, req.Name)
	}
	if len(req.Args) != 4 {
		return fmt.Errorf("%w: expected exactly 4 arguments, got %d", ErrInvalidRequest, len(req.Args))
	}
	if req.Args[0] != "set" || req.Args[1] != "vdir" {
		return fmt.Errorf("%w: unexpected verb %q %q", ErrInvalidRequest, req.Args[0], req.Args[1])
	}

	site, ok := strings.CutSuffix(req.Args[2], "/")
	if !ok {
		return fmt.Errorf("%w: site argument %q must end with /", ErrInvalidRequest, req.Args[2])
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}

	path, ok := strings.CutPrefix(req.Args[3], "/physicalPath:")
	if !ok {
		return fmt.Errorf("%w: fourth argument must start with /physicalPath:", ErrInvalidRequest)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: physical path %q must be absolute", ErrInvalidRequest, path)
	}
	if releasesRoot == "" {
		return fmt.Errorf("%w: no releases root is configured, refusing every physical path", ErrInvalidRequest)
	}
	rel, err := filepath.Rel(releasesRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: physical path %q is outside the configured releases root %q", ErrInvalidRequest, path, releasesRoot)
	}

	return nil
}
