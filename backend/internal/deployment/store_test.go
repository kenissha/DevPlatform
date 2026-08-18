package deployment

import (
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

func TestFind_ReturnsErrNoTargetWhenStoreIsEmpty(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")

	if _, err := store.Find("intranet-backend", "production"); err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
}

func TestSet_ThenFind_ReturnsTheStoredTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"Intranet Backend": true}

	target := Target{
		Repo:          "intranet-backend",
		Environment:   "production",
		Recipe:        deploy.RecipeDotnet,
		SiteName:      "Intranet Backend",
		SecretsTarget: "appsettings.Production.json",
		KeepVersions:  3,
	}
	if err := store.Set(target, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got, err := store.Find("intranet-backend", "production")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if got.SiteName != "Intranet Backend" || got.KeepVersions != 3 || got.SecretsTarget != "appsettings.Production.json" {
		t.Errorf("got %+v, unexpected fields", got)
	}
}

func TestSet_DefaultsKeepVersionsTo5WhenOmitted(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true}

	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	got, err := store.Find("r", "e")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if got.KeepVersions != 5 {
		t.Errorf("KeepVersions = %d, want default of 5", got.KeepVersions)
	}
}

func TestSet_ReplacesAnExistingTargetRatherThanDuplicating(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true}

	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("first Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeDotnet, SiteName: "B", KeepVersions: 9}, allowed); err != nil {
		t.Fatalf("second Set returned error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d targets, want 1 (replaced, not duplicated)", len(list))
	}
	if list[0].Recipe != deploy.RecipeDotnet || list[0].SiteName != "B" || list[0].KeepVersions != 9 {
		t.Errorf("list[0] = %+v, want the replacement's fields", list[0])
	}
}

func TestSet_RejectsASiteNameNotInTheAllowList(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"Approved Site": true}

	err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "Someone Else's Site"}, allowed)
	if err == nil {
		t.Fatal("Set returned no error for a site name outside the allow-list")
	}
	if _, findErr := store.Find("r", "e"); findErr != ErrNoTarget {
		t.Error("expected the rejected target to not be persisted")
	}
}

func TestSet_RejectsInvalidFields(t *testing.T) {
	allowed := map[string]bool{"A": true}
	tests := []struct {
		name   string
		target Target
	}{
		{"empty repo", Target{Repo: "", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"repo escaping its directory", Target{Repo: "../etc", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"empty environment", Target{Repo: "r", Environment: "", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"unknown recipe", Target{Repo: "r", Environment: "e", Recipe: "make", SiteName: "A"}},
		{"empty siteName", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: ""}},
		{"secretsTarget escaping the release", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A", SecretsTarget: "../../appsettings.json"}},
		{"absolute secretsTarget", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A", SecretsTarget: "C:/inetpub/appsettings.json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
			if err := store.Set(tc.target, allowed); err == nil {
				t.Fatal("Set returned no error, want the target rejected")
			}
		})
	}
}

func TestDelete_RemovesTheTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true}
	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if err := store.Delete("r", "e"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := store.Find("r", "e"); err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
}

func TestDelete_NonexistentTargetIsNotAnError(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")

	if err := store.Delete("r", "e"); err != nil {
		t.Errorf("Delete on a nonexistent target returned error: %v", err)
	}
}

func TestEnvironments_ReturnsOnlyMatchingRepo(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true, "C": true}
	if err := store.Set(Target{Repo: "intranet-backend", Environment: "production", Recipe: deploy.RecipeDotnet, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "intranet-backend", Environment: "test", Recipe: deploy.RecipeDotnet, SiteName: "B"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "intranet-frontend", Environment: "production", Recipe: deploy.RecipeNpm, SiteName: "C"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	envs := store.Environments("intranet-backend")
	if len(envs) != 2 {
		t.Fatalf("got %d environments, want 2: %v", len(envs), envs)
	}
}

func TestList_ReturnsEveryTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true}
	if err := store.Set(Target{Repo: "r1", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "r2", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "B"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d targets, want 2", len(list))
	}
}

func TestTargetStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/deploy-targets.json"
	store1 := NewTargetStore(path)
	allowed := map[string]bool{"A": true}
	if err := store1.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	store2 := NewTargetStore(path)
	if _, err := store2.Find("r", "e"); err != nil {
		t.Errorf("a fresh TargetStore instance backed by the same file does not see the earlier Set: %v", err)
	}
}
