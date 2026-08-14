package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFrontendFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>panel</html>"), 0o600); err != nil {
		t.Fatalf("failed to write index.html fixture: %v", err)
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.Mkdir(assetsDir, 0o750); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app.js"), []byte("console.log('app')"), 0o600); err != nil {
		t.Fatalf("failed to write app.js fixture: %v", err)
	}
	return dir
}

func TestFrontendHandler_ServesARealFileDirectly(t *testing.T) {
	handler := frontendHandler(newFrontendFixture(t))

	req := httptest.NewRequest("GET", "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "console.log") {
		t.Fatalf("expected the real asset file's content, got %q", got)
	}
}

func TestFrontendHandler_FallsBackToIndexHTMLForAClientSideRoute(t *testing.T) {
	handler := frontendHandler(newFrontendFixture(t))

	req := httptest.NewRequest("GET", "/repos/some-repo/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200 (index.html fallback), got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "panel") {
		t.Fatalf("expected index.html's content as the fallback, got %q", got)
	}
}

func TestFrontendHandler_ServesIndexHTMLAtRoot(t *testing.T) {
	handler := frontendHandler(newFrontendFixture(t))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "panel") {
		t.Fatalf("expected index.html's content at root, got %q", got)
	}
}
