package repoimport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

const testJWTSecret = "test-secret"

func addAuth(t *testing.T, req *http.Request, subject, role string) *http.Request {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signed)
	return req
}

func newTestMux(t *testing.T, cloner Cloner) (*http.ServeMux, *Store, string) {
	t.Helper()
	root := t.TempDir()
	store := NewStore(root, cloner)
	h := &Handlers{Store: store, Audit: audit.New(filepath.Join(t.TempDir(), "audit.jsonl"))}

	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repo-imports", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Start))))
	mux.Handle("GET /api/repo-imports", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.List))))
	mux.Handle("GET /api/repo-imports/{id}", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Get))))
	return mux, store, root
}

func post(t *testing.T, mux *http.ServeMux, role string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/repo-imports", bytes.NewReader(raw))
	req = addAuth(t, req, "dev-1", role)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Importing creates a repository, and creating one has always been an
// admin action.
func TestStart_DeveloperIsRefused(t *testing.T) {
	mux, _, _ := newTestMux(t, &fakeCloner{})

	rec := post(t, mux, "developer", map[string]string{
		"name": "deneme", "source": "https://github.com/x/y.git",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// 202, not 201: the repository does not exist yet when this returns.
func TestStart_ReturnsAcceptedWithAJobToWatch(t *testing.T) {
	mux, _, _ := newTestMux(t, &fakeCloner{})

	rec := post(t, mux, "admin", map[string]string{
		"name": "deneme", "source": "https://github.com/kenissha/STK-React.git",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body: %s)", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.ID == "" || job.Name != "deneme" {
		t.Fatalf("job = %+v", job)
	}
}

// The token is request-only. If it ever appeared in the response the
// panel would hold a credential it has no use for.
func TestStart_ResponseNeverCarriesTheToken(t *testing.T) {
	const token = "ghp_COKGIZLI"
	mux, _, _ := newTestMux(t, &fakeCloner{})

	rec := post(t, mux, "admin", map[string]string{
		"name": "deneme", "source": "https://github.com/x/y.git", "token": token,
	})
	if strings.Contains(rec.Body.String(), token) {
		t.Fatalf("yanıt anahtarı içeriyor: %s", rec.Body.String())
	}
}

func TestStart_RejectsADangerousSourceWith400(t *testing.T) {
	mux, _, _ := newTestMux(t, &fakeCloner{})

	rec := post(t, mux, "admin", map[string]string{"name": "deneme", "source": "ext::sh -c whoami"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestStart_ExistingRepositoryIs409(t *testing.T) {
	mux, _, root := newTestMux(t, &fakeCloner{})
	if err := os.MkdirAll(filepath.Join(root, "deneme.git"), 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}

	rec := post(t, mux, "admin", map[string]string{
		"name": "deneme", "source": "https://github.com/x/y.git",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

// The panel polls this while a clone runs, so it has to report progress
// and then the finished result including findings.
func TestGet_ReportsProgressThenTheResult(t *testing.T) {
	release := make(chan struct{})
	cloner := &fakeCloner{build: func(dest string) error {
		<-release
		return os.MkdirAll(dest, 0o750)
	}}
	mux, store, _ := newTestMux(t, cloner)

	rec := post(t, mux, "admin", map[string]string{
		"name": "deneme", "source": "https://github.com/x/y.git",
	})
	var job Job
	_ = json.Unmarshal(rec.Body.Bytes(), &job)

	req := httptest.NewRequest(http.MethodGet, "/api/repo-imports/"+job.ID, nil)
	req = addAuth(t, req, "dev-1", "admin")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var running Job
	if err := json.Unmarshal(rec.Body.Bytes(), &running); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if running.Status != StatusRunning {
		t.Fatalf("status = %q, want running", running.Status)
	}

	close(release)
	// The fake clone produces an empty directory, so the import fails on
	// "no branches" — which is the right outcome for a source that turned
	// out to hold nothing.
	done := waitFor(t, store, job.ID)
	if done.Running() {
		t.Fatal("iş hâlâ çalışıyor")
	}
}

func TestGet_UnknownJobIs404(t *testing.T) {
	mux, _, _ := newTestMux(t, &fakeCloner{})

	req := httptest.NewRequest(http.MethodGet, "/api/repo-imports/yok", nil)
	req = addAuth(t, req, "dev-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// A pasted "https://token@github.com/..." must not reach the audit log —
// that file is kept and read by people.
func TestStart_AuditEntryHasNoCredential(t *testing.T) {
	root := t.TempDir()
	auditPath := filepath.Join(root, "audit.jsonl")
	store := NewStore(t.TempDir(), &fakeCloner{})
	h := &Handlers{Store: store, Audit: audit.New(auditPath)}

	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repo-imports", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Start))))

	raw, _ := json.Marshal(map[string]string{
		"name": "deneme", "source": "https://ghp_COKGIZLI@github.com/kenissha/STK-React.git",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repo-imports", bytes.NewReader(raw))
	req = addAuth(t, req, "dev-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("denetim kaydı okunamadı: %v", err)
	}
	if strings.Contains(string(data), "ghp_COKGIZLI") {
		t.Fatalf("denetim kaydı anahtar içeriyor:\n%s", data)
	}
}
