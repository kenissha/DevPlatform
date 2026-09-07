package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrBadCredentials marks intranetLogin's specific "wrong username or
// password" outcome (a 401 from Intranet-B) so callers — namely
// promptAndLogin's one-retry — can distinguish it from every other
// failure in the chain (network errors, Intranet-B down, not
// authorized for DevPlatform, ...), which retrying with the same
// password can't fix and shouldn't be retried blindly.
var ErrBadCredentials = errors.New("kullanıcı adı/şifre hatalı olabilir")

// intranetBaseURL and devplatformBaseURL are vars, not consts, so
// tests can point them at an httptest.Server — the same seam pattern
// internal/deploy/versionstore.go uses for time.Now. Intranet-B's API
// listens on :8443, not the default HTTPS port — confirmed live
// (2026-09-03) after the design's assumed bare-443 URL 404'd against
// the real server.
var (
	intranetBaseURL    = "https://intranet.sigortatahkim.org:8443"
	devplatformBaseURL = "https://git.sigortatahkim.org"
)

// httpClient has an explicit timeout — git invokes this whole login
// chain synchronously from inside a credential-helper call and blocks
// on it, so an unreachable-but-not-refusing Intranet-B (VPN drop, a
// load balancer that accepts the connection and never responds) would
// otherwise hang `git clone`/`pull`/`push` forever with no output and
// no way to tell what's wrong.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// session is everything one successful login produces: the credential
// git itself asked for, plus the panel identity behind it.
//
// Email and DisplayName exist so the login can also settle this
// machine's git identity (see gitidentity.go). Without them a person
// logs in successfully and their commits still get stamped with
// whatever address happened to sit in their global gitconfig, leaving
// the contribution graph empty for no reason they can see.
type session struct {
	Subject     string
	Token       string
	Email       string
	DisplayName string
	// jwt is the DevPlatform session token. Held in this process's
	// memory only — never written to the credential cache — so identity
	// follow-ups (claiming a git address) can act as this person
	// without a second password prompt.
	jwt string
}

// login runs the exchange (Intranet-B login -> devplatform-sso ->
// DevPlatform git-token -> /api/me) and returns the resulting session.
// The AD password is only ever held in this process's memory — never
// written to disk.
func login(username, password string) (session, error) {
	// intranetLogin and devplatformSSO's own errors are already
	// complete, staged Turkish messages (they name which step failed
	// and why) — wrapping them again here just repeats "giriş"/
	// "girişi" back to back for no added information. mintGitToken and
	// jwtSubject's inner errors are comparatively raw/technical, so
	// those two DO still get a Turkish wrapper naming the stage.
	intranetJWT, err := intranetLogin(username, password)
	if err != nil {
		return session{}, err
	}

	devplatformJWT, err := devplatformSSO(intranetJWT)
	if err != nil {
		return session{}, err
	}

	_, gitToken, err := mintGitToken(devplatformJWT, hostLabel())
	if err != nil {
		return session{}, fmt.Errorf("git anahtarı alınamadı: %w", err)
	}

	subject, err := jwtSubject(devplatformJWT)
	if err != nil {
		return session{}, fmt.Errorf("devplatform oturum bilgisi okunamadı: %w", err)
	}

	s := session{Subject: subject, Token: gitToken, jwt: devplatformJWT}

	// The identity lookup is the one step in this chain allowed to fail
	// quietly. Everything above it produces the credential git is
	// blocking on; this only decides whether we can also tidy up the git
	// config, and somebody whose push is waiting shouldn't be stopped by
	// a cosmetic follow-up call.
	email, displayName, err := fetchMe(devplatformJWT)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: uyarı: panel kimliği okunamadı: %v\n", err)
		return s, nil
	}
	s.Email, s.DisplayName = email, displayName
	return s, nil
}

func intranetLogin(username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	resp, err := httpClient.Post(intranetBaseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("intranet girişi %d döndü: %w", resp.StatusCode, ErrBadCredentials)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("intranet girişi %d döndü", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("intranet girişi bir token döndürmedi")
	}
	return parsed.Token, nil
}

func devplatformSSO(intranetJWT string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, intranetBaseURL+"/api/auth/devplatform-sso", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+intranetJWT)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("bu hesaba DevPlatform yetkisi verilmemiş (403) — admin panelinden yetki verilmesi gerekiyor")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("devplatform-sso %d döndü", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("devplatform-sso bir token döndürmedi")
	}
	return parsed.Token, nil
}

func mintGitToken(devplatformJWT, label string) (id, token string, err error) {
	body, _ := json.Marshal(map[string]string{"label": label})
	req, err := http.NewRequest(http.MethodPost, devplatformBaseURL+"/api/me/git-token", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+devplatformJWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("git-token %d döndü: %s", resp.StatusCode, respBody)
	}
	var parsed struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", err
	}
	return parsed.ID, parsed.Token, nil
}

// fetchMe reads the caller's panel identity from /api/me — the same
// endpoint the panel header uses, so the address and name written into
// git config are exactly the ones the person sees on screen.
func fetchMe(devplatformJWT string) (email, displayName string, err error) {
	req, err := http.NewRequest(http.MethodGet, devplatformBaseURL+"/api/me", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+devplatformJWT)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("/api/me %d döndü", resp.StatusCode)
	}
	var parsed struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", err
	}
	return parsed.Email, parsed.DisplayName, nil
}

// claimGitEmail binds an address this machine already commits with to
// the logged-in panel account, so the contribution graph counts those
// commits. Deliberately the same endpoint the panel's "Hesabım" page
// posts to — there is no CLI-only path into the store.
func claimGitEmail(devplatformJWT, email string) error {
	if devplatformJWT == "" {
		return fmt.Errorf("oturum bilgisi yok")
	}
	body, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequest(http.MethodPost, devplatformBaseURL+"/api/me/git-emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+devplatformJWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("git-emails %d döndü: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return nil
}

// jwtSubject reads the "sub" claim out of a JWT without verifying its
// signature — safe here because this JWT was just received directly
// from DevPlatform itself over HTTPS a moment ago, not supplied by an
// untrusted caller.
func jwtSubject(tokenString string) (string, error) {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(tokenString, claims); err != nil {
		return "", err
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", fmt.Errorf("token has no sub claim")
	}
	return sub, nil
}

// hostLabel names the token after this machine, so the "Hesabım" list
// shows which device each active token belongs to. Falls back to a
// generic label if the hostname can't be read — that's a cosmetic
// detail, not worth failing the whole login over.
func hostLabel() string {
	name, err := os.Hostname()
	if err != nil {
		return "CLI"
	}
	return "CLI - " + name
}
