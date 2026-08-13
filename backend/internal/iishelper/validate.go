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
// value), and allowedSites is the set of IIS site names this deploy
// server is actually configured to manage (see LoadAllowedSites) — and
// rejects anything that deviates in any way from
// appcmd.exe set vdir "<one of allowedSites>/" /physicalPath:<absolute path>
func ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool) error {
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

	return nil
}
