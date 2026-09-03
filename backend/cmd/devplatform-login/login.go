package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// intranetBaseURL and devplatformBaseURL are vars, not consts, so
// tests can point them at an httptest.Server — the same seam pattern
// internal/deploy/versionstore.go uses for time.Now.
var (
	intranetBaseURL    = "https://intranet.sigortatahkim.org"
	devplatformBaseURL = "https://git.sigortatahkim.org"
)

// login runs the 3-step exchange (Intranet-B login -> devplatform-sso
// -> DevPlatform git-token) and returns the resulting subject and git
// token. The AD password is only ever held in this process's memory —
// never written to disk.
func login(username, password string) (subject, token string, err error) {
	intranetJWT, err := intranetLogin(username, password)
	if err != nil {
		return "", "", fmt.Errorf("intranet girişi başarısız: %w", err)
	}

	devplatformJWT, err := devplatformSSO(intranetJWT)
	if err != nil {
		return "", "", fmt.Errorf("devplatform yetkisi alınamadı: %w", err)
	}

	_, gitToken, err := mintGitToken(devplatformJWT, hostLabel())
	if err != nil {
		return "", "", fmt.Errorf("git anahtarı alınamadı: %w", err)
	}

	subject, err = jwtSubject(devplatformJWT)
	if err != nil {
		return "", "", fmt.Errorf("devplatform token'ı okunamadı: %w", err)
	}
	return subject, gitToken, nil
}

func intranetLogin(username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	resp, err := http.Post(intranetBaseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("intranet girişi %d döndü — kullanıcı adı/şifre hatalı olabilir", resp.StatusCode)
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
	resp, err := http.DefaultClient.Do(req)
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
	resp, err := http.DefaultClient.Do(req)
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
