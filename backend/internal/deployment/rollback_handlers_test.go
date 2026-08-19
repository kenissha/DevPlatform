package deployment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
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
