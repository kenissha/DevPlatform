package secretsvault

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlers_Set_StoresTheSecret(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/secrets/sample/test", strings.NewReader(`{"content":"OAS_PASSWORD=hunter2"}`))
	req.SetPathValue("repo", "sample")
	req.SetPathValue("environment", "test")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got, err := store.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "OAS_PASSWORD=hunter2" {
		t.Errorf("stored content = %q, want %q", got, "OAS_PASSWORD=hunter2")
	}
}

func TestHandlers_Set_NeverEchoesTheContentBack(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/secrets/sample/test", strings.NewReader(`{"content":"OAS_PASSWORD=hunter2"}`))
	req.SetPathValue("repo", "sample")
	req.SetPathValue("environment", "test")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("response body echoed the secret content back: %s", rec.Body.String())
	}
}

func TestHandlers_Set_RejectsMissingRepo(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/secrets//test", strings.NewReader(`{"content":"x"}`))
	req.SetPathValue("repo", "")
	req.SetPathValue("environment", "test")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlers_Set_RejectsMalformedBody(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/secrets/sample/test", strings.NewReader(`not json`))
	req.SetPathValue("repo", "sample")
	req.SetPathValue("environment", "test")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlers_Set_ReturnsServiceUnavailableWhenVaultNotConfigured(t *testing.T) {
	h := &Handlers{Store: nil}

	req := httptest.NewRequest(http.MethodPut, "/api/secrets/sample/test", strings.NewReader(`{"content":"x"}`))
	req.SetPathValue("repo", "sample")
	req.SetPathValue("environment", "test")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
