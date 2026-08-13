// Package iishelper implements privilege separation for the one operation
// in this codebase that needs Administrator rights: pointing an IIS
// site's physical path at a new release directory via appcmd.exe.
// devplatform.exe (git hosting, panel API, and — critically — running a
// repository's own build scripts) never needs to run elevated; only this
// package's Server does, and it accepts exactly one request shape.
//
// This package is Windows-only: it depends on named pipes
// (github.com/Microsoft/go-winio) and, in cmd/iishelper, the Windows
// Service Control Manager (golang.org/x/sys/windows/svc). DevPlatform
// only ever runs on Windows (IIS has no other platform), so no
// cross-platform fallback is provided.
package iishelper

// PipeName is the well-known named pipe devplatform.exe's
// HelperCommandRunner dials and cmd/iishelper listens on. Fixed and
// unconfigurable — this is not a general-purpose IPC mechanism, it is the
// one channel between exactly these two processes.
const PipeName = `\\.\pipe\devplatform-iishelper`

// Request is what devplatform.exe sends: run Name with Args. Its shape
// mirrors deploy.CommandRunner.Run's parameters, but in practice Name is
// always deploy.AppcmdPath() and Args is always the fixed "set vdir"
// shape ValidateRequest checks — see that function's doc comment for why
// this type stays generic-looking without the request itself being
// treated as trusted.
type Request struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// Response carries the result back. Error is empty on success; Output
// holds appcmd's combined stdout/stderr either way, matching
// deploy.CommandRunner.Run's own (output, error) shape.
type Response struct {
	Output []byte `json:"output"`
	Error  string `json:"error,omitempty"`
}
