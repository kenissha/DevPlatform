package iishelper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllowedSites_EmptyPathReturnsEmptySet(t *testing.T) {
	sites, err := LoadAllowedSites("")
	if err != nil {
		t.Fatalf("expected no error for an empty path, got: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected an empty set, got: %v", sites)
	}
}

func TestLoadAllowedSites_ReadsSiteNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed-sites.json")
	const contents = `["Intranet-F Test", "Intranet-B Test"]`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	sites, err := LoadAllowedSites(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sites["Intranet-F Test"] || !sites["Intranet-B Test"] {
		t.Fatalf("expected both configured site names to be present, got: %v", sites)
	}
	if len(sites) != 2 {
		t.Fatalf("expected exactly 2 sites, got %d: %v", len(sites), sites)
	}
}

func TestLoadAllowedSites_MissingFileIsAnError(t *testing.T) {
	_, err := LoadAllowedSites(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent (but non-empty) path")
	}
}

func TestLoadAllowedSites_MalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed-sites.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	_, err := LoadAllowedSites(path)
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
