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
}

func (f *fakeCommandRunner) Run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.failWith != nil {
		return nil, f.failWith
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
