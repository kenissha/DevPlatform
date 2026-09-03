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

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login.exe", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login.exe", nil)
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDownload_ConfiguredButMissingFileReturns404(t *testing.T) {
	h := &Handlers{Path: filepath.Join(t.TempDir(), "does-not-exist.exe")}

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login.exe", nil)
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestInstallScript_PrefersBaseURLOverTheRequestsHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devplatform-login.exe")
	if err := os.WriteFile(path, []byte("fake exe bytes"), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	// This is the real production shape: the process sits behind IIS's
	// reverse proxy and only ever sees an internal loopback address as
	// the request's own Host — BaseURL must win, or the generated
	// script tries to download from an address only reachable on the
	// server itself.
	h := &Handlers{Path: path, BaseURL: "https://git.sigortatahkim.org"}

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login/install.ps1", nil)
	req.Host = "127.0.0.1:8082"
	rec := httptest.NewRecorder()
	h.InstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://git.sigortatahkim.org/api/devplatform-login.exe") {
		t.Errorf("script body does not reference BaseURL's download URL: %s", body)
	}
	if strings.Contains(body, "127.0.0.1:8082") {
		t.Errorf("script body leaks the internal loopback host instead of using BaseURL: %s", body)
	}
	if !strings.Contains(body, "install") {
		t.Errorf("script body does not run the install command: %s", body)
	}
}

func TestInstallScript_FallsBackToTheRequestsHostWhenBaseURLIsUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devplatform-login.exe")
	if err := os.WriteFile(path, []byte("fake exe bytes"), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	h := &Handlers{Path: path}

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login/install.ps1", nil)
	req.Host = "localhost:8080"
	rec := httptest.NewRecorder()
	h.InstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://localhost:8080/api/devplatform-login.exe") {
		t.Errorf("script body does not fall back to the request's own host: %s", body)
	}
}

func TestInstallScript_NotConfiguredReturns404(t *testing.T) {
	h := &Handlers{Path: ""}

	req := httptest.NewRequest(http.MethodGet, "/api/devplatform-login/install.ps1", nil)
	rec := httptest.NewRecorder()
	h.InstallScript(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
