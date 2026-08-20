package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// build.js requires "fixture-dep", a dependency declared in
	// package.json but never committed as node_modules (same as any real
	// project's node_modules). If Build ever stops installing dependencies
	// before running the build script, this fails with exactly the "Cannot
	// find module" error a real Vite/React deploy hit in production.
	if !strings.Contains(string(content), "fixture-dep-installed") {
		t.Errorf("index.html = %q, want it to contain the fixture-dep marker (was the dependency actually installed?)", content)
	}

	// The fixture also writes a nested assets/style.css, mirroring the
	// subdirectories a real Vite/React build produces. copyDir must recurse
	// into subdirectories instead of silently dropping them — otherwise a
	// real deploy would 200 on index.html but 404 on every hashed asset.
	assetContent, err := os.ReadFile(filepath.Join(outputDir, "assets", "style.css"))
	if err != nil {
		t.Fatalf("expected assets/style.css in output dir (nested dirs must be copied): %v", err)
	}
	if string(assetContent) != "body { color: red; }\n" {
		t.Errorf("assets/style.css content = %q, want %q", assetContent, "body { color: red; }\n")
	}
}

func TestBuild_Go_ProducesOutput(t *testing.T) {
	requireTool(t, "go")

	source, err := filepath.Abs("testdata/go-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	outputDir := t.TempDir()

	b := &Builder{}
	if err := b.Build(source, RecipeGo, outputDir); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	exeInfo, err := os.Stat(filepath.Join(outputDir, "app.exe"))
	if err != nil {
		t.Fatalf("expected app.exe in output dir: %v", err)
	}
	if exeInfo.Size() == 0 {
		t.Error("app.exe is empty")
	}

	// web.config isn't produced by `go build` — it has to be copied from
	// the project source explicitly (see buildGo's doc comment). Prove it
	// actually happened rather than app.exe alone existing by accident.
	webConfig, err := os.ReadFile(filepath.Join(outputDir, "web.config"))
	if err != nil {
		t.Fatalf("expected web.config to be copied into output dir: %v", err)
	}
	if !strings.Contains(string(webConfig), "httpPlatformHandler") {
		t.Errorf("web.config content = %q, missing expected marker", webConfig)
	}
}

func TestBuild_Go_RequiresWebConfig(t *testing.T) {
	requireTool(t, "go")

	// A fixture with no web.config at all — go build itself succeeds, but
	// the recipe must still fail clearly rather than silently producing a
	// release IIS could never actually serve (no httpPlatformHandler
	// config to tell it how to launch app.exe).
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module no-web-config\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(source, "cmd", "server"), 0o750); err != nil {
		t.Fatalf("failed to create cmd/server: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "cmd", "server", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	b := &Builder{}
	if err := b.Build(source, RecipeGo, t.TempDir()); err == nil {
		t.Fatal("expected an error when web.config is missing, got nil")
	}
}

func TestBuild_RejectsUnknownRecipe(t *testing.T) {
	b := &Builder{}
	err := b.Build(t.TempDir(), Recipe("not-a-real-recipe"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error for an unknown recipe, got nil")
	}
}
