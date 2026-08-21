package displaynames

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlers_List_ReturnsEveryOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodGet, "/api/display-names", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Rifat") {
		t.Errorf("body = %q, want it to contain the override", rec.Body.String())
	}
}

func TestHandlers_Set_StoresTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/display-names/dev-1", strings.NewReader(`{"name":"Rifat Öztürk"}`))
	req.SetPathValue("subject", "dev-1")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := store.Get("dev-1", "fallback"); got != "Rifat Öztürk" {
		t.Errorf("stored name = %q, want %q", got, "Rifat Öztürk")
	}
}

func TestHandlers_Set_RejectsMissingSubject(t *testing.T) {
	h := &Handlers{Store: NewStore(filepath.Join(t.TempDir(), "display-names.json"))}

	req := httptest.NewRequest(http.MethodPut, "/api/display-names/", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlers_Clear_RemovesTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodDelete, "/api/display-names/dev-1", nil)
	req.SetPathValue("subject", "dev-1")
	rec := httptest.NewRecorder()
	h.Clear(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := store.Get("dev-1", "fallback"); got != "fallback" {
		t.Errorf("Get after Clear = %q, want fallback", got)
	}
}
