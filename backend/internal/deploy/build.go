// Package deploy builds and releases projects hosted on this platform. It
// deliberately never accepts a free-text build command from a caller —
// every build is one of a small, fixed set of recipes, so nothing a user
// types ever reaches a shell.
package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Recipe identifies a fixed, known way to build a project. There is no
// "custom command" recipe — adding a new project type means adding a new
// Recipe constant and a case in Builder.Build, not accepting a string from
// a caller.
type Recipe string

const (
	RecipeDotnet Recipe = "dotnet"
	RecipeNpm    Recipe = "npm"
	RecipeGo     Recipe = "go"
)

// NeedsProcessRestart reports whether recipe builds a long-running
// process (a backend that locks its own files while running) rather than
// static content an IIS worker can serve directly out of whatever folder
// its physical path currently points at. IISSwapper.ActivateRelease uses
// this to decide whether a release needs the stop→swap→start sequence
// (see its own doc comment) or a plain physical-path swap — a single
// method here, rather than a recipe-by-recipe check at each call site,
// so adding a future process-based recipe can't repeat the mistake this
// one fixed: dotnet originally got the process treatment by comparing
// directly against RecipeDotnet, which would have silently treated any
// later process-based recipe (this one, Go) as static.
func (r Recipe) NeedsProcessRestart() bool {
	return r == RecipeDotnet || r == RecipeGo
}

// Builder runs a fixed build recipe against sourceDir, writing the result
// to outputDir. outputDir is created if it doesn't exist; it is the
// caller's responsibility to pass a fresh, empty directory (see
// VersionStore in a later task) — Builder does not clean up outputDir
// itself.
type Builder struct{}

// Build runs recipe against sourceDir and writes the result to outputDir.
// Every exec.Command call here uses a fixed program name and a fixed
// argument list built only from sourceDir/outputDir (paths the caller
// controls, not free text from an end user) — never a shell, never string
// concatenation into a command line.
func (b *Builder) Build(sourceDir string, recipe Recipe, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("deploy: failed to create output dir: %w", err)
	}

	switch recipe {
	case RecipeDotnet:
		return b.buildDotnet(sourceDir, outputDir)
	case RecipeNpm:
		return b.buildNpm(sourceDir, outputDir)
	case RecipeGo:
		return b.buildGo(sourceDir, outputDir)
	default:
		return fmt.Errorf("deploy: unknown recipe %q", recipe)
	}
}

func (b *Builder) buildDotnet(sourceDir, outputDir string) error {
	cmd := exec.Command("dotnet", "publish", sourceDir, "-o", outputDir, "-c", "Release")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploy: dotnet publish failed: %w\n%s", err, out)
	}
	return nil
}

func (b *Builder) buildNpm(sourceDir, outputDir string) error {
	// A fresh git checkout never has node_modules — it's the one directory
	// every real npm project's .gitignore excludes — so "npm run build"
	// alone fails on any project with real dependencies (e.g. Vite) with
	// "not recognized as an internal or external command" or "Cannot find
	// module". npm ci requires package-lock.json (committed, unlike
	// node_modules) and installs exactly what it pins, which is both the
	// fix and the reproducible-build behavior a deploy pipeline wants over
	// plain "npm install".
	installCmd := exec.Command("npm", "ci")
	installCmd.Dir = sourceDir
	if out, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("deploy: npm ci failed: %w\n%s", err, out)
	}

	cmd := exec.Command("npm", "run", "build")
	cmd.Dir = sourceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploy: npm run build failed: %w\n%s", err, out)
	}

	// npm's convention is to write build output to a "dist" folder inside
	// sourceDir; copy its contents into outputDir so every recipe has the
	// same "outputDir now holds the deployable artifact" contract.
	return copyDir(filepath.Join(sourceDir, "dist"), outputDir)
}

// buildGo compiles the project's fixed entry point (./cmd/server, the one
// path every go-recipe project is required to use — no per-target custom
// build path, matching this package's "fixed recipe, never free text"
// rule) into a fixed binary name, app.exe, inside outputDir. The output
// name is fixed rather than derived from the module/project so every
// go-recipe project's web.config can reference it identically
// (processPath=".\app.exe") regardless of which project it is.
//
// go build alone only produces the binary — unlike `dotnet publish`,
// which bundles a project's web.config automatically, Go has no such
// convention, so the project's own web.config (the httpPlatformHandler
// config telling IIS how to launch app.exe, checked into the repo
// alongside main.go) has to be copied into the release explicitly.
func (b *Builder) buildGo(sourceDir, outputDir string) error {
	exePath := filepath.Join(outputDir, "app.exe")
	cmd := exec.Command(goBin(), "build", "-o", exePath, "./cmd/server")
	cmd.Dir = sourceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploy: go build failed: %w\n%s", err, out)
	}

	webConfig, err := os.ReadFile(filepath.Join(sourceDir, "web.config"))
	if err != nil {
		return fmt.Errorf("deploy: go recipe requires a web.config at the project root: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "web.config"), webConfig, 0o640); err != nil {
		return fmt.Errorf("deploy: failed to write web.config into release: %w", err)
	}
	return nil
}

// goBin returns the go toolchain executable to run, preferring
// DEVPLATFORM_GO_BIN (a full path to go.exe) when set. exec.Command
// resolves a bare "go" via the launching process's own PATH — under
// IIS's httpPlatformHandler, that process's PATH does not reliably pick
// up a PATH environment variable set in the site's web.config even after
// the app pool is restarted (observed in production: PATH edits added to
// web.config's <environmentVariables> and a full restart still left "go"
// unresolvable), so a full-path override is the dependable fix. Falls
// back to the bare "go" (ordinary PATH lookup) when unset, matching this
// package's behavior everywhere PATH resolution does work (local dev,
// most non-IIS hosts).
func goBin() string {
	if v := os.Getenv("DEVPLATFORM_GO_BIN"); v != "" {
		return v
	}
	return "go"
}

// copyDir recursively copies the contents of src into dst, preserving the
// relative directory structure. Real frontend builds (e.g. Vite/React, which
// this platform's own frontend already uses) produce nested subdirectories
// such as assets/ — copying only top-level files would silently produce a
// broken deploy: index.html would 200 but every hashed JS/CSS asset would
// 404, with no error anywhere to indicate anything went wrong.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("deploy: failed to read npm build output %q: %w", src, err)
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0o750); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o640); err != nil {
			return err
		}
	}
	return nil
}
