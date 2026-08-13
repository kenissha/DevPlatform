package iishelper

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialTimeout bounds how long a single Run call — connect, send, and
// read the response — is allowed to take. If iishelper is not running or
// hangs, a deploy must fail cleanly here rather than block forever;
// internal/deployment.Handlers.Approve already has its own, longer
// deploy-level timeout, so this is the inner safety net for this one
// step.
const dialTimeout = 30 * time.Second

// HelperCommandRunner implements deploy.CommandRunner by forwarding the
// call to iishelper over a named pipe instead of executing appcmd.exe
// directly — this is the only production change deploy.IISSwapper needed,
// since it already depended only on the CommandRunner interface.
type HelperCommandRunner struct {
	// Dial opens a connection to iishelper. NewHelperCommandRunner sets
	// this to a real named-pipe dial; tests set it directly to dial a
	// loopback listener instead.
	Dial func() (net.Conn, error)
}

var _ interface {
	Run(name string, args ...string) ([]byte, error)
} = (*HelperCommandRunner)(nil)

// NewHelperCommandRunner returns a HelperCommandRunner that dials
// iishelper's well-known named pipe (PipeName).
func NewHelperCommandRunner() *HelperCommandRunner {
	return &HelperCommandRunner{
		Dial: func() (net.Conn, error) {
			return winio.DialPipe(PipeName, nil)
		},
	}
}

// Run satisfies deploy.CommandRunner. name/args are forwarded verbatim to
// iishelper, which independently validates them before executing
// anything — Run itself does no validation, it is purely a transport.
func (h *HelperCommandRunner) Run(name string, args ...string) ([]byte, error) {
	conn, err := h.Dial()
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to connect to the IIS helper service: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(dialTimeout))

	if err := json.NewEncoder(conn).Encode(Request{Name: name, Args: args}); err != nil {
		return nil, fmt.Errorf("iishelper: failed to send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("iishelper: failed to read response: %w", err)
	}
	if resp.Error != "" {
		return resp.Output, fmt.Errorf("iishelper: %s", resp.Error)
	}
	return resp.Output, nil
}
