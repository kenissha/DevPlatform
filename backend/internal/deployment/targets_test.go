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

// writeTargetsFile writes a targets fixture and returns its path.
func writeTargetsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(path, []byte(contents), 0o640); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	return path
}

// TestLoadTargets_RejectsInvalidEntries pins down what a bad targets file
// must do: fail loading, which in main.go means the server refuses to
// start, rather than run with a config whose entries decide which live IIS
// site a deploy overwrites.
func TestLoadTargets_RejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			// The dangerous one: (repo, environment) is the deploy lookup
			// key, so a repeat silently decided which site "production" meant.
			name: "duplicate repo and environment",
			json: `[
				{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"Intranet Backend"},
				{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"Someone Else's Site"}
			]`,
		},
		{
			name: "unknown recipe",
			json: `[{"repo":"intranet-backend","environment":"production","recipe":"make","siteName":"Intranet Backend"}]`,
		},
		{
			name: "secretsTarget escaping the release",
			json: `[{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"A","secretsTarget":"../../appsettings.json"}]`,
		},
		{
			name: "absolute secretsTarget",
			json: `[{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"A","secretsTarget":"C:/inetpub/appsettings.json"}]`,
		},
		{
			name: "empty repo",
			json: `[{"repo":"","environment":"production","recipe":"dotnet","siteName":"A"}]`,
		},
		{
			name: "empty environment",
			json: `[{"repo":"intranet-backend","environment":"","recipe":"dotnet","siteName":"A"}]`,
		},
		{
			name: "empty siteName",
			json: `[{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":""}]`,
		},
		{
			name: "repo escaping its directory",
			json: `[{"repo":"../etc","environment":"production","recipe":"dotnet","siteName":"A"}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadTargets(writeTargetsFile(t, tc.json)); err == nil {
				t.Fatal("LoadTargets returned no error, want the config rejected")
			}
		})
	}
}

func TestLoadTargets_AcceptsAValidFile(t *testing.T) {
	path := writeTargetsFile(t, `[
		{"repo":"intranet-backend","environment":"production","recipe":"dotnet","siteName":"Intranet Backend","secretsTarget":"appsettings.Production.json"},
		{"repo":"intranet-backend","environment":"test","recipe":"dotnet","siteName":"Intranet Backend Test"},
		{"repo":"intranet-frontend","environment":"production","recipe":"npm","siteName":"Intranet Frontend"}
	]`)

	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatalf("LoadTargets returned error: %v", err)
	}
	if _, err := targets.Find("intranet-backend", "production"); err != nil {
		t.Errorf("Find returned error for a valid target: %v", err)
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
