package deployment

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

const testJWTSecret = "test-secret"

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found on PATH, skipping integration test")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput:\n%s", args, err, out)
	}
	return string(out)
}

// fakeCommandRunner stands in for appcmd.exe — mirrors internal/deploy's
// own unexported test fake (can't be imported across packages), so the
// IIS "swap" step in these tests never touches a real IIS installation.
type fakeCommandRunner struct {
	calls [][]string
}

func (f *fakeCommandRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return []byte("ok"), nil
}

// failingCommandRunner simulates appcmd.exe itself failing (e.g. access
// denied), so Approve's failure path can be exercised without needing a
// real broken IIS to reproduce one.
type failingCommandRunner struct{}

func (failingCommandRunner) Run(name string, args ...string) ([]byte, error) {
	return nil, errors.New("appcmd exited 5: access denied")
}

// hangingVersionStore stands in for deploy.VersionStore and never returns
// from NewRelease until released, simulating a deploy step that wedges
// (the real-world case being a hung `npm run build`). It satisfies the
// unexported releaseStore interface deploy.NewPipeline accepts, which is
// what lets a test hang the pipeline right after checkout — before the
// slow real build — so Approve's timeout path can be exercised in
// milliseconds.
type hangingVersionStore struct {
	release chan struct{}
}

func (h hangingVersionStore) NewRelease(repo, environment string) (string, error) {
	<-h.release
	return "", errors.New("release store released after the test finished")
}

func (h hangingVersionStore) Prune(repo, environment string, keep int) error { return nil }

func signTestToken(t *testing.T, subject, role string) string {
	t.Helper()
	c := jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

func addAuth(r *http.Request, t *testing.T, subject, role string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	return r
}

func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", authMW(http.HandlerFunc(h.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Approve))))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Reject))))
	mux.Handle("GET /api/deployments", authMW(http.HandlerFunc(h.ListAll)))
	return mux
}

// newTestHandlers creates a bare repo named "sample" with the npm build
// fixture pushed to "main", and Handlers wired to a real Pipeline whose
// only faked collaborator is the IIS command runner. Returns the runner
// too, so a test can inspect what it would have told appcmd.exe.
func newTestHandlers(t *testing.T, runner deploy.CommandRunner) (*Handlers, *repostore.Store) {
	t.Helper()
	requireGit(t)

	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	repoPath, err := repos.Create("sample")
	if err != nil {
		t.Fatalf("failed to create bare repo: %v", err)
	}

	fixtureSrc, err := filepath.Abs("../deploy/testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	runGit(t, work, "remote", "add", "origin", repoPath)
	copyFile(t, filepath.Join(fixtureSrc, "package.json"), filepath.Join(work, "package.json"))
	copyFile(t, filepath.Join(fixtureSrc, "build.js"), filepath.Join(work, "build.js"))
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "push", "origin", "main")

	targets := NewTargets([]Target{
		{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Fake Site"},
	})

	pipeline := deploy.NewPipeline(
		&deploy.Builder{},
		deploy.NewVersionStore(filepath.Join(dataDir, "releases")),
		deploy.NewIISSwapper(runner),
		nil, // no secretsTarget configured, so a nil secretsvault.Store is never touched
	)

	h := &Handlers{
		Store:        NewStore(filepath.Join(dataDir, "deployments")),
		Repos:        repos,
		Targets:      targets,
		Pipeline:     pipeline,
		CheckoutRoot: t.TempDir(),
		Audit:        audit.New(filepath.Join(dataDir, "audit.jsonl")),
		Notify:       notify.NewStore(filepath.Join(dataDir, "notifications")),
		Users:        users.NewStore(filepath.Join(dataDir, "users.json")),
	}
	return h, repos
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", dst, err)
	}
}

