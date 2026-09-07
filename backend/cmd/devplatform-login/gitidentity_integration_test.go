package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These exercise syncGitIdentity against a real `git config --global`
// rather than a stub, because the thing most likely to be wrong here is
// not the branching — it's whether the git invocations do what we think
// they do. A fake HOME keeps every write inside t.TempDir(): git reads
// and writes $HOME/.gitconfig for --global, so pointing HOME at a scratch
// directory means these tests can never touch the developer's own
// identity. USERPROFILE is set alongside it because Git for Windows falls
// back to it when HOME is absent.
func fakeGitHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// Guard the guard: if the override didn't take, every assertion
	// below would silently be reading and rewriting the real gitconfig.
	if err := setGitConfigGlobal("devplatform.probe", "1"); err != nil {
		t.Fatalf("failed to write into the fake HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatalf("git did not honour the HOME override — refusing to run against the real gitconfig: %v", err)
	}
	return home
}

// captureConsole stands in for the console *os.File that syncGitIdentity
// writes its messages to.
func captureConsole(t *testing.T) (*os.File, func() string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "console.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create console capture file: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f, func() string {
		f.Sync()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read console capture file: %v", err)
		}
		return string(b)
	}
}

func TestSyncGitIdentity_WritesTheIdentityOnAMachineThatHasNone(t *testing.T) {
	fakeGitHome(t)
	out, console := captureConsole(t)

	syncGitIdentity(out, bufio.NewScanner(strings.NewReader("")), session{
		Email:       "rifat.ozturk@sigortatahkim.org",
		DisplayName: "Rifat Öztürk",
	})

	if got := gitConfigGlobal("user.email"); got != "rifat.ozturk@sigortatahkim.org" {
		t.Errorf("user.email = %q, want the panel address", got)
	}
	if got := gitConfigGlobal("user.name"); got != "Rifat Öztürk" {
		t.Errorf("user.name = %q, want %q", got, "Rifat Öztürk")
	}
	// Nothing was taken away from anybody, so this case must not stop to
	// ask — that is the whole "log in and get on with it" promise.
	if body := console(); strings.Contains(body, "[E/h]") {
		t.Errorf("prompted about an identity that was never set: %s", body)
	}
}

func TestSyncGitIdentity_LeavesAMatchingIdentityAlone(t *testing.T) {
	fakeGitHome(t)
	if err := setGitConfigGlobal("user.name", "Elle Ayarlanmış İsim"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := setGitConfigGlobal("user.email", "rifat.ozturk@sigortatahkim.org"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	out, console := captureConsole(t)

	syncGitIdentity(out, bufio.NewScanner(strings.NewReader("")), session{
		Email:       "RIFAT.OZTURK@sigortatahkim.org",
		DisplayName: "Rifat Öztürk",
	})

	// A case-only difference is not a difference. Rewriting user.name
	// here would clobber a name somebody chose deliberately, for nothing.
	if got := gitConfigGlobal("user.name"); got != "Elle Ayarlanmış İsim" {
		t.Errorf("user.name = %q, want the name left untouched", got)
	}
	if body := console(); body != "" {
		t.Errorf("said something when there was nothing to do: %s", body)
	}
}

func TestSyncGitIdentity_SwitchesToThePanelAddressWhenTheAnswerIsYes(t *testing.T) {
	fakeGitHome(t)
	if err := setGitConfigGlobal("user.email", "rifatozturk061@gmail.com"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	out, console := captureConsole(t)

	// Bare Enter — the default answer, and the one this whole design is
	// built around somebody giving without thinking about it.
	syncGitIdentity(out, bufio.NewScanner(strings.NewReader("\n")), session{
		Email:       "rifat.ozturk@sigortatahkim.org",
		DisplayName: "Rifat Öztürk",
	})

	if got := gitConfigGlobal("user.email"); got != "rifat.ozturk@sigortatahkim.org" {
		t.Errorf("user.email = %q, want the panel address after answering yes", got)
	}
	body := console()
	if !strings.Contains(body, "rifatozturk061@gmail.com") {
		t.Errorf("did not name the address being replaced: %s", body)
	}
	if !strings.Contains(body, "rifat.ozturk@sigortatahkim.org") {
		t.Errorf("did not name the address being adopted: %s", body)
	}
}

func TestSyncGitIdentity_ClaimsTheExistingAddressWhenTheAnswerIsNo(t *testing.T) {
	fakeGitHome(t)
	if err := setGitConfigGlobal("user.email", "rifatozturk061@gmail.com"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	var claimed string
	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/git-emails" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		claimed = body["email"]
		w.WriteHeader(http.StatusOK)
	}))
	defer devplatform.Close()

	orig := devplatformBaseURL
	devplatformBaseURL = devplatform.URL
	defer func() { devplatformBaseURL = orig }()

	out, console := captureConsole(t)

	syncGitIdentity(out, bufio.NewScanner(strings.NewReader("h\n")), session{
		Email:       "rifat.ozturk@sigortatahkim.org",
		DisplayName: "Rifat Öztürk",
		jwt:         "dp-jwt",
	})

	// Saying no keeps their git config exactly as it was...
	if got := gitConfigGlobal("user.email"); got != "rifatozturk061@gmail.com" {
		t.Errorf("user.email = %q, want their own address left in place after answering no", got)
	}
	// ...but the address still gets bound to the panel account, so the
	// contribution graph fills either way. That is what makes "no" a
	// real answer rather than a dead end.
	if claimed != "rifatozturk061@gmail.com" {
		t.Errorf("claimed address = %q, want the address git actually signs with", claimed)
	}
	if body := console(); !strings.Contains(body, "bağlandı") {
		t.Errorf("did not tell them the address was linked: %s", body)
	}
}

func TestSyncGitIdentity_SurvivesAFailedClaim(t *testing.T) {
	fakeGitHome(t)
	if err := setGitConfigGlobal("user.email", "rifatozturk061@gmail.com"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer devplatform.Close()

	orig := devplatformBaseURL
	devplatformBaseURL = devplatform.URL
	defer func() { devplatformBaseURL = orig }()

	out, console := captureConsole(t)

	// This runs inside a `git push` that is waiting on a credential.
	// A server-side failure here must degrade into a message, not a panic
	// and not an exit.
	syncGitIdentity(out, bufio.NewScanner(strings.NewReader("h\n")), session{
		Email: "rifat.ozturk@sigortatahkim.org",
		jwt:   "dp-jwt",
	})

	if body := console(); !strings.Contains(body, "Hesabım") {
		t.Errorf("did not point them at the manual fallback: %s", body)
	}
}
