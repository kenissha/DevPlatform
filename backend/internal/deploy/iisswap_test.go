package deploy

import (
	"errors"
	"testing"
)

// fakeCommandRunner records every call it receives instead of executing
// anything real — this is how IISSwapper's argument-building logic gets
// tested without ever invoking the real appcmd.exe, which requires
// Administrator privileges this test environment doesn't have.
type fakeCommandRunner struct {
	calls    [][]string
	failWith error
	// failStart is consumed in order, one entry per "start site" call —
	// ActivateRelease's auto-revert path makes two such calls (the new
	// release, then the previous one on failure) and tests need to make
	// the first fail while the second succeeds (or both fail), which a
	// single blanket failWith can't express since neither call carries
	// any argument that tells them apart (StartSite takes no path).
	failStart []error
}

func (f *fakeCommandRunner) Run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.failWith != nil {
		return nil, f.failWith
	}
	if len(args) > 0 && args[0] == "start" && len(f.failStart) > 0 {
		err := f.failStart[0]
		f.failStart = f.failStart[1:]
		if err != nil {
			return nil, err
		}
	}
	return []byte("ok"), nil
}

func TestSetPhysicalPath_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.SetPhysicalPath("DevPlatform Test Site", `C:\releases\sample\test\20260812T000000.000000000`); err != nil {
		t.Fatalf("SetPhysicalPath returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls to the command runner, want 1", len(runner.calls))
	}
	got := runner.calls[0]
	want := []string{
		AppcmdPath(), "set", "vdir", "DevPlatform Test Site/",
		`/physicalPath:C:\releases\sample\test\20260812T000000.000000000`,
	}
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSetPhysicalPath_RejectsRelativePath(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	err := swapper.SetPhysicalPath("DevPlatform Test Site", `relative\path`)
	if err == nil {
		t.Fatal("expected an error for a relative path, got nil")
	}

	if len(runner.calls) != 0 {
		t.Errorf("got %d calls to the command runner, want 0 (validation should happen before the command runs)", len(runner.calls))
	}
}

func TestSetPhysicalPath_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	err := swapper.SetPhysicalPath("DevPlatform Test Site", `C:\releases\sample\test\v1`)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestStopSite_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.StopSite("DevPlatform Test Site"); err != nil {
		t.Fatalf("StopSite returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(runner.calls))
	}
	want := []string{AppcmdPath(), "stop", "site", "/site.name:DevPlatform Test Site"}
	got := runner.calls[0]
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStartSite_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.StartSite("DevPlatform Test Site"); err != nil {
		t.Fatalf("StartSite returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(runner.calls))
	}
	want := []string{AppcmdPath(), "start", "site", "/site.name:DevPlatform Test Site"}
	got := runner.calls[0]
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStopSite_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	if err := swapper.StopSite("DevPlatform Test Site"); err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestStartSite_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	if err := swapper.StartSite("DevPlatform Test Site"); err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestActivateRelease_NpmRecipeIsJustAPlainSwap(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeNpm, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if err != nil {
		t.Fatalf("ActivateRelease returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1 (npm recipe must never stop/start)", len(runner.calls))
	}
	if runner.calls[0][1] != "set" {
		t.Errorf("call = %v, want a plain 'set vdir'", runner.calls[0])
	}
}

func TestActivateRelease_DotnetRecipeStopsSwapsThenStarts(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if err != nil {
		t.Fatalf("ActivateRelease returned error: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("got %d calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" {
		t.Errorf("call[0] = %v, want stop site first", runner.calls[0])
	}
	if runner.calls[1][1] != "set" {
		t.Errorf("call[1] = %v, want set vdir second", runner.calls[1])
	}
	if runner.calls[1][len(runner.calls[1])-1] != `/physicalPath:C:\releases\v2` {
		t.Errorf("call[1] = %v, want it to target the new release", runner.calls[1])
	}
	if runner.calls[2][1] != "start" {
		t.Errorf("call[2] = %v, want start site third", runner.calls[2])
	}
}

func TestActivateRelease_DotnetRecipeRevertsOnStartFailure(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{errors.New("simulated: new release crashes on start")}}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}

	// stop, set(new), start(new, fails), set(previous), start(previous)
	if len(runner.calls) != 5 {
		t.Fatalf("got %d calls, want 5: %v", len(runner.calls), runner.calls)
	}
	if runner.calls[3][len(runner.calls[3])-1] != `/physicalPath:C:\releases\v1` {
		t.Errorf("call[3] = %v, want the revert swap to target the previous release", runner.calls[3])
	}
	if runner.calls[4][1] != "start" {
		t.Errorf("call[4] = %v, want a final start site to bring the previous release back up", runner.calls[4])
	}
}

func TestActivateRelease_DotnetRecipeReturnsSiteDownWhenRevertAlsoFails(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{
		errors.New("simulated: new release crashes on start"),
		errors.New("simulated: previous release also fails to start"),
	}}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if !errors.Is(err, ErrSiteDown) {
		t.Fatalf("err = %v, want ErrSiteDown", err)
	}
}

func TestActivateRelease_DotnetRecipeReturnsSiteDownImmediatelyWithNoPreviousRelease(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{errors.New("simulated: first-ever deploy crashes on start")}}
	swapper := NewIISSwapper(runner)

	// previousReleaseDir is "" — there is nothing to fall back to (e.g.
	// the very first deploy for a target).
	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v1`, "")
	if !errors.Is(err, ErrSiteDown) {
		t.Fatalf("err = %v, want ErrSiteDown", err)
	}
	// stop, set(new), start(new, fails) — no revert attempt possible.
	if len(runner.calls) != 3 {
		t.Fatalf("got %d calls, want 3 (no revert attempt without a previous release): %v", len(runner.calls), runner.calls)
	}
}
