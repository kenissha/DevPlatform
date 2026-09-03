package main

import (
	"bufio"
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
		fmt.Fprintf(os.Stderr, "devplatform-login: uyarı: token önbelleğe yazılamadı: %v\n", err)
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

func installCommand(selfPath string) *exec.Cmd {
	return exec.Command("git", "config", "--global",
		"credential.https://git.sigortatahkim.org.helper", selfPath)
}

func runInstall() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: kendi yolum bulunamadı: %v\n", err)
		os.Exit(1)
	}
	cmd := installCommand(self)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: git config başarısız: %v\n%s\n", err, out)
		os.Exit(1)
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

	fmt.Fprint(out, "STK Atölye kullanıcı adı: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", "", fmt.Errorf("kullanıcı adı okunamadı")
	}
	username := scanner.Text()

	fmt.Fprint(out, "Şifre: ")
	passwordBytes, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", "", fmt.Errorf("şifre okunamadı: %w", err)
	}

	return login(username, string(passwordBytes))
}