func TestCreate_RejectsUnconfiguredTarget(t *testing.T) {
	h, _ := newTestHandlers(t, &fakeCommandRunner{})
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"environment": "production", "sourceBranch": "main"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreate_ReturnsPendingRequestForConfiguredTarget(t *testing.T) {
	h, _ := newTestHandlers(t, &fakeCommandRunner{})
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created Request
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.Status != StatusPending || created.Author != "dev-1" {
		t.Errorf("created = %+v, unexpected fields", created)
	}
}

// TestApprove_RunsTheFullPipelineAgainstAFakeIIS is the end-to-end proof
// this package exists for: create → approve actually checks out the real
// commit from the bare repo, runs `npm run build` for real, versions the
// output, and "swaps" IIS — everything except the last step is real; only
// appcmd.exe is faked, exactly the way internal/deploy's own tests avoid
// needing Administrator privileges.
func TestApprove_RunsTheFullPipelineAgainstAFakeIIS(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)

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
	if deployed.ReleaseDir == "" {
		t.Fatal("expected ReleaseDir to be set")
	}

	// The build genuinely ran: its known output must exist in the release.
	indexHTML, err := os.ReadFile(filepath.Join(deployed.ReleaseDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read built index.html from release dir: %v", err)
	}
	if !bytes.Contains(indexHTML, []byte("deploy fixture build ok")) {
		t.Errorf("index.html content = %q, missing expected marker", indexHTML)
	}
	if _, err := os.Stat(filepath.Join(deployed.ReleaseDir, "assets", "style.css")); err != nil {
		t.Errorf("expected nested assets/style.css to be copied into the release: %v", err)
	}

	// The IIS swap happened exactly once, against the fake.
	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls to the command runner, want 1: %v", len(runner.calls), runner.calls)
	}

	// Audit recorded both the open and the successful deploy.
	events, err := h.Audit.List(10)
	if err != nil {
		t.Fatalf("Audit.List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d audit events, want 2: %+v", len(events), events)
	}
	if events[0].Action != audit.ActionDeploymentSuccess {
		t.Errorf("newest audit action = %q, want %q", events[0].Action, audit.ActionDeploymentSuccess)
	}

	// The author was notified of the outcome.
	notifications, err := h.Notify.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 || notifications[0].Kind != "deployment_decided" {
		t.Fatalf("notifications = %+v, want a single deployment_decided entry", notifications)
	}

	// The request can't be approved a second time.
	replayRec := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
	replayReq = addAuth(replayReq, t, "admin-1", "admin")
	mux.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusConflict {
		t.Errorf("re-approve status = %d, want %d", replayRec.Code, http.StatusConflict)
	}
}

func TestApprove_RecordsFailureWhenIISSwapFails(t *testing.T) {
	h, _ := newTestHandlers(t, failingCommandRunner{})
	mux := newMux(h)

	createBody, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(createBody))
	createReq = addAuth(createReq, t, "dev-1", "developer")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	var created Request
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
	approveReq = addAuth(approveReq, t, "admin-1", "admin")
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)

	// The approve HTTP call itself succeeds — it processed the request and
	// recorded a real outcome, which happens to be failure.
	if approveRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", approveRec.Code, http.StatusOK, approveRec.Body.String())
	}
	var failed Request
	if err := json.Unmarshal(approveRec.Body.Bytes(), &failed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if failed.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q", failed.Status, StatusFailed)
	}
	if failed.FailureReason == "" {
		t.Error("expected a non-empty FailureReason")
	}

	// The reason names the stage that failed and nothing else: the raw
	// error text (here appcmd's own output, in a real build its entire
	// stdout/stderr, plus absolute release paths) must never reach the
	// panel, which anyone with access to the repo can read.
	if failed.FailureReason != "IIS yayınlama başarısız" {
		t.Errorf("FailureReason = %q, want the classified IIS message", failed.FailureReason)
	}
	for _, leak := range []string{"access denied", "appcmd", "deploy:", "physical path"} {
		if strings.Contains(strings.ToLower(failed.FailureReason), leak) {
			t.Errorf("FailureReason %q leaks raw error text %q", failed.FailureReason, leak)
		}
	}

	// The author's notification is the same classified message, for the
	// same reason — it's the other place the raw error used to land.
	notifications, err := h.Notify.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if strings.Contains(strings.ToLower(notifications[0].Message), "access denied") {
		t.Errorf("notification %q leaks raw error text", notifications[0].Message)
	}
}

// TestApprove_ConcurrentApprovalsDeployOnlyOnce is the regression test for
// the overlapping-deploy race: two admins approving the same request at
// the same moment both used to pass the pending check and both ran the
// whole pipeline, racing each other's IIS swap on the same live site. Run
// with -race — the unsynchronized fakeCommandRunner below would also flag
// a genuine concurrent second pipeline run as a data race.
func TestApprove_ConcurrentApprovalsDeployOnlyOnce(t *testing.T) {
	runner := &fakeCommandRunner{}
	h, _ := newTestHandlers(t, runner)
	mux := newMux(h)
	created := createTestRequest(t, mux)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
			req = addAuth(req, t, "admin-1", "admin")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	ok, conflict := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("unexpected approve status %d", code)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("got %d OK and %d conflict responses, want exactly 1 of each: %v", ok, conflict, codes)
	}

	// The decisive assertion: IIS was swapped once, not twice.
	if len(runner.calls) != 1 {
		t.Fatalf("got %d IIS swap calls, want exactly 1: %v", len(runner.calls), runner.calls)
	}

	deployed, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if deployed.Status != StatusDeployed {
		t.Errorf("Status = %q, want %q (failure reason: %q)", deployed.Status, StatusDeployed, deployed.FailureReason)
	}
}

