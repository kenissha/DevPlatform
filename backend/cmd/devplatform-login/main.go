package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: devplatform-login <get|store|erase|install>")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "get":
		runGet()
	case "store":
		runStore()
	case "erase":
		runErase()
	case "install":
		runInstall()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

// drainProtocolInput reads and discards git's credential-protocol
// input on stdin (key=value lines, terminated by EOF) — this tool is
// only ever configured for one host (git.sigortatahkim.org, via
// `install`'s git-config), so there is nothing in the input worth
// inspecting; it still has to be read so git's pipe to us doesn't
// block.
func drainProtocolInput() {
	io.Copy(io.Discard, os.Stdin)
}

func runGet() {
	drainProtocolInput()

	cred, err := loadCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: önbellek okunamadı: %v\n", err)
		os.Exit(1)
	}
	if cred != nil {
		fmt.Printf("username=%s\npassword=%s\n", cred.Subject, cred.Token)
		return
	}

	subject, token, err := promptAndLogin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: giriş başarısız: %v\n", err)
		os.Exit(1)
	}
	if err := saveCache(cachedCredential{Subject: subject, Token: token, CachedAt: time.Now()}); err != nil {
		// A cache write failure shouldn't block this login from working
		// right now — it just means the next git operation prompts again.
		fmt.Fprintf(os.Stderr, "devplatform-login: uyarı: anahtar önbelleğe yazılamadı: %v\n", err)
	}
	fmt.Printf("username=%s\npassword=%s\n", subject, token)
}

func runStore() {
	drainProtocolInput()
	// No-op: this tool populates its own cache during `get`, it doesn't
	// need git to confirm a credential worked.
}

func runErase() {
	drainProtocolInput()
	if err := clearCache(); err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: önbellek temizlenemedi: %v\n", err)
		os.Exit(1)
	}
}

// installCommands returns the two git-config invocations `install`
// needs to run, in order:
//
//  1. Reset any inherited credential.helper chain for this host to
//     empty first. Git for Windows' own system config sets a generic
//     credential.helper=manager for every host; without this reset,
//     that helper answers first (it's read before --global) and
//     devplatform-login is never invoked at all — this is the exact
//     failure mode ("stale token, silent 401") the whole plan exists
//     to remove. An empty helper value at a more specific URL scope
//     tells git "ignore anything less specific, start the list over
//     here" — the same idiom GitHub CLI's own `gh auth setup-git`
//     writes for github.com (see this machine's own ~/.gitconfig).
//  2. Add the real helper, wrapped as an explicit shell command (the
//     leading "!") with the path single-quoted. Git invokes helpers
//     via `sh -c`, which treats a bare backslash as an escape
//     character — an unquoted Windows path like
//     C:\Users\x\devplatform-login.exe is silently mangled into
//     something that "command not found"s. Single-quoting inside a
//     sh command string makes backslashes (and spaces, e.g. under
//     "C:\Program Files") literal.
func installCommands(selfPath string) []*exec.Cmd {
	helperValue := "!'" + selfPath + "'"
	return []*exec.Cmd{
		exec.Command("git", "config", "--global", "--replace-all",
			"credential.https://git.sigortatahkim.org.helper", ""),
		exec.Command("git", "config", "--global", "--add",
			"credential.https://git.sigortatahkim.org.helper", helperValue),
	}
}

func runInstall() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: kendi yolum bulunamadı: %v\n", err)
		os.Exit(1)
	}
	for _, cmd := range installCommands(self) {
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "devplatform-login: git config başarısız: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}
	fmt.Println("Kuruldu. Artık git.sigortatahkim.org için git işlemleri otomatik kimlik doğrulayacak.")
}

// promptAndLogin opens the real console directly (not stdin/stdout,
// which `get` has already reserved for git's own protocol) to ask for
// credentials interactively, then runs the login chain.
func promptAndLogin() (subject, token string, err error) {
	in, out, err := openConsole()
	if err != nil {
		return "", "", fmt.Errorf("konsol açılamadı (bu araç etkileşimli bir terminalden çalıştırılmalı): %w", err)
	}
	defer in.Close()
	defer out.Close()

	fmt.Fprint(out, "STK Atölye (Intranet) kullanıcı adı: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", "", fmt.Errorf("kullanıcı adı okunamadı")
	}
	username := scanner.Text()

	password, err := readPassword(in, out, "Windows şifreniz: ")
	if err != nil {
		return "", "", err
	}

	subject, token, err = login(username, password)
	if err != nil && errors.Is(err, ErrBadCredentials) {
		// One retry for the single most common case — a mistyped
		// password — before giving up. Anything else login can fail
		// with (network error, Intranet-B down, not authorized for
		// DevPlatform, ...) isn't something retrying the same password
		// fixes, so only ErrBadCredentials gets this second chance.
		fmt.Fprintln(out, "Kullanıcı adı veya şifre hatalı, tekrar deneyin.")
		password, err = readPassword(in, out, "Windows şifreniz: ")
		if err != nil {
			return "", "", err
		}
		subject, token, err = login(username, password)
	}
	return subject, token, err
}

// readPassword prompts on out and reads a password from in without
// echoing it — shared by promptAndLogin's initial attempt and its one
// retry.
func readPassword(in, out *os.File, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	passwordBytes, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("şifre okunamadı: %w", err)
	}
	return string(passwordBytes), nil
}
