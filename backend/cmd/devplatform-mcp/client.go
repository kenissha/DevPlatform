package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiClient talks to DevPlatform's REST API as the person who logged in
// with devplatform-login.
//
// Authentication is HTTP Basic with the cached subject and git token —
// see internal/apiauth for why that credential and not a new one. Nothing
// here mints, stores, or prints a credential; it reads the one already on
// the machine and sends it.
type apiClient struct {
	baseURL string
	subject string
	token   string
	http    *http.Client
}

func newAPIClient(baseURL, subject, token string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		subject: subject,
		token:   token,
		// Long enough for a slow intranet round trip, short enough that a
		// wedged request surfaces as an error the model can report rather
		// than a tool call that never returns. A hung tool call is worse
		// than a failed one: the session simply stops.
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// do performs one API call and decodes the JSON body into out (which may
// be nil for calls whose body is not needed).
func (c *apiClient) do(method, path string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, c.baseURL+path, payload)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.subject, c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("DevPlatform'a ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.describeFailure(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// describeFailure turns an HTTP failure into something the model can act
// on rather than relay verbatim.
//
// The status alone is not enough: a 401 here almost always means the
// cached login expired, and the useful answer is "run devplatform-login",
// not "401 Unauthorized". The API's own message is included when there is
// one, because the task endpoints answer in readable Turkish already
// ("400 geçersiz etiket") and re-wording that would only lose detail.
func (c *apiClient) describeFailure(resp *http.Response) error {
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(detail))

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("DevPlatform girişi geçersiz veya süresi dolmuş — terminalde devplatform-login çalıştır")
	case http.StatusForbidden:
		return fmt.Errorf("bu işlem için yetkin yok: %s", message)
	case http.StatusNotFound:
		return fmt.Errorf("bulunamadı: %s", message)
	}
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("DevPlatform hatası (%d): %s", resp.StatusCode, message)
}

func (c *apiClient) get(path string, out any) error {
	return c.do(http.MethodGet, path, nil, out)
}

// repoPath builds a per-repository path with the name escaped. The name
// reaches here from the model, so it is never interpolated raw: a value
// containing a slash would otherwise address a different endpoint
// entirely.
func repoPath(repo string, suffix ...string) string {
	path := "/api/repos/" + url.PathEscape(repo)
	for _, part := range suffix {
		path += "/" + part
	}
	return path
}
