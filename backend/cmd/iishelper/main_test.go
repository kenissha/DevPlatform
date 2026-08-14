package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetup_LoadsSitesAndOpensListener exercises setup()'s wiring without
// ever executing a real appcmd.exe — it only opens the pipe and checks
// the Server it returns is configured correctly; it never calls
// Serve/Execute. If a real iishelper Windows Service happens to already
// be running on the machine this test runs on, opening the same
// well-known pipe name will fail — that's expected and matches how this
// codebase's other live-system tests document their environment
// assumptions (see docs/DURUM.md's dotnet SDK version note).
func TestSetup_LoadsSitesAndOpensListener(t *testing.T) {
	dir := t.TempDir()
	targetsFile := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(targetsFile, []byte(`[{"siteName":"Test Site"}]`), 0o600); err != nil {
		t.Fatalf("failed to write fixture targets file: %v", err)
	}
	t.Setenv("DEVPLATFORM_DEPLOY_TARGETS_FILE", targetsFile)
	t.Setenv("DEVPLATFORM_IISHELPER_SDDL", "")

	ln, srv, err := setup()
	if err != nil {
		t.Fatalf("setup() returned an error: %v", err)
	}
	defer ln.Close()

	if !srv.AllowedSites["Test Site"] {
		t.Errorf("expected %q to be an allowed site, got %v", "Test Site", srv.AllowedSites)
	}
	if srv.AppcmdPath == "" {
		t.Error("expected a non-empty AppcmdPath")
	}
}
