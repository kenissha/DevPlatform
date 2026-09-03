package logincli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownload_ServesTheConfiguredBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devplatform-login.exe")
	if err := os.WriteFile(path, []byte("fake exe bytes"), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	h := &Handlers{Path: path}

	req := httptest.NewRequest(http.MethodGet, "/devplatform-login.exe", nil)
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fake exe bytes" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fake exe bytes")
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "devplatform-login.exe") {
		t.Errorf("Content-Disposition = %q, want it to name devplatform-login.exe", got)
	}
}

func TestDownload_NotConfiguredReturns404(t *testing.T) {
	h := &Handlers{Path: ""}

	req := httptest.NewRequest(http.MethodGet, "/devplatform-login.exe", nil)
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDownload_ConfiguredButMissingFileReturns404(t *testing.T) {
	h := &Handlers{Path: filepath.Join(t.TempDir(), "does-not-exist.exe")}

	req := httptest.NewRequest(http.MethodGet, "/devplatform-login.exe", nil)
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestInstallScript_ReferencesTheRequestsOwnHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devplatform-login.exe")
	if err := os.WriteFile(path, []byte("fake exe bytes"), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	h := &Handlers{Path: path}

	req := httptest.NewRequest(http.MethodGet, "/devplatform-login/install.ps1", nil)
	req.Host = "git.sigortatahkim.org"
	rec := httptest.NewRecorder()
	h.InstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://git.sigortatahkim.org/devplatform-login.exe") {
		t.Errorf("script body does not reference the download URL for the request's own host: %s", body)
	}
	if !strings.Contains(body, "install") {
		t.Errorf("script body does not run the install command: %s", body)
	}
}

func TestInstallScript_NotConfiguredReturns404(t *testing.T) {
	h := &Handlers{Path: ""}

	req := httptest.NewRequest(http.MethodGet, "/devplatform-login/install.ps1", nil)
	rec := httptest.NewRecorder()
	h.InstallScript(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
