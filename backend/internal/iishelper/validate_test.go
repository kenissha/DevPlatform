package iishelper

import (
	"errors"
	"testing"
)

const testAppcmdPath = `C:\Windows\System32\inetsrv\appcmd.exe`

// testReleasesRoot is the one directory tree valid test requests are
// allowed to point into — mirrors what devplatform.exe's VersionStore
// would actually be rooted at in production.
const testReleasesRoot = `C:\inetpub\devplatform-test\releases`

func testAllowedSites() map[string]bool {
	return map[string]bool{"DevPlatform Test Site": true}
}

func TestValidateRequest_AcceptsTheOneAllowedShape(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	if err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot); err != nil {
		t.Fatalf("expected a valid request to be accepted, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongProgram(t *testing.T) {
	req := Request{
		Name: `C:\Windows\System32\cmd.exe`,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong program, got: %v", err)
	}
}

func TestValidateRequest_RejectsUnknownSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "Some Other Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unlisted site, got: %v", err)
	}
}

func TestValidateRequest_RejectsRelativePhysicalPath(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", "/physicalPath:releases\\5"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a relative physical path, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongVerb(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"delete", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong verb, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongArgumentCount(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a short argument list, got: %v", err)
	}
}

func TestValidateRequest_RejectsSiteArgumentMissingTrailingSlash(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a site argument missing its trailing slash, got: %v", err)
	}
}

func TestValidateRequest_RejectsFourthArgumentMissingPhysicalPathPrefix(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a fourth argument missing the /physicalPath: prefix, got: %v", err)
	}
}

func TestValidateRequest_RejectsPhysicalPathOutsideReleasesRoot(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\Windows\System32`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a physical path outside the releases root, got: %v", err)
	}
}

// A naive prefix check (strings.HasPrefix(path, root)) would wrongly
// accept this: "...\releases-evil" starts with "...\releases" as a raw
// string, even though it is a sibling directory, not a subdirectory.
func TestValidateRequest_RejectsSiblingDirectoryOfReleasesRoot(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases-evil\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a sibling directory of the releases root, got: %v", err)
	}
}

func TestValidateRequest_RejectsEverythingWhenReleasesRootIsUnset(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), "")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest when no releases root is configured, got: %v", err)
	}
}
