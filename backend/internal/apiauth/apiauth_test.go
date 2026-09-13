package apiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

const secret = "test-secret"

// fakeTokens accepts exactly one subject/token pair.
type fakeTokens struct{ subject, token string }

func (f fakeTokens) Verify(subject, token string) bool {
	return subject == f.subject && token == f.token
}

type fakePeople map[string]users.User

func (p fakePeople) Get(subject string) (users.User, bool, error) {
	u, ok := p[subject]
	return u, ok, nil
}

type erroringPeople struct{}

func (erroringPeople) Get(string) (users.User, bool, error) {
	return users.User{}, false, http.ErrServerClosed
}

// seen records the user the wrapped handler was given, so a test can
// assert the context is populated identically whichever credential was
// used — the whole point of the package.
type seen struct {
	user auth.User
	ok   bool
}

func newHandler(t *testing.T, tokens TokenVerifier, people PeopleStore) (http.Handler, *seen) {
	t.Helper()
	got := &seen{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if ok {
			got.user, got.ok = *u, true
		}
		w.WriteHeader(http.StatusOK)
	})
	return Require([]byte(secret), tokens, people, inner), got
}

func signedJWT(t *testing.T, subject, role string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   subject,
		"role":  role,
		"email": subject + "@localhost",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestBearerJWTStillWorks(t *testing.T) {
	h, got := newHandler(t, fakeTokens{"dev-1", "tok"}, fakePeople{})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+signedJWT(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !got.ok || got.user.Subject != "dev-1" {
		t.Fatalf("user = %+v", got.user)
	}
}

// The reason the package exists: a git token reaches the same handler
// with the same user attached.
func TestGitTokenIsAcceptedAsTheSameUser(t *testing.T) {
	people := fakePeople{"dev-1": {Subject: "dev-1", Email: "dev-1@localhost", Role: "developer"}}
	h, got := newHandler(t, fakeTokens{"dev-1", "tok"}, people)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.user.Subject != "dev-1" || got.user.Email != "dev-1@localhost" {
		t.Fatalf("user = %+v", got.user)
	}
	if got.user.Role != auth.RoleDeveloper {
		t.Fatalf("role = %q, want developer", got.user.Role)
	}
}

func TestGitTokenCarriesTheAdminRoleFromTheRegistry(t *testing.T) {
	people := fakePeople{"boss": {Subject: "boss", Role: string(auth.RoleAdmin)}}
	h, got := newHandler(t, fakeTokens{"boss", "tok"}, people)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("boss", "tok")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.user.Role != auth.RoleAdmin {
		t.Fatalf("role = %q, want admin", got.user.Role)
	}
}

// An unrecognised registry value must not become a privilege. Guessing
// upward is the one mistake here that cannot be walked back.
func TestUnknownRoleIsTreatedAsDeveloper(t *testing.T) {
	people := fakePeople{"dev-1": {Subject: "dev-1", Role: "super-yonetici"}}
	h, got := newHandler(t, fakeTokens{"dev-1", "tok"}, people)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", "tok")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.user.Role != auth.RoleDeveloper {
		t.Fatalf("role = %q, want developer", got.user.Role)
	}
}

// Somebody with a valid token who has never opened the panel still gets
// in — as a developer. The token is the proof of identity; the registry
// only supplies the role.
func TestUnknownSubjectWithAValidTokenIsADeveloper(t *testing.T) {
	h, got := newHandler(t, fakeTokens{"yeni", "tok"}, fakePeople{})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("yeni", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.user.Subject != "yeni" || got.user.Role != auth.RoleDeveloper {
		t.Fatalf("user = %+v", got.user)
	}
}

func TestWrongTokenIs401(t *testing.T) {
	h, got := newHandler(t, fakeTokens{"dev-1", "tok"}, fakePeople{})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", "yanlis")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got.ok {
		t.Fatal("handler'a ulasti")
	}
}

// The two schemes must not leak into each other: a failed Basic must not
// get a second chance as a Bearer, and vice versa. Otherwise a failure in
// one path becomes an oracle for probing the other.
func TestSchemesDoNotFallThroughToEachOther(t *testing.T) {
	h, _ := newHandler(t, fakeTokens{"dev-1", "tok"}, fakePeople{})

	// A valid JWT sent as a Basic password must fail - it is not a token.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", signedJWT(t, "dev-1", "admin"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Basic icinde JWT kabul edildi: %d", rec.Code)
	}

	// A valid token sent as a Bearer must fail - it is not a JWT.
	req = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Bearer icinde anahtar kabul edildi: %d", rec.Code)
	}
}

func TestNoCredentialIs401(t *testing.T) {
	h, _ := newHandler(t, fakeTokens{"dev-1", "tok"}, fakePeople{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A deployment that never wired the token store stays JWT-only rather
// than silently accepting anything.
func TestNilTokenStoreLeavesTheRouteJWTOnly(t *testing.T) {
	h, _ := newHandler(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Basic status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+signedJWT(t, "dev-1", "developer"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Bearer status = %d, want 200", rec.Code)
	}
}

// A registry read that fails must not quietly downgrade the caller to a
// developer - it is a server fault, and answering 200 would hide it.
func TestRegistryFailureIs500NotADowngrade(t *testing.T) {
	h, got := newHandler(t, fakeTokens{"boss", "tok"}, erroringPeople{})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("boss", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got.ok {
		t.Fatal("handler'a ulasti")
	}
}

// A failed token gets the challenge header, as git's own endpoints do.
func TestUnauthorizedCarriesTheBasicChallenge(t *testing.T) {
	h, _ := newHandler(t, fakeTokens{"dev-1", "tok"}, fakePeople{})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.SetBasicAuth("dev-1", "yanlis")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("WWW-Authenticate yok")
	}
}
