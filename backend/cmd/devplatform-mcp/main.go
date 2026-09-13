// Command devplatform-mcp puts the DevPlatform task board in front of
// Claude Code as a set of tools.
//
// It is not a server anybody deploys. Claude Code starts this process
// when a session opens, talks to it over stdin/stdout, and closes it when
// the session ends — the same lifecycle as any other MCP server. Nothing
// listens on a port, and nothing is installed on the IIS box.
//
// It authenticates as whoever ran devplatform-login on this machine, with
// the credential that login already cached (see internal/logincache and
// internal/apiauth). It mints nothing, stores nothing, and prints no
// credential — the one already on disk is the one it uses.
//
// Setup, once:
//
//	claude mcp add devplatform -- C:\path\to\devplatform-mcp.exe
//
// with DEVPLATFORM_URL pointing at the panel.
package main

import (
	"fmt"
	"os"

	"github.com/kenissha/DevPlatform/backend/internal/logincache"
)

// buildVersion is reported in the MCP handshake so a client's logs name
// which build answered.
const buildVersion = "1"

// defaultBaseURL is the production panel. A developer running against a
// local instance overrides it with DEVPLATFORM_URL rather than editing
// the MCP registration.
const defaultBaseURL = "https://git.sigortatahkim.org"

func main() {
	if err := run(); err != nil {
		// stderr, never stdout: stdout is the protocol channel, and a
		// stray line there desynchronises the client for the rest of the
		// session. Claude Code surfaces stderr in its MCP logs.
		fmt.Fprintf(os.Stderr, "devplatform-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	baseURL := os.Getenv("DEVPLATFORM_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	cred, err := logincache.Load()
	if err != nil {
		return fmt.Errorf("kayıtlı giriş okunamadı: %w", err)
	}
	if cred == nil || cred.Token == "" {
		// Failing at startup rather than on the first tool call: an MCP
		// server that starts and then errors on everything looks broken,
		// while one that refuses to start says why in the client's logs.
		return fmt.Errorf("bu makinede DevPlatform girişi yok — terminalde devplatform-login çalıştır")
	}

	api := newAPIClient(baseURL, cred.Subject, cred.Token)
	srv := &server{in: os.Stdin, out: os.Stdout, tools: buildTools(api)}
	return srv.serve()
}
