package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestDecideIdentity(t *testing.T) {
	cases := []struct {
		name    string
		current string
		panel   string
		want    identityDecision
	}{
		{
			name:    "no global user.email is the fresh-machine case",
			current: "",
			panel:   "rifat.ozturk@sigortatahkim.org",
			want:    identityUnset,
		},
		{
			name:    "whitespace-only is still unset",
			current: "   ",
			panel:   "rifat.ozturk@sigortatahkim.org",
			want:    identityUnset,
		},
		{
			name:    "already correct means don't touch anything",
			current: "rifat.ozturk@sigortatahkim.org",
			panel:   "rifat.ozturk@sigortatahkim.org",
			want:    identityMatches,
		},
		{
			// Addresses are case-insensitive, and a difference in case
			// alone would otherwise prompt this person on every single
			// login for a change that would accomplish nothing.
			name:    "case differences are not a real difference",
			current: "Rifat.Ozturk@SigortaTahkim.org",
			panel:   "rifat.ozturk@sigortatahkim.org",
			want:    identityMatches,
		},
		{
			name:    "a different address is what we have to ask about",
			current: "rifatozturk061@gmail.com",
			panel:   "rifat.ozturk@sigortatahkim.org",
			want:    identityDiffers,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideIdentity(tc.current, tc.panel); got != tc.want {
				t.Errorf("decideIdentity(%q, %q) = %v, want %v", tc.current, tc.panel, got, tc.want)
			}
		})
	}
}

func TestAskYes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		// Enter alone takes the default, which is yes — somebody who
		// just wanted to push code shouldn't have to type anything to
		// get the setup that works.
		{name: "bare Enter", input: "\n", want: true},
		{name: "e for evet", input: "e\n", want: true},
		{name: "uppercase E", input: "E\n", want: true},
		{name: "y for yes", input: "y\n", want: true},
		{name: "h for hayır", input: "h\n", want: false},
		{name: "hayir spelled out", input: "hayir\n", want: false},
		{name: "n for no", input: "n\n", want: false},
		{name: "surrounding spaces are trimmed", input: "  h  \n", want: false},
		// A console that can't be read at all (closed stdin, a
		// non-interactive shell) must not hang or crash — it takes the
		// default like everything else.
		{name: "no input at all", input: "", want: true},
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tc.input))
			if got := askYes(devNull, scanner, "soru? "); got != tc.want {
				t.Errorf("askYes(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSyncGitIdentity_DoesNothingWithoutAPanelAddress(t *testing.T) {
	// login() leaves Email empty when /api/me didn't answer. Reaching
	// git config in that state would either write an empty address or
	// prompt about a comparison with nothing on one side.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	scanner := bufio.NewScanner(strings.NewReader(""))
	syncGitIdentity(devNull, scanner, session{Subject: "dev-1", Token: "tok"})
}

func TestShouldWriteName(t *testing.T) {
	cases := []struct {
		name        string
		displayName string
		email       string
		want        bool
	}{
		{
			name:        "a real display name is worth writing",
			displayName: "Rifat Öztürk",
			email:       "rifat.ozturk@sigortatahkim.org",
			want:        true,
		},
		{
			// /api/me's displayName falls back to the email when the
			// account has no override, which would otherwise produce
			// "rifat@x.org <rifat@x.org>" on every commit.
			name:        "the panel's email fallback is not a name",
			displayName: "rifat.ozturk@sigortatahkim.org",
			email:       "rifat.ozturk@sigortatahkim.org",
			want:        false,
		},
		{
			name:        "the same fallback in another case is still not a name",
			displayName: "Rifat.Ozturk@SigortaTahkim.org",
			email:       "rifat.ozturk@sigortatahkim.org",
			want:        false,
		},
		{
			name:        "nothing to write",
			displayName: "",
			email:       "rifat.ozturk@sigortatahkim.org",
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldWriteName(tc.displayName, tc.email); got != tc.want {
				t.Errorf("shouldWriteName(%q, %q) = %v, want %v", tc.displayName, tc.email, got, tc.want)
			}
		})
	}
}
