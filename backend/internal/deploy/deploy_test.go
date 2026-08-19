package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
)

// fakePruneFailingStore wraps a real *VersionStore to delegate NewRelease
// normally, but always fails Prune — simulating "release activated but
// cleanup failed" deterministically, without relying on filesystem-level
// tricks (permissions, locked files) that are unreliable across platforms.
type fakePruneFailingStore struct {
	real *VersionStore
}

func (f *fakePruneFailingStore) NewRelease(repo, environment string) (string, error) {
	return f.real.NewRelease(repo, environment)
}

func (f *fakePruneFailingStore) Prune(repo, environment string, keep int) error {
	return errors.New("simulated prune failure: release directory locked by another process")
}

func TestPipeline_Deploy_BuildsVersionsAndSwaps(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, "")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(releaseDir, "index.html")); err != nil {
		t.Errorf("expected built output in release dir: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d appcmd calls, want 1", len(runner.calls))
	}
	lastArg := runner.calls[0][len(runner.calls[0])-1]
	if lastArg != "/physicalPath:"+releaseDir {
		t.Errorf("appcmd physicalPath arg = %q, want %q", lastArg, "/physicalPath:"+releaseDir)
	}
}

func TestPipeline_Deploy_RejectsNonPositiveKeepVersions(t *testing.T) {
	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	for _, keep := range []int{0, -1} {
		vs := NewVersionStore(t.TempDir())
		runner := &fakeCommandRunner{}
		pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil)

		_, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", keep, "")
		if err == nil {
			t.Fatalf("Deploy with keepVersions=%d: expected an error, got nil", keep)
		}

		// The guard must short-circuit before SetPhysicalPath or Prune ever
		// run — otherwise IIS could be left pointed at a release that was
		// then deleted by Prune. Zero recorded appcmd calls proves neither
		// ran.
		if len(runner.calls) != 0 {
			t.Errorf("Deploy with keepVersions=%d: got %d appcmd calls, want 0 (guard should short-circuit before any IIS call)", keep, len(runner.calls))
		}
	}
}

func TestPipeline_Deploy_PrunesOldReleases(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil)

	for i := 0; i < 3; i++ {
		if _, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 2, ""); err != nil {
			t.Fatalf("Deploy #%d returned error: %v", i, err)
		}
	}

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases after 3 deploys with keepVersions=2, want 2", len(releases))
	}
}

func TestPipeline_Deploy_PruneFailureReturnsReleaseDirAndErrPruneFailed(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	store := &fakePruneFailingStore{real: NewVersionStore(t.TempDir())}
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, store, NewIISSwapper(runner), nil)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, "")

	// The release was already built and activated (SetPhysicalPath ran
	// successfully — Prune is the only thing that failed), so the caller
	// must still get back the valid, now-live release directory instead of
	// an empty string indistinguishable from an actual deploy failure.
	if releaseDir == "" {
		t.Fatal("Deploy returned an empty releaseDir on a Prune-only failure; the release was already live and its path must not be lost")
	}
	if _, statErr := os.Stat(filepath.Join(releaseDir, "index.html")); statErr != nil {
		t.Errorf("expected returned releaseDir to be the real, built release dir: %v", statErr)
	}

	if err == nil {
		t.Fatal("expected an error when Prune fails, got nil")
	}
	if !errors.Is(err, ErrPruneFailed) {
		t.Errorf("errors.Is(err, ErrPruneFailed) = false, want true; err = %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d appcmd calls, want 1 (SetPhysicalPath must have run before Prune failed)", len(runner.calls))
	}
}

func TestPipeline_Deploy_InjectsSecretsWhenConfigured(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	key := []byte("01234567890123456789012345678901"[:32])
	secrets := secretsvault.NewStore(t.TempDir(), key)
	if err := secrets.Put("sample", "test", []byte(`{"connectionString": "test-only"}`)); err != nil {
		t.Fatalf("failed to seed secrets: %v", err)
	}

	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), secrets)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, "appsettings.Production.json")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(releaseDir, "appsettings.Production.json"))
	if err != nil {
		t.Fatalf("expected secrets file in release dir: %v", err)
	}
	if string(content) != `{"connectionString": "test-only"}` {
		t.Errorf("secrets file content = %q, want the seeded value", content)
	}
}

