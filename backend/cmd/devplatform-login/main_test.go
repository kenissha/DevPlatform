package main

import "testing"

func TestInstallCommand_UsesTheExactHelperConfigKey(t *testing.T) {
	cmd := installCommand(`C:\tools\devplatform-login.exe`)

	want := []string{
		"git", "config", "--global",
		"credential.https://git.sigortatahkim.org.helper", `C:\tools\devplatform-login.exe`,
	}
	got := cmd.Args
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
