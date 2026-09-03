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
		if r.URL.Path != "/api/me/git-token" {
			t.Errorf("unexpected devplatform request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+devplatformJWT {
			t.Errorf("git-token Authorization = %q, want Bearer %s", got, devplatformJWT)
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "abc", "token": "final-git-token"})
	}))
	defer devplatform.Close()

	origIntranet, origDevplatform := intranetBaseURL, devplatformBaseURL
	intranetBaseURL, devplatformBaseURL = intranet.URL, devplatform.URL
	defer func() { intranetBaseURL, devplatformBaseURL = origIntranet, origDevplatform }()

	subject, token, err := login("rifat", "sifre123")
	if err != nil {
		t.Fatalf("login returned error: %v", err)
	}
	if subject != "dev-1" {
		t.Errorf("subject = %q, want %q", subject, "dev-1")
	}
	if token != "final-git-token" {
		t.Errorf("token = %q, want %q", token, "final-git-token")
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

	_, _, err := login("rifat", "yanlis-sifre")
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

	_, _, err := login("rifat", "yanlis-sifre")
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

	_, _, err := login("rifat", "sifre123")
	if err == nil {
		t.Fatal("login returned no error for a 500 from intranet login")
	}
	if errors.Is(err, ErrBadCredentials) {
		t.Error("errors.Is(err, ErrBadCredentials) = true for a 500 — a retry wouldn't fix a server error, only a 401 should trigger one")
	}
}
