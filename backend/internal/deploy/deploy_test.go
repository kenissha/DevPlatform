package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPipeline_Deploy_BuildsVersionsAndSwaps(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner))

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 5)
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

func TestPipeline_Deploy_PrunesOldReleases(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner))

	for i := 0; i < 3; i++ {
		if _, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 2); err != nil {
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
