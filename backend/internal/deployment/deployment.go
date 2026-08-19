package deployment

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidRepo = fmt.Errorf("deployment: invalid repository name")
	ErrInvalidID   = fmt.Errorf("deployment: invalid deploy request id")
	ErrNotFound    = fmt.Errorf("deployment: not found")
	ErrNotPending  = fmt.Errorf("deployment: request is not pending")
)

// validRepoName mirrors repostore's, mergerequest's, and taskboard's own
// copy of this same allow-list — this package builds filesystem paths
// from repo names too.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// idPattern matches only IDs this package itself generates (see newID),
// so an ID coming from a URL path parameter can be validated before it is
// ever joined into a filesystem path.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Status is a deploy request's place in its review-then-act lifecycle.
type Status string

const (
	StatusPending Status = "pending"
	// StatusInProgress is the claimed-but-not-yet-finished state a request
	// sits in while its deploy actually runs (see Store.Claim). It exists
	// so a request that is mid-deploy is distinguishable from one still
	// waiting for an admin: a second, concurrent approval of the same
	// request is rejected the moment it sees this status, before it can
	// start a second build racing the first one onto the same IIS site.
	StatusInProgress Status = "in_progress"
	StatusDeployed   Status = "deployed"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

// Kind distinguishes a rollback record from an ordinary deploy request.
// The zero value (empty string) means an ordinary deploy — every request
// Create makes, and every request ever persisted before rollback existed,
// so old on-disk JSON with no "kind" field at all still decodes as an
// ordinary deploy.
type Kind string

const KindRollback Kind = "rollback"

// Request is a request to release repo's sourceBranch into environment.
// Approval runs the deploy synchronously and records the outcome in the
// same object — there is no separate "deploy log" to cross-reference,
// mirroring how MergeRequest records its own MergedCommit rather than
// pointing elsewhere for it.
type Request struct {
	ID          string `json:"id"`
	Repo        string `json:"repo"`
	Environment string `json:"environment"`
	// Kind is empty for every ordinary deploy request; CreateRollback is
	// the only thing that sets it to KindRollback. SourceBranch is
	// meaningless for a rollback record (a rollback deploys no branch, it
	// repoints IIS at a release an earlier deploy already built), so it is
	// left empty there — the frontend tells the two apart via Kind, not by
	// SourceBranch being blank.
	Kind          Kind      `json:"kind,omitempty"`
	SourceBranch  string    `json:"sourceBranch,omitempty"`
	Author        string    `json:"author"`
	Status        Status    `json:"status"`
	ReleaseDir    string    `json:"releaseDir,omitempty"`
	FailureReason string    `json:"failureReason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	// DecidedAt is a pointer, not a bare time.Time: encoding/json's
	// omitempty never treats a struct type as "empty" regardless of its
	// value, so a zero time.Time would serialize as "0001-01-01T..." for
	// every still-pending request instead of being omitted. A nil pointer
	// omits correctly and is what the frontend's optional field expects.
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

// Store persists deploy requests as one JSON file per request under
// rootDir, grouped in a per-repo subdirectory — the same flat-file
// approach mergerequest and taskboard already use.
type Store struct {
	rootDir string
	// mu serializes every status transition (Claim, Decide) so a
	// read-check-write pair can't interleave with another one: without it
	// two concurrent approvals of the same request both read StatusPending
	// and both go on to build and swap IIS. Get takes it too, so a reader
	// never observes a half-rewritten file. Callers must hold it before
	// calling any of the unexported get/write helpers, and those helpers
	// never take it themselves — that's what keeps Decide from deadlocking
	// against its own read.
	mu sync.Mutex
}

// NewStore returns a Store rooted at rootDir. rootDir does not need to
// exist yet.
func NewStore(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// Create persists a new, StatusPending request.
func (s *Store) Create(repo, environment, sourceBranch, author string) (Request, error) {
	if !validRepoName.MatchString(repo) {
		return Request{}, ErrInvalidRepo
	}
	return s.create(Request{
		Repo:         repo,
		Environment:  environment,
		SourceBranch: sourceBranch,
		Author:       author,
		Status:       StatusPending,
		CreatedAt:    time.Now().UTC(),
	})
}

// CreateRollback persists a new, already-terminal StatusDeployed request
// recording a rollback to releaseDir. Unlike Create, there is no
// pending/review stage: by the time this is called the IIS swap has
// already happened (see Handlers.Rollback) — a rollback is one immediate
// admin action against a release that was already built and live before,
// not a request to build and deploy new code.
func (s *Store) CreateRollback(repo, environment, releaseDir, author string) (Request, error) {
	if !validRepoName.MatchString(repo) {
		return Request{}, ErrInvalidRepo
	}
	now := time.Now().UTC()
	return s.create(Request{
		Repo:        repo,
		Environment: environment,
		Kind:        KindRollback,
		Author:      author,
		Status:      StatusDeployed,
		ReleaseDir:  releaseDir,
		CreatedAt:   now,
		DecidedAt:   &now,
	})
}

// create allocates a unique ID for req and persists it, retrying on the
// rare ID collision — shared by Create and CreateRollback, which differ
// only in what fields req arrives with already set. Like Create before
// this was extracted, it does not take s.mu: it only ever creates a new
// file (O_EXCL), never reads-then-writes an existing one, so it has
// nothing to race against Get/Claim/Decide over.
func (s *Store) create(req Request) (Request, error) {
	dir := filepath.Join(s.rootDir, req.Repo)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Request{}, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		id, err := newID()
		if err != nil {
			return Request{}, err
		}
		req.ID = id

		path := filepath.Join(dir, id+".json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return Request{}, err
		}
		err = json.NewEncoder(f).Encode(req)
		closeErr := f.Close()
		if err != nil {
			return Request{}, err
		}
		if closeErr != nil {
			return Request{}, closeErr
		}
		return req, nil
	}
	return Request{}, fmt.Errorf("deployment: failed to allocate a unique id after 5 attempts")
}

// Get returns the request identified by (repo, id).
func (s *Store) Get(repo, id string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(repo, id)
}

// get is Get without the lock; callers must already hold s.mu.
func (s *Store) get(repo, id string) (Request, error) {
	path, err := s.path(repo, id)
	if err != nil {
		return Request{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Request{}, ErrNotFound
		}
		return Request{}, err
	}

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// List returns every deploy request for repo, newest first.
func (s *Store) List(repo string) ([]Request, error) {
	if !validRepoName.MatchString(repo) {
		return nil, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Request{}, nil
		}
		return nil, err
	}

	requests := []Request{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	sort.Slice(requests, func(i, j int) bool {
		return requests[i].CreatedAt.After(requests[j].CreatedAt)
	})
	return requests, nil
}

// Claim atomically moves a StatusPending request to StatusInProgress,
// returning ErrNotPending if someone else got there first. It is how a
// caller reserves the right to actually run a deploy: it must be called
// before any build or IIS work starts, so that a second concurrent
// approval of the same request is turned away before it touches the
// filesystem or the live site — not minutes later, at Decide time, once
// it has already overwritten production.
func (s *Store) Claim(repo, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, err := s.get(repo, id)
	if err != nil {
		return err
	}
	if req.Status != StatusPending {
		return ErrNotPending
	}

	req.Status = StatusInProgress
	return s.write(req)
}

// Decide transitions the request identified by (repo, id) from
// StatusPending or StatusInProgress to a terminal status (StatusDeployed,
// StatusFailed, or StatusRejected), recording releaseDir and/or
// failureReason and stamping DecidedAt. It is the only way this store ever
// reaches a terminal status — there is no path back out of one, matching
// mergerequest's own "approved/rejected exactly once" invariant (there,
// ErrNotOpen). It shares Claim's lock, so the claim-then-decide pair a
// deploy performs can't interleave with another caller's.
func (s *Store) Decide(repo, id string, status Status, releaseDir, failureReason string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, err := s.get(repo, id)
	if err != nil {
		return Request{}, err
	}
	if req.Status != StatusPending && req.Status != StatusInProgress {
		return Request{}, ErrNotPending
	}

	req.Status = status
	req.ReleaseDir = releaseDir
	req.FailureReason = failureReason
	decidedAt := time.Now().UTC()
	req.DecidedAt = &decidedAt

	if err := s.write(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// write overwrites req's on-disk file. Callers must already hold s.mu.
func (s *Store) write(req Request) error {
	path, err := s.path(req.Repo, req.ID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(req)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (s *Store) path(repo, id string) (string, error) {
	if !validRepoName.MatchString(repo) {
		return "", ErrInvalidRepo
	}
	if !idPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.rootDir, repo, id+".json"), nil
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
