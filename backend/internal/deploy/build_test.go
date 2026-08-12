package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found on PATH, skipping", name)
	}
}

func TestBuild_Dotnet_ProducesOutput(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	outputDir := t.TempDir()

	b := &Builder{}
	if err := b.Build(source, RecipeDotnet, outputDir); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "Fixture.dll")); err != nil {
		t.Errorf("expected Fixture.dll in output dir: %v", err)
	}
}

func TestBuild_Npm_ProducesOutput(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	outputDir := t.TempDir()

	b := &Builder{}
	if err := b.Build(source, RecipeNpm, outputDir); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outputDir, "index.html"))
	if err != nil {
		t.Fatalf("expected index.html in output dir: %v", err)
	}
	if string(content) == "" {
		t.Error("index.html is empty")
	}
}

func TestBuild_RejectsUnknownRecipe(t *testing.T) {
	b := &Builder{}
	err := b.Build(t.TempDir(), Recipe("not-a-real-recipe"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error for an unknown recipe, got nil")
	}
}