func TestPipeline_Deploy_SkipsSecretsWhenTargetEmpty(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	key := []byte("01234567890123456789012345678901"[:32])
	secrets := secretsvault.NewStore(t.TempDir(), key)
	// Deliberately not seeding any secrets for "sample"/"test" — proves
	// Deploy never even tries to read them when secretsTarget is empty.
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), secrets)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, "")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(releaseDir, "appsettings.Production.json")); !os.IsNotExist(err) {
		t.Error("expected no secrets file to be written when secretsTarget is empty")
	}
}

func TestPipeline_Deploy_ErrorsWhenSecretsTargetGivenButNoStoreConfigured(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil) // no secrets store

	_, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, "appsettings.Production.json")
	if err == nil {
		t.Fatal("expected an error when secretsTarget is set but no secrets store is configured")
	}
}

func TestPipeline_Deploy_RejectsPathTraversalInSecretsTarget(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	key := []byte("01234567890123456789012345678901"[:32])

	escapeDir := t.TempDir()
	targets := []string{
		"../escape.json",
		"../../../../etc/escape.json",
		filepath.Join(escapeDir, "escape.json"), // absolute path
	}

	for _, target := range targets {
		vs := NewVersionStore(t.TempDir())
		runner := &fakeCommandRunner{}
		secrets := secretsvault.NewStore(t.TempDir(), key)
		if err := secrets.Put("sample", "test", []byte(`{"connectionString": "test-only"}`)); err != nil {
			t.Fatalf("failed to seed secrets: %v", err)
		}
		pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), secrets)

		_, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", "", 5, target)
		if err == nil {
			t.Fatalf("Deploy with secretsTarget %q: expected an error, got nil", target)
		}

		// The guard must short-circuit before SetPhysicalPath ever runs —
		// zero recorded appcmd calls proves the traversal attempt never
		// reached IIS activation.
		if len(runner.calls) != 0 {
			t.Errorf("Deploy with secretsTarget %q: got %d appcmd calls, want 0 (guard should short-circuit before any IIS call)", target, len(runner.calls))
		}

		// Nothing should have been written outside the escape directory
		// either — the file must not exist since Deploy should have
		// rejected the target before ever calling secrets.Get or
		// os.WriteFile.
		if _, statErr := os.Stat(filepath.Join(escapeDir, "escape.json")); !os.IsNotExist(statErr) {
			t.Errorf("Deploy with secretsTarget %q: escape.json was written outside the release dir", target)
		}
	}
}

func TestPipeline_Deploy_DotnetRecipeStopsAndStartsTheSite(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil)

	_, err = pipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", "", 5, "")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("got %d IIS calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" || runner.calls[2][1] != "start" {
		t.Errorf("calls = %v, want stop first and start last", runner.calls)
	}
}

func TestPipeline_Deploy_DotnetRecipeReturnsPreviousReleaseOnRevert(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())

	// First deploy succeeds normally, establishing a "previous release".
	firstRunner := &fakeCommandRunner{}
	firstPipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(firstRunner), nil)
	firstReleaseDir, err := firstPipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", "", 5, "")
	if err != nil {
		t.Fatalf("first Deploy returned error: %v", err)
	}

	// Second deploy's new release fails to start.
	secondRunner := &fakeCommandRunner{failStart: []error{errors.New("simulated: crashes on start")}}
	secondPipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(secondRunner), nil)
	releaseDir, err := secondPipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", firstReleaseDir, 5, "")

	if !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}
	if releaseDir != firstReleaseDir {
		t.Errorf("releaseDir = %q, want the previous (still-live) release %q", releaseDir, firstReleaseDir)
	}
}
