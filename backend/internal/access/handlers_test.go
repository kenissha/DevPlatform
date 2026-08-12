package access

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newHandlersMux(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /api/access", http.HandlerFunc(h.List))
	mux.Handle("PUT /api/access/{subject}", http.HandlerFunc(h.Set))
	mux.Handle("DELETE /api/access/{subject}", http.HandlerFunc(h.Clear))
	return mux
}

func TestHandlersSet_RestrictsTheSubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	h := &Handlers{Store: store}
	mux := newHandlersMux(h)

	body, _ := json.Marshal(map[string][]string{"repos": {"intranet-backend"}})
	req := httptest.NewRequest(http.MethodPut, "/api/access/dev-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	ok, err := store.CanAccess("dev-1", "intranet-backend")
	if err != nil || !ok {
		t.Errorf("CanAccess = %v, %v, want true, nil", ok, err)
	}
}

func TestHandlersSet_RejectsMalformedBody(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/access.json")}
	mux := newHandlersMux(h)

	req := httptest.NewRequest(http.MethodPut, "/api/access/dev-1", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandlersClear_RemovesTheRestriction(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}
	mux := newHandlersMux(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/access/dev-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	ok, err := store.CanAccess("dev-1", "any-repo")
	if err != nil || !ok {
		t.Errorf("CanAccess = %v, %v, want true, nil (unrestricted after Clear)", ok, err)
	}
}

func TestHandlersList_ReturnsOnlyRestrictedSubjects(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}
	mux := newHandlersMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/access", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || len(got["dev-1"]) != 1 || got["dev-1"][0] != "intranet-backend" {
		t.Errorf("got %v, want {dev-1: [intranet-backend]}", got)
	}
}
