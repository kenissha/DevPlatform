package iishelper

import (
	"errors"
	"testing"
)

const testAppcmdPath = `C:\Windows\System32\inetsrv\appcmd.exe`

func testAllowedSites() map[string]bool {
	return map[string]bool{"DevPlatform Test Site": true}
}

func TestValidateRequest_AcceptsTheOneAllowedShape(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	if err := ValidateRequest(req, testAppcmdPath, testAllowedSites()); err != nil {
		t.Fatalf("expected a valid request to be accepted, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongProgram(t *testing.T) {
	req := Request{
		Name: `C:\Windows\System32\cmd.exe`,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong program, got: %v", err)
	}
}

func TestValidateRequest_RejectsUnknownSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "Some Other Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unlisted site, got: %v", err)
	}
}

func TestValidateRequest_RejectsRelativePhysicalPath(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", "/physicalPath:releases\\5"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a relative physical path, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongVerb(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"delete", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong verb, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongArgumentCount(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a short argument list, got: %v", err)
	}
}

func TestValidateRequest_RejectsSiteArgumentMissingTrailingSlash(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a site argument missing its trailing slash, got: %v", err)
	}
}

func TestValidateRequest_RejectsFourthArgumentMissingPhysicalPathPrefix(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a fourth argument missing the /physicalPath: prefix, got: %v", err)
	}
}
