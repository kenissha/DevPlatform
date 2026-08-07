package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if body := rec.Body.String(); body != `{"status":"ok"}`+"\n" {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`+"\n")
	}
}