// TestApprove_MarksFailedWhenTheDeployTimesOut covers the bounded-pipeline
// fix: a deploy step that never returns must not pin the handler forever
// and must not leave the request stuck mid-deploy with no way out.
func TestApprove_MarksFailedWhenTheDeployTimesOut(t *testing.T) {
	h, _ := newTestHandlers(t, &fakeCommandRunner{})

	release := make(chan struct{})
	// Released at cleanup so the abandoned pipeline goroutine can finish
	// before the test's temp dirs are removed.
	t.Cleanup(func() { close(release) })
	h.Pipeline = deploy.NewPipeline(
		&deploy.Builder{},
		hangingVersionStore{release: release},
		deploy.NewIISSwapper(&fakeCommandRunner{}),
		nil,
	)

	previous := deployTimeout
	deployTimeout = 200 * time.Millisecond
	t.Cleanup(func() { deployTimeout = previous })

	mux := newMux(h)
	created := createTestRequest(t, mux)

	approveReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
	approveReq = addAuth(approveReq, t, "admin-1", "admin")
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)

	if approveRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", approveRec.Code, http.StatusOK, approveRec.Body.String())
	}
	var failed Request
	if err := json.Unmarshal(approveRec.Body.Bytes(), &failed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if failed.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q — a timed-out deploy must not stay in progress", failed.Status, StatusFailed)
	}
	if failed.FailureReason != "Deploy zaman aşımına uğradı" {
		t.Errorf("FailureReason = %q, want the timeout message", failed.FailureReason)
	}

	// And it is genuinely terminal on disk, not just in the response.
	reread, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Status != StatusFailed {
		t.Errorf("persisted Status = %q, want %q", reread.Status, StatusFailed)
	}
}

// createTestRequest opens one deploy request against sample/test via the
// API, the way every approve test needs one to exist first.
func createTestRequest(t *testing.T, mux *http.ServeMux) Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created Request
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	return created
}

func TestApprove_RejectsNonAdmin(t *testing.T) {
	h, _ := newTestHandlers(t, &fakeCommandRunner{})
	mux := newMux(h)

	createBody, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(createBody))
	createReq = addAuth(createReq, t, "dev-1", "developer")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	var created Request
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/approve", nil)
	approveReq = addAuth(approveReq, t, "dev-1", "developer")
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)

	if approveRec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", approveRec.Code, http.StatusForbidden)
	}

	reread, err := h.Store.Get("sample", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Status != StatusPending {
		t.Errorf("Status = %q, want unchanged %q", reread.Status, StatusPending)
	}
}

func TestReject_MarksRejectedAndNotifiesAuthor(t *testing.T) {
	h, _ := newTestHandlers(t, &fakeCommandRunner{})
	mux := newMux(h)

	createBody, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(createBody))
	createReq = addAuth(createReq, t, "dev-1", "developer")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	var created Request
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments/"+created.ID+"/reject", nil)
	rejectReq = addAuth(rejectReq, t, "admin-1", "admin")
	rejectRec := httptest.NewRecorder()
	mux.ServeHTTP(rejectRec, rejectReq)

	if rejectRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rejectRec.Code, http.StatusOK, rejectRec.Body.String())
	}
	var rejected Request
	if err := json.Unmarshal(rejectRec.Body.Bytes(), &rejected); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("Status = %q, want %q", rejected.Status, StatusRejected)
	}

	notifications, err := h.Notify.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
}

func TestListAll_SpansRepos(t *testing.T) {
	h, repos := newTestHandlers(t, &fakeCommandRunner{})
	if _, err := repos.Create("other"); err != nil {
		t.Fatalf("failed to create second repo: %v", err)
	}
	h.Targets = NewTargets([]Target{
		{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "A"},
		{Repo: "other", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "B"},
	})
	mux := newMux(h)

	// "other" has no branches at all, but Create only needs a target and a
	// real branch to exist on "sample" for this test — skip creating a
	// request against "other" and just prove ListAll aggregates what
	// exists across repos already covered by "sample"'s two requests.
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
		req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(body))
		req = addAuth(req, t, "dev-1", "developer")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status = %d, want %d", rec.Code, http.StatusCreated)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/deployments", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	h.ListAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var all []Request
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d requests, want 2", len(all))
	}
}

func TestListAll_NarrowsToAllowedReposForARestrictedDeveloper(t *testing.T) {
	h, repos := newTestHandlers(t, &fakeCommandRunner{})
	if _, err := repos.Create("other"); err != nil {
		t.Fatalf("failed to create second repo: %v", err)
	}
	h.Targets = NewTargets([]Target{
		{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "A"},
	})
	h.Access = access.NewStore(t.TempDir() + "/access.json")
	if err := h.Access.Set("dev-1", []string{"other"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"environment": "test", "sourceBranch": "main"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/deployments", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/deployments", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var all []Request
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// dev-1 is restricted to "other", which has no deploy requests — the
	// request opened against "sample" (a repo they can't see) must not
	// appear even though it exists.
	if len(all) != 0 {
		t.Errorf("got %d requests, want 0 (sample is not in dev-1's allow-list)", len(all))
	}
}
