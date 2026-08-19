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
// independently re-derives what a legitimate request must look like and
// rejects anything that deviates from one of the small, fixed set of
// operations this helper is willing to perform:
//
//	appcmd.exe set vdir "<one of allowedSites>/" /physicalPath:<path under releasesRoot>
//	appcmd.exe stop site /site.name:"<one of allowedSites>"
//	appcmd.exe start site /site.name:"<one of allowedSites>"
//
// The latter two exist for process-based (dotnet-recipe) deploy targets:
// a running process locks its own files, so a bare physical-path swap
// doesn't make it pick up a new release the way it does for a static
// site — see docs/superpowers/specs/2026-08-19-process-based-backend-deploy-design.md.
// Both are gated by the exact same allowedSites set as the physical-path
// swap: iishelper never learns or cares which sites are process-based,
// that decision lives entirely in the deployment package.
func ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool, releasesRoot string) error {
	if req.Name != appcmdPath {
		return fmt.Errorf("%w: unexpected program %q", ErrInvalidRequest, req.Name)
	}

	switch {
	case len(req.Args) == 4 && req.Args[0] == "set" && req.Args[1] == "vdir":
		return validatePhysicalPathSwap(req.Args, allowedSites, releasesRoot)
	case len(req.Args) == 3 && (req.Args[0] == "stop" || req.Args[0] == "start") && req.Args[1] == "site":
		return validateSiteLifecycle(req.Args, allowedSites)
	default:
		return fmt.Errorf("%w: unrecognized command shape", ErrInvalidRequest)
	}
}

// validatePhysicalPathSwap validates the "set vdir .../physicalPath:..."
// shape — unchanged from before this function was split out of
// ValidateRequest.
func validatePhysicalPathSwap(args []string, allowedSites map[string]bool, releasesRoot string) error {
	site, ok := strings.CutSuffix(args[2], "/")
	if !ok {
		return fmt.Errorf("%w: site argument %q must end with /", ErrInvalidRequest, args[2])
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}

	path, ok := strings.CutPrefix(args[3], "/physicalPath:")
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

// validateSiteLifecycle validates "stop site /site.name:<site>" and
// "start site /site.name:<site>" — args[0] is already known to be "stop"
// or "start" and args[1] already known to be "site" by the caller's
// switch, so only the site-name argument needs checking here.
func validateSiteLifecycle(args []string, allowedSites map[string]bool) error {
	site, ok := strings.CutPrefix(args[2], "/site.name:")
	if !ok {
		return fmt.Errorf("%w: third argument must start with /site.name:", ErrInvalidRequest)
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}
	return nil
}
