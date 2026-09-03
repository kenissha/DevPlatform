package main

import "testing"

func TestInstallCommands_ResetsThenAddsTheShellSafeHelper(t *testing.T) {
	cmds := installCommands(`C:\tools\devplatform-login.exe`)

	if len(cmds) != 2 {
		t.Fatalf("installCommands returned %d commands, want 2", len(cmds))
	}

	wantReset := []string{
		"git", "config", "--global", "--replace-all",
		"credential.https://git.sigortatahkim.org.helper", "",
	}
	if got := cmds[0].Args; !argsEqual(got, wantReset) {
		t.Errorf("reset command args = %v, want %v", got, wantReset)
	}

	wantAdd := []string{
		"git", "config", "--global", "--add",
		"credential.https://git.sigortatahkim.org.helper",
		`!'C:\tools\devplatform-login.exe'`,
	}
	if got := cmds[1].Args; !argsEqual(got, wantAdd) {
		t.Errorf("add command args = %v, want %v", got, wantAdd)
	}
}

func TestInstallCommands_QuotesAPathContainingSpaces(t *testing.T) {
	cmds := installCommands(`C:\Program Files\devplatform-login.exe`)

	want := `!'C:\Program Files\devplatform-login.exe'`
	if got := cmds[1].Args[len(cmds[1].Args)-1]; got != want {
		t.Errorf("helper value = %q, want %q", got, want)
	}
}

func argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
