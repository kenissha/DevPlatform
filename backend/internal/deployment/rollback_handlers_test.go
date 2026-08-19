package deployment

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// deployOnce creates and approves one deploy request against sample/test,
// the same create-then-approve pair TestApprove_RunsTheFullPipelineAgainstAFakeIIS
// already establishes, factored out here because the rollback tests below
// each need at least two real, on-disk releases to roll back between.
func deployOnce(t *testing.T, mux *http.ServeMux) Request {
	t.Helper()

	createBody, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(createBody))
	createReq = addAuth(createReq, t, "dev-1", "developer")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var created Request
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
	approveReq = addAuth(approveReq, t, "admin-1", "admin")
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d, body: %s", approveRec.Code, http.StatusOK, approveRec.Body.String())
	}
	var deployed Request
	if err := json.Unmarshal(approveRec.Body.Bytes(), &deployed); err != nil {
		t.Fatalf("failed to decode approve response: %v", err)
	}
	if deployed.Status != StatusDeployed {
		t.Fatalf("Status = %q, want %q (failure reason: %q)", deployed.Status, StatusDeployed, deployed.FailureReason)
	}
	return deployed
}

func TestReleases_ListsReleasesNewestFirstWithActiveMarked(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

	first := deployOnce(t, mux)
	second := deployOnce(t, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/deployments/test/releases", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var releases []releaseInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &releases); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2: %+v", len(releases), releases)
	}

	firstName := filepath.Base(first.ReleaseDir)
	secondName := filepath.Base(second.ReleaseDir)
	if releases[0].Name != secondName {
		t.Errorf("releases[0].Name = %q, want the newest release %q first", releases[0].Name, secondName)
	}
	if !releases[0].Active {
		t.Error("expected the newest (most recently deployed) release to be marked active")
	}
	if releases[1].Name != firstName {
		t.Errorf("releases[1].Name = %q, want %q", releases[1].Name, firstName)
	}
	if releases[1].Active {
		t.Error("expected the older release to not be marked active")
	}
}

func TestRollback_PointsIISBackAtAnOlderReleaseAndRecordsIt(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

	first := deployOnce(t, mux)
	deployOnce(t, mux)
	if len(runner.calls) != 2 {
		t.Fatalf("got %d IIS calls after two deploys, want 2: %v", len(runner.calls), runner.calls)
	}

	body, _ := json.Marshal(map[string]string{"release": filepath.Base(first.ReleaseDir)})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/test/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var rolledBack Request
	if err := json.Unmarshal(rec.Body.Bytes(), &rolledBack); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rolledBack.Kind != KindRollback {
		t.Errorf("Kind = %q, want %q", rolledBack.Kind, KindRollback)
	}
	if rolledBack.Status != StatusDeployed {
		t.Errorf("Status = %q, want %q", rolledBack.Status, StatusDeployed)
	}
	if rolledBack.ReleaseDir != first.ReleaseDir {
		t.Errorf("ReleaseDir = %q, want the rolled-back-to release %q", rolledBack.ReleaseDir, first.ReleaseDir)
	}

	// A third appcmd call happened, pointing at the OLD release — no new
	// build, no new release directory, just the swap.
	if len(runner.calls) != 3 {
		t.Fatalf("got %d IIS calls after rollback, want 3: %v", len(runner.calls), runner.calls)
	}
	lastCall := runner.calls[len(runner.calls)-1]
	if lastCall[len(lastCall)-1] != "/physicalPath:"+first.ReleaseDir {
		t.Errorf("last appcmd call = %v, want it to target %q", lastCall, first.ReleaseDir)
	}

	// Releases now reports the rolled-back-to release as active.
	listReq := httptest.NewRequest(http.MethodGet, "/api/repos/sample/deployments/test/releases", nil)
	listReq = addAuth(listReq, t, "admin-1", "admin")
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	var releases []releaseInfo
	if err := json.Unmarshal(listRec.Body.Bytes(), &releases); err != nil {
		t.Fatalf("failed to decode releases response: %v", err)
	}
	activeName := filepath.Base(first.ReleaseDir)
	found := false
	for _, r := range releases {
		if r.Name == activeName {
			found = true
			if !r.Active {
				t.Error("expected the rolled-back-to release to now be marked active")
			}
		} else if r.Active {
			t.Errorf("release %q unexpectedly marked active after rolling back to %q", r.Name, activeName)
		}
	}
	if !found {
		t.Fatalf("rolled-back-to release %q missing from releases listing: %+v", activeName, releases)
	}

	// Audit recorded the rollback.
	events, err := h.Audit.List(10)
	if err != nil {
		t.Fatalf("Audit.List returned error: %v", err)
	}
	if events[0].Action != audit.ActionDeploymentRollback {
		t.Errorf("newest audit action = %q, want %q", events[0].Action, audit.ActionDeploymentRollback)
	}
}

