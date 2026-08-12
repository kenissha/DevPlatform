package deployment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

func TestLoadTargets_EmptyPathMeansNoTargets(t *testing.T) {
	targets, err := LoadTargets("")
	if err != nil {
		t.Fatalf("LoadTargets returned error: %v", err)
	}

	_, err = targets.Find("intranet-backend", "production")
	if err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
}

func TestLoadTargets_ReadsConfiguredTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	json := `[
		{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"Intranet Backend","secretsTarget":"appsettings.Production.json","keepVersions":5},
		{"repo":"intranet-frontend","environment":"test","recipe":"npm","siteName":"Intranet Frontend Test"}
	]`
	if err := os.WriteFile(path, []byte(json), 0o640); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatalf("LoadTargets returned error: %v", err)
	}

	backend, err := targets.Find("intranet-backend", "production")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if backend.Recipe != deploy.RecipeDotnet || backend.SiteName != "Intranet Backend" || backend.KeepVersions != 5 {
		t.Errorf("backend target = %+v, unexpected fields", backend)
	}

	frontend, err := targets.Find("intranet-frontend", "test")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if frontend.KeepVersions != 5 {
		t.Errorf("KeepVersions = %d, want default of 5 when omitted", frontend.KeepVersions)
	}

	if _, err := targets.Find("intranet-backend", "test"); err != ErrNoTarget {
		t.Errorf("err = %v, want ErrNoTarget for an unconfigured (repo, environment) pair", err)
	}
}

func TestFind_NilTargetsIsSafe(t *testing.T) {
	var targets *Targets

	_, err := targets.Find("intranet-backend", "production")
	if err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
	if envs := targets.Environments("intranet-backend"); len(envs) != 0 {
		t.Errorf("Environments = %v, want empty", envs)
	}
}

func TestEnvironments_ReturnsOnlyMatchingRepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	json := `[
		{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"A"},
		{"repo":"intranet-backend","environment":"test","recipe":"dotnet","siteName":"B"},
		{"repo":"intranet-frontend","environment":"production","recipe":"npm","siteName":"C"}
	]`
	if err := os.WriteFile(path, []byte(json), 0o640); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatalf("LoadTargets returned error: %v", err)
	}

	envs := targets.Environments("intranet-backend")
	if len(envs) != 2 {
		t.Fatalf("got %d environments, want 2: %v", len(envs), envs)
	}
}
