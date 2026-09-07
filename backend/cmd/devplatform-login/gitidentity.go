package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Git stamps every commit with whatever `git config user.email` says on
// the machine it was made on, and DevPlatform's contribution graph finds
// a person's commits by matching that stamp against their panel account.
// When the two differ — a personal address left over in a global
// gitconfig, a machine set up before the panel existed — the graph looks
// empty even though the person has been committing all week.
//
// This file closes that gap at the only moment where the two identities
// are both in hand and a human is watching: the interactive login. See
// docs/DURUM.md's entry for the reasoning behind doing it here rather
// than asking in the panel afterwards.

// identityDecision is what syncGitIdentity should do about the git
// identity already configured on this machine.
type identityDecision int

const (
	// identityMatches — git already signs commits with the panel
	// address. Nothing to do.
	identityMatches identityDecision = iota
	// identityUnset — no global user.email at all, so writing one takes
	// nothing away from anybody. Done silently.
	identityUnset
	// identityDiffers — git signs with some other address. Never
	// overwritten without asking: it may be deliberate (a machine also
	// used for personal work), and silently rewriting someone's git
	// identity is exactly the kind of surprise that erodes trust in a
	// tool that runs unattended inside `git push`.
	identityDiffers
)

func decideIdentity(current, panel string) identityDecision {
	switch {
	case strings.TrimSpace(current) == "":
		return identityUnset
	case strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(panel)):
		return identityMatches
	default:
		return identityDiffers
	}
}

// gitConfigGlobal reads one --global key, returning "" when it is unset.
// git exits non-zero for a missing key, which is not an error here.
func gitConfigGlobal(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func setGitConfigGlobal(key, value string) error {
	cmd := exec.Command("git", "config", "--global", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config --global %s: %v: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shouldWriteName reports whether name is worth writing as user.name.
// The panel falls back to the email address when an account has no
// display-name override, and a commit that reads
// "rifat@x.org <rifat@x.org>" looks like a bug in every git log — better
// to leave user.name to git's own default in that case.
func shouldWriteName(name, email string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.EqualFold(name, strings.TrimSpace(email))
}

// writeGitIdentity sets both halves of the identity.
func writeGitIdentity(name, email string) error {
	if err := setGitConfigGlobal("user.email", email); err != nil {
		return err
	}
	if shouldWriteName(name, email) {
		if err := setGitConfigGlobal("user.name", name); err != nil {
			return err
		}
	}
	return nil
}

// syncGitIdentity lines this machine's git identity up with the panel
// account that just logged in.
//
// It is deliberately incapable of failing the login: every problem here
// is reported and stepped over. The credential the caller came for has
// already been issued by this point, and a git-config hiccup must not
// stand between somebody and their `git push`.
func syncGitIdentity(out *os.File, scanner *bufio.Scanner, s session) {
	if s.Email == "" {
		return
	}

	current := gitConfigGlobal("user.email")
	switch decideIdentity(current, s.Email) {
	case identityMatches:
		return

	case identityUnset:
		if err := writeGitIdentity(s.DisplayName, s.Email); err != nil {
			fmt.Fprintf(out, "Uyarı: git kimliği ayarlanamadı: %v\n", err)
			return
		}
		fmt.Fprintf(out, "Git kimliğin ayarlandı: %s\n", s.Email)

	case identityDiffers:
		fmt.Fprintf(out, "\nGit commit'lerin şu an %s adresiyle imzalanıyor,\n", current)
		fmt.Fprintf(out, "panel hesabın ise %s.\n", s.Email)
		if askYes(out, scanner, "Bundan sonra panel adresin kullanılsın mı? [E/h] ") {
			if err := writeGitIdentity(s.DisplayName, s.Email); err != nil {
				fmt.Fprintf(out, "Uyarı: git kimliği ayarlanamadı: %v\n", err)
				return
			}
			fmt.Fprintf(out, "Tamam, bundan sonraki commit'lerin %s adresiyle imzalanacak.\n", s.Email)
			return
		}
		// They kept their own address, so bind it to the panel account
		// instead — either way the contribution graph ends up finding
		// their commits, which is the whole point of asking.
		if err := claimGitEmail(s.jwt, current); err != nil {
			fmt.Fprintf(out, "Uyarı: %s adresi panel hesabına bağlanamadı: %v\n", current, err)
			fmt.Fprintln(out, "Hesabım sayfasından elle ekleyebilirsin.")
			return
		}
		fmt.Fprintf(out, "Tamam, %s adresi panel hesabına bağlandı — katkı grafiğin yine dolacak.\n", current)
	}
}

// askYes asks a yes/no question where Enter means yes. Anything starting
// with h or n (hayır / no) is a no; everything else — including a
// console that can't be read — takes the default, which is the answer
// that needs no explanation to a person who just wanted to push code.
func askYes(out *os.File, scanner *bufio.Scanner, prompt string) bool {
	fmt.Fprint(out, prompt)
	if !scanner.Scan() {
		fmt.Fprintln(out)
		return true
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return !strings.HasPrefix(answer, "h") && !strings.HasPrefix(answer, "n")
}