func TestRollback_RejectsUnknownRelease(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

	deployOnce(t, mux)
	callsBefore := len(runner.calls)

	body, _ := json.Marshal(map[string]string{"release": "20000101T000000.000000000"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/test/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(runner.calls) != callsBefore {
		t.Errorf("got %d IIS calls, want unchanged %d — a rejected rollback must never touch IIS", len(runner.calls), callsBefore)
	}
}

func TestRollback_RejectsNonAdmin(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

	first := deployOnce(t, mux)
	callsBefore := len(runner.calls)

	body, _ := json.Marshal(map[string]string{"release": filepath.Base(first.ReleaseDir)})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/test/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if len(runner.calls) != callsBefore {
		t.Errorf("got %d IIS calls, want unchanged %d — a forbidden rollback must never touch IIS", len(runner.calls), callsBefore)
	}
}

func TestReleases_RejectsNonAdmin(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

	deployOnce(t, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/deployments/test/releases", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// dotnetFailStartRunner fails a given number of "start site" appcmd calls
// in order, then succeeds — lets
// TestRollback_DotnetRecipeDoesNotRecordAnythingWhenStartFails exercise
// ActivateRelease's revert path without modifying the shared
// fakeCommandRunner in handlers_test.go (which has no failStart support and
// is out of scope for this change).
type dotnetFailStartRunner struct {
	failStart []error
}

func (f *dotnetFailStartRunner) Run(name string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "start" && len(f.failStart) > 0 {
		err := f.failStart[0]
		f.failStart = f.failStart[1:]
		if err != nil {
			return nil, err
		}
	}
	return []byte("ok"), nil
}

// newDotnetTestHandlers wires a Handlers around a fake dotnet-recipe deploy
// target ("backend-sample"/"prod") with two releases already on disk, as if
// two ordinary deploys already happened — Rollback only ever operates on
// releases Versions.List already reports, it never creates one. Unlike
// newTestHandlers, this does not use the npm-recipe "sample/test" target,
// since these tests exist specifically to isolate dotnet-recipe behavior.
func newDotnetTestHandlers(t *testing.T, runner deploy.CommandRunner) *Handlers {
	t.Helper()
	dataDir := t.TempDir()

	// repoExists() checks Repos.List(), so a bare repo named
	// "backend-sample" must exist even though nothing is ever pushed to
	// it — Rollback never touches the repo itself, only Targets/Versions.
	if _, err := repostore.New(dataDir).Create("backend-sample"); err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	targets := NewTargetStore(filepath.Join(dataDir, "deploy-targets.json"))
	if err := targets.Set(
		Target{Repo: "backend-sample", Environment: "prod", Recipe: deploy.RecipeDotnet, SiteName: "Fake Backend Site"},
		map[string]bool{"Fake Backend Site": true},
	); err != nil {
		t.Fatalf("failed to seed dotnet deploy target: %v", err)
	}

	versions := deploy.NewVersionStore(filepath.Join(dataDir, "releases"))
	store := NewStore(filepath.Join(dataDir, "deployments"))

	// Two releases already on disk, as if two ordinary deploys already
	// happened — Rollback only ever operates on releases Versions.List
	// already reports, it never creates one.
	first, err := versions.NewRelease("backend-sample", "prod")
	if err != nil {
		t.Fatalf("failed to seed first release: %v", err)
	}
	if _, err := store.CreateRollback("backend-sample", "prod", first, "system"); err != nil {
		t.Fatalf("failed to record first release as active: %v", err)
	}
	second, err := versions.NewRelease("backend-sample", "prod")
	if err != nil {
		t.Fatalf("failed to seed second release: %v", err)
	}
	if _, err := store.CreateRollback("backend-sample", "prod", second, "system"); err != nil {
		t.Fatalf("failed to record second release as active: %v", err)
	}

	return &Handlers{
		Store:    store,
		Repos:    repostore.New(dataDir),
		Targets:  targets,
		Versions: versions,
		IIS:      deploy.NewIISSwapper(runner),
		Audit:    audit.New(filepath.Join(dataDir, "audit.jsonl")),
	}
}

func TestRollback_DotnetRecipeStopsSwapsThenStarts(t *testing.T) {
	runner := &fakeCommandRunner{}
	h := newDotnetTestHandlers(t, runner)
	mux := newMux(h)

	releases, err := h.Versions.List("backend-sample", "prod")
	if err != nil || len(releases) != 2 {
		t.Fatalf("expected 2 seeded releases, got %v (err: %v)", releases, err)
	}
	olderRelease := filepath.Base(releases[1]) // List is newest-first

	body, _ := json.Marshal(map[string]string{"release": olderRelease})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/backend-sample/deployments/prod/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(runner.calls) != 3 {
		t.Fatalf("got %d IIS calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" || runner.calls[2][1] != "start" {
		t.Errorf("calls = %v, want stop first and start last", runner.calls)
	}
}

func TestRollback_DotnetRecipeDoesNotRecordAnythingWhenStartFails(t *testing.T) {
	runner := &dotnetFailStartRunner{failStart: []error{errors.New("simulated: target release crashes on start")}}
	h := newDotnetTestHandlers(t, runner)
	mux := newMux(h)

	releases, err := h.Versions.List("backend-sample", "prod")
	if err != nil || len(releases) != 2 {
		t.Fatalf("expected 2 seeded releases, got %v (err: %v)", releases, err)
	}
	olderRelease := filepath.Base(releases[1])

	before, err := h.Store.List("backend-sample")
	if err != nil {
		t.Fatalf("Store.List failed: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"release": olderRelease})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/backend-sample/deployments/prod/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	after, err := h.Store.List("backend-sample")
	if err != nil {
		t.Fatalf("Store.List failed: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("got %d requests after a failed rollback, want unchanged %d — a failed rollback must never be recorded as one", len(after), len(before))
	}
}
