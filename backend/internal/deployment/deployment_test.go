package deployment

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestCreate_OmitsDecidedAtFromJSONWhilePending(t *testing.T) {
	// Regression test: DecidedAt used to be a bare time.Time, and
	// encoding/json's omitempty never treats a struct type as "empty"
	// regardless of its value — so a pending request's zero DecidedAt
	// serialized as "0001-01-01T00:00:00Z" and the frontend's `d.decidedAt
	// && ...` check rendered a nonsense "karar: 01 Oca 1" line for every
	// request that had never been decided.
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if strings.Contains(string(encoded), "decidedAt") {
		t.Errorf("encoded pending request contains \"decidedAt\": %s", encoded)
	}
}

func TestCreate_PersistsAndReturnsPendingRequest(t *testing.T) {
	store := NewStore(t.TempDir())

	req, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if req.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if req.Status != StatusPending {
		t.Errorf("Status = %q, want %q", req.Status, StatusPending)
	}
	if req.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGet_ReturnsCreatedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != created {
		t.Errorf("got %+v, want %+v", got, created)
	}
}

func TestCreateRollback_PersistsAsTerminalDeployedWithKind(t *testing.T) {
	store := NewStore(t.TempDir())

	req, err := store.CreateRollback("intranet-backend", "production", `C:\releases\5`, "admin-1")
	if err != nil {
		t.Fatalf("CreateRollback returned error: %v", err)
	}
	if req.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if req.Kind != KindRollback {
		t.Errorf("Kind = %q, want %q", req.Kind, KindRollback)
	}
	if req.Status != StatusDeployed {
		t.Errorf("Status = %q, want %q", req.Status, StatusDeployed)
	}
	if req.ReleaseDir != `C:\releases\5` {
		t.Errorf("ReleaseDir = %q, want %q", req.ReleaseDir, `C:\releases\5`)
	}
	if req.SourceBranch != "" {
		t.Errorf("SourceBranch = %q, want empty for a rollback record", req.SourceBranch)
	}
	if req.DecidedAt == nil {
		t.Error("expected DecidedAt to be set immediately — a rollback has no pending stage")
	}

	// Persisted, not just returned in memory: Get must see the same record.
	got, err := store.Get("intranet-backend", req.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Kind != KindRollback || got.Status != StatusDeployed {
		t.Errorf("Get returned %+v, want Kind=%q Status=%q", got, KindRollback, StatusDeployed)
	}
}

func TestGet_ReturnsErrNotFoundForMissingID(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Get("intranet-backend", "0123456789abcdef")
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_ReturnsNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Create("intranet-backend", "test", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := store.List("intranet-backend")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d requests, want 2", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("expected newest-first order [%s, %s], got [%s, %s]",
			second.ID, first.ID, list[0].ID, list[1].ID)
	}
}

func TestDecide_TransitionsToDeployedAndRecordsReleaseDir(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.Decide("intranet-backend", created.ID, StatusDeployed, "/releases/123", "")
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if updated.Status != StatusDeployed || updated.ReleaseDir != "/releases/123" {
		t.Errorf("updated = %+v, unexpected fields", updated)
	}
	if updated.DecidedAt.IsZero() {
		t.Error("expected DecidedAt to be set")
	}

	reread, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get after Decide failed: %v", err)
	}
	if reread.Status != StatusDeployed || reread.ReleaseDir != "/releases/123" {
		t.Errorf("persisted = %+v, unexpected fields", reread)
	}
}

func TestDecide_RecordsFailureReason(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := store.Decide("intranet-backend", created.ID, StatusFailed, "", "build failed: exit status 1")
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if updated.Status != StatusFailed || updated.FailureReason != "build failed: exit status 1" {
		t.Errorf("updated = %+v, unexpected fields", updated)
	}
}

// TestClaim_OnlyOneOfManyConcurrentCallersWins is the store-level half of
// the fix for overlapping deploys: whichever caller wins the claim is the
// only one allowed to go on and build/swap IIS, so exactly one of N
// concurrent claims may succeed. Run with -race.
func TestClaim_OnlyOneOfManyConcurrentCallersWins(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = store.Claim("intranet-backend", created.ID)
		}(i)
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		switch err {
		case nil:
			won++
		case ErrNotPending:
		default:
			t.Errorf("unexpected Claim error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d callers won the claim, want exactly 1", won)
	}

	claimed, err := store.Get("intranet-backend", created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if claimed.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", claimed.Status, StatusInProgress)
	}
}

func TestDecide_RecordsOutcomeOfAClaimedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := store.Claim("intranet-backend", created.ID); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	// The claimant must still be able to record its own outcome — a claim
	// reserves the deploy, it doesn't lock the request out of being decided.
	updated, err := store.Decide("intranet-backend", created.ID, StatusDeployed, "/releases/123", "")
	if err != nil {
		t.Fatalf("Decide after Claim returned error: %v", err)
	}
	if updated.Status != StatusDeployed {
		t.Errorf("Status = %q, want %q", updated.Status, StatusDeployed)
	}

	if err := store.Claim("intranet-backend", created.ID); err != ErrNotPending {
		t.Errorf("err = %v, want ErrNotPending when claiming a finished request", err)
	}
}

func TestDecide_RejectsAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	created, err := store.Create("intranet-backend", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.Decide("intranet-backend", created.ID, StatusRejected, "", ""); err != nil {
		t.Fatalf("first Decide failed: %v", err)
	}

	_, err = store.Decide("intranet-backend", created.ID, StatusDeployed, "/x", "")
	if err != ErrNotPending {
		t.Fatalf("err = %v, want ErrNotPending", err)
	}
}

// ------------------------------------------- description & rejection note

func TestCreate_StoresTheDescription(t *testing.T) {
	store := NewStore(t.TempDir())

	req, err := store.Create("sample", "production", "main", "  Hakem raporu düzeltmesi canlıya çıksın.  ", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if req.Description != "Hakem raporu düzeltmesi canlıya çıksın." {
		t.Errorf("description = %q, want it stored and trimmed", req.Description)
	}

	reread, err := store.Get("sample", req.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Description != req.Description {
		t.Errorf("re-read description = %q, want it to survive the round trip", reread.Description)
	}
}

// Requests written before the field existed carry no "description" key;
// they must read back as empty rather than needing a migration.
func TestGet_TreatsAMissingDescriptionAsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	req, err := store.Create("sample", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	reread, err := store.Get("sample", req.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.Description != "" || reread.DecisionNote != "" {
		t.Errorf("got %q / %q, want both empty", reread.Description, reread.DecisionNote)
	}
}

func TestReject_RecordsTheReason(t *testing.T) {
	store := NewStore(t.TempDir())
	req, err := store.Create("sample", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	rejected, err := store.Reject("sample", req.ID, "  Önce test ortamında dene.  ")
	if err != nil {
		t.Fatalf("Reject failed: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Errorf("status = %q, want %q", rejected.Status, StatusRejected)
	}
	if rejected.DecisionNote != "Önce test ortamında dene." {
		t.Errorf("decisionNote = %q, want it stored and trimmed", rejected.DecisionNote)
	}
	if rejected.DecidedAt == nil {
		t.Error("decidedAt is nil, want it stamped")
	}

	reread, err := store.Get("sample", req.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if reread.DecisionNote != rejected.DecisionNote {
		t.Errorf("re-read note = %q, want it persisted", reread.DecisionNote)
	}
}

func TestReject_WorksWithoutAReason(t *testing.T) {
	store := NewStore(t.TempDir())
	req, err := store.Create("sample", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	rejected, err := store.Reject("sample", req.ID, "")
	if err != nil {
		t.Fatalf("Reject failed: %v", err)
	}
	if rejected.Status != StatusRejected || rejected.DecisionNote != "" {
		t.Errorf("got %q / %q, want rejected with no note", rejected.Status, rejected.DecisionNote)
	}
}

// Rejecting a build that is already running would record a decision the
// deploy is about to overwrite with its own outcome.
func TestReject_RefusesARequestThatIsAlreadyRunning(t *testing.T) {
	store := NewStore(t.TempDir())
	req, err := store.Create("sample", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := store.Claim("sample", req.ID); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	if _, err := store.Reject("sample", req.ID, "geç kaldım"); !errors.Is(err, ErrNotPending) {
		t.Errorf("Reject of an in-progress request = %v, want ErrNotPending", err)
	}
}

func TestReject_RefusesAnAlreadyDecidedRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	req, err := store.Create("sample", "production", "main", "", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.Reject("sample", req.ID, "hayır"); err != nil {
		t.Fatalf("first Reject failed: %v", err)
	}

	if _, err := store.Reject("sample", req.ID, "yine hayır"); !errors.Is(err, ErrNotPending) {
		t.Errorf("second Reject = %v, want ErrNotPending", err)
	}
}
