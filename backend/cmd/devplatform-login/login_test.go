package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func signTestJWT(t *testing.T, subject string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": subject})
	s, err := tok.SignedString([]byte("irrelevant-since-unverified"))
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	return s
}

func TestLogin_ChainsAllThreeCallsAndReturnsTheGitToken(t *testing.T) {
	devplatformJWT := signTestJWT(t, "dev-1")

	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["Username"] != "rifat" || body["Password"] != "sifre123" {
				t.Errorf("login body = %v, want rifat/sifre123", body)
			}
			json.NewEncoder(w).Encode(map[string]string{"token": "intranet-jwt"})
		case "/api/auth/devplatform-sso":
			if got := r.Header.Get("Authorization"); got != "Bearer intranet-jwt" {
				t.Errorf("devplatform-sso Authorization = %q, want Bearer intranet-jwt", got)
			}
			json.NewEncoder(w).Encode(map[string]string{"token": devplatformJWT})
		default:
			t.Errorf("unexpected intranet request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer intranet.Close()

	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+devplatformJWT {
			t.Errorf("%s Authorization = %q, want Bearer %s", r.URL.Path, got, devplatformJWT)
		}
		switch r.URL.Path {
		case "/api/me/git-token":
			json.NewEncoder(w).Encode(map[string]string{"id": "abc", "token": "final-git-token"})
		case "/api/me":
			json.NewEncoder(w).Encode(map[string]string{
				"email":       "rifat.ozturk@sigortatahkim.org",
				"displayName": "Rifat Öztürk",
			})
		default:
			t.Errorf("unexpected devplatform request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer devplatform.Close()

	origIntranet, origDevplatform := intranetBaseURL, devplatformBaseURL
	intranetBaseURL, devplatformBaseURL = intranet.URL, devplatform.URL
	defer func() { intranetBaseURL, devplatformBaseURL = origIntranet, origDevplatform }()

	s, err := login("rifat", "sifre123")
	if err != nil {
		t.Fatalf("login returned error: %v", err)
	}
	if s.Subject != "dev-1" {
		t.Errorf("subject = %q, want %q", s.Subject, "dev-1")
	}
	if s.Token != "final-git-token" {
		t.Errorf("token = %q, want %q", s.Token, "final-git-token")
	}
	if s.Email != "rifat.ozturk@sigortatahkim.org" {
		t.Errorf("email = %q, want the address /api/me returned — syncGitIdentity has nothing to work with without it", s.Email)
	}
	if s.DisplayName != "Rifat Öztürk" {
		t.Errorf("displayName = %q, want %q", s.DisplayName, "Rifat Öztürk")
	}
	if s.jwt != devplatformJWT {
		t.Errorf("jwt = %q, want the devplatform JWT — claimGitEmail authenticates with it", s.jwt)
	}
}

// The identity lookup is a convenience layered on top of the credential
// exchange. If it breaks, the person still has to be able to push.
func TestLogin_StillReturnsTheCredentialWhenTheIdentityLookupFails(t *testing.T) {
	devplatformJWT := signTestJWT(t, "dev-1")

	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "intranet-jwt"})
		case "/api/auth/devplatform-sso":
			json.NewEncoder(w).Encode(map[string]string{"token": devplatformJWT})
		}
	}))
	defer intranet.Close()

	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me/git-token":
			json.NewEncoder(w).Encode(map[string]string{"id": "abc", "token": "final-git-token"})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer devplatform.Close()

	origIntranet, origDevplatform := intranetBaseURL, devplatformBaseURL
	intranetBaseURL, devplatformBaseURL = intranet.URL, devplatform.URL
	defer func() { intranetBaseURL, devplatformBaseURL = origIntranet, origDevplatform }()

	s, err := login("rifat", "sifre123")
	if err != nil {
		t.Fatalf("login failed because /api/me did: %v — a git push must not depend on the identity lookup", err)
	}
	if s.Token != "final-git-token" {
		t.Errorf("token = %q, want %q", s.Token, "final-git-token")
	}
	if s.Email != "" {
		t.Errorf("email = %q, want empty — syncGitIdentity uses that to know it should do nothing", s.Email)
	}
}

func TestClaimGitEmail_PostsTheAddressAsTheLoggedInUser(t *testing.T) {
	var gotAuth, gotEmail string
	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/git-emails" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		gotEmail = body["email"]
		w.WriteHeader(http.StatusOK)
	}))
	defer devplatform.Close()

	orig := devplatformBaseURL
	devplatformBaseURL = devplatform.URL
	defer func() { devplatformBaseURL = orig }()

	if err := claimGitEmail("dp-jwt", "rifatozturk061@gmail.com"); err != nil {
		t.Fatalf("claimGitEmail returned error: %v", err)
	}
	if gotAuth != "Bearer dp-jwt" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer dp-jwt")
	}
	if gotEmail != "rifatozturk061@gmail.com" {
		t.Errorf("posted email = %q, want %q", gotEmail, "rifatozturk061@gmail.com")
	}
}

func TestClaimGitEmail_ReportsAServerRejection(t *testing.T) {
	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "400 geçerli bir e-posta adresi girin", http.StatusBadRequest)
	}))
	defer devplatform.Close()

	orig := devplatformBaseURL
	devplatformBaseURL = devplatform.URL
	defer func() { devplatformBaseURL = orig }()

	if err := claimGitEmail("dp-jwt", "not-an-address"); err == nil {
		t.Fatal("claimGitEmail returned no error for a 400")
	}
}

func TestLogin_ReturnsAClearErrorOn403FromDevplatformSSO(t *testing.T) {
	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "intranet-jwt"})
		case "/api/auth/devplatform-sso":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer intranet.Close()

	origIntranet := intranetBaseURL
	intranetBaseURL = intranet.URL
	defer func() { intranetBaseURL = origIntranet }()

	_, err := login("rifat", "yanlis-sifre")
	if err == nil {
		t.Fatal("login returned no error for a 403 from devplatform-sso")
	}
}

func TestLogin_ReturnsAClearErrorOnBadIntranetCredentials(t *testing.T) {
	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer intranet.Close()

	origIntranet := intranetBaseURL
	intranetBaseURL = intranet.URL
	defer func() { intranetBaseURL = origIntranet }()

	_, err := login("rifat", "yanlis-sifre")
	if err == nil {
		t.Fatal("login returned no error for a 401 from intranet login")
	}
	if !errors.Is(err, ErrBadCredentials) {
		t.Errorf("errors.Is(err, ErrBadCredentials) = false, want true — promptAndLogin's retry depends on this: err = %v", err)
	}
}

func TestLogin_DoesNotMarkOtherIntranetFailuresAsBadCredentials(t *testing.T) {
	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer intranet.Close()

	origIntranet := intranetBaseURL
	intranetBaseURL = intranet.URL
	defer func() { intranetBaseURL = origIntranet }()

	_, err := login("rifat", "sifre123")
	if err == nil {
		t.Fatal("login returned no error for a 500 from intranet login")
	}
	if errors.Is(err, ErrBadCredentials) {
		t.Error("errors.Is(err, ErrBadCredentials) = true for a 500 — a retry wouldn't fix a server error, only a 401 should trigger one")
	}
}
