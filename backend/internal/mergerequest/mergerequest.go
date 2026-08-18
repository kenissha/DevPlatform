// Package mergerequest implements the "Geliştirici main'e birleştir talebi
// açar, Yönetici diff'i görüp onaylar/reddeder" flow from the design doc.
//
// A merge request records intent (merge SourceBranch into TargetBranch in a
// given repository) and a review status; it does not, on its own, perform
// the git merge. Approval only records that a Yönetici reviewed and signed
// off — actually moving TargetBranch is a separate, deliberately deferred
// step (see the package's design notes in the project's plan docs) because
// it has to interact carefully with gitserver's branch protection instead
// of just bypassing it.
package mergerequest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidRepo   = errors.New("mergerequest: invalid repository name")
	ErrInvalidID     = errors.New("mergerequest: invalid merge request id")
	ErrNotFound      = errors.New("mergerequest: not found")
	ErrInvalidStatus = errors.New("mergerequest: invalid status transition")
)

// validRepoName mirrors repostore's own name validation. Duplicated rather
// than imported so this package's on-disk path-building stays safe against
// path traversal even if repostore's validation is only ever checked by a
// caller further up the stack, not here.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// idPattern matches only IDs this package itself generates (see newID),
// so an ID coming from a URL path parameter can be validated before it is
// ever joined into a filesystem path.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Status is a merge request's review state.
type Status string

const (
	StatusOpen     Status = "open"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// MergeRequest records a request to merge SourceBranch into TargetBranch in
// Repo, and its review status.
type MergeRequest struct {
	ID           string    `json:"id"`
	Repo         string    `json:"repo"`
	Title        string    `json:"title"`
	SourceBranch string    `json:"sourceBranch"`
	TargetBranch string    `json:"targetBranch"`
	Author       string    `json:"author"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	// MergedCommit is the commit TargetBranch was fast-forwarded to when
	// this request was approved (see Handlers.Approve / FastForwardMerge).
	// Empty until then.
	MergedCommit string `json:"mergedCommit,omitempty"`
}

// Store persists merge requests as one JSON file per request under
// rootDir, grouped in a per-repo subdirectory — the same "flat files on
// disk, no database" approach repostore already uses for repositories
// themselves.
type Store struct {
	rootDir string

	mu            sync.Mutex
	lastCreatedAt time.Time
}

// NewStore returns a Store rooted at rootDir. rootDir does not need to
// exist yet.
func NewStore(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// nextCreatedAt returns the current time (UTC), guaranteed strictly after
// every value this Store has returned before — even when two Create calls
// land in the same clock tick, which happens occasionally on this
// project's dev/CI machines. Without this, two merge requests created back
// to back could get an identical CreatedAt, leaving List's
// CreatedAt.After-based sort to fall back on sort.Slice's unstable
// ordering for the tied pair — the exact cause of a flaky
// TestList_ReturnsAllRequestsNewestFirst (see docs/DURUM.md, "Sıradaki
// iş"). Bumping by a nanosecond keeps creation order exact without
// touching the sort itself.
func (s *Store) nextCreatedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if !now.After(s.lastCreatedAt) {
		now = s.lastCreatedAt.Add(time.Nanosecond)
	}
	s.lastCreatedAt = now
	return now
}

// Create persists a new merge request for repo and returns it with its
// generated ID, Status (always StatusOpen), and CreatedAt populated.
func (s *Store) Create(repo, title, sourceBranch, targetBranch, author string) (MergeRequest, error) {
	if !validRepoName.MatchString(repo) {
		return MergeRequest{}, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return MergeRequest{}, err
	}

	mr := MergeRequest{
		Repo:         repo,
		Title:        title,
		SourceBranch: sourceBranch,
		TargetBranch: targetBranch,
		Author:       author,
		Status:       StatusOpen,
		CreatedAt:    s.nextCreatedAt(),
	}

	// Retry on the astronomically unlikely chance a random ID collides with
	// an existing file; O_EXCL makes the check-then-create atomic so two
	// concurrent Create calls can never overwrite each other's request.
	for attempt := 0; attempt < 5; attempt++ {
		id, err := newID()
		if err != nil {
			return MergeRequest{}, err
		}
		mr.ID = id

		path := filepath.Join(dir, id+".json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return MergeRequest{}, err
		}
		err = json.NewEncoder(f).Encode(mr)
		closeErr := f.Close()
		if err != nil {
			return MergeRequest{}, err
		}
		if closeErr != nil {
			return MergeRequest{}, closeErr
		}
		return mr, nil
	}
	return MergeRequest{}, fmt.Errorf("mergerequest: failed to allocate a unique id after 5 attempts")
}

// Get returns the merge request identified by (repo, id).
func (s *Store) Get(repo, id string) (MergeRequest, error) {
	path, err := s.path(repo, id)
	if err != nil {
		return MergeRequest{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MergeRequest{}, ErrNotFound
		}
		return MergeRequest{}, err
	}

	var mr MergeRequest
	if err := json.Unmarshal(data, &mr); err != nil {
		return MergeRequest{}, err
	}
	return mr, nil
}

// List returns every merge request for repo, newest first.
func (s *Store) List(repo string) ([]MergeRequest, error) {
	if !validRepoName.MatchString(repo) {
		return nil, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MergeRequest{}, nil
		}
		return nil, err
	}

	mrs := []MergeRequest{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var mr MergeRequest
		if err := json.Unmarshal(data, &mr); err != nil {
			return nil, err
		}
		mrs = append(mrs, mr)
	}

	sort.Slice(mrs, func(i, j int) bool {
		return mrs[i].CreatedAt.After(mrs[j].CreatedAt)
	})
	return mrs, nil
}

// ErrNotOpen is returned by SetStatus and MarkApproved when the merge
// request has already left StatusOpen — a request can be approved or
// rejected exactly once, not re-decided.
var ErrNotOpen = errors.New("mergerequest: merge request is not open")

// SetStatus transitions the merge request identified by (repo, id) from
// StatusOpen to status. Only StatusRejected is a valid target here —
// approval goes through MarkApproved instead, since it also has to record
// the resulting merged commit in the same write.
func (s *Store) SetStatus(repo, id string, status Status) (MergeRequest, error) {
	if status != StatusRejected {
		return MergeRequest{}, ErrInvalidStatus
	}

	mr, err := s.Get(repo, id)
	if err != nil {
		return MergeRequest{}, err
	}
	if mr.Status != StatusOpen {
		return MergeRequest{}, ErrNotOpen
	}
	mr.Status = status

	if err := s.overwrite(repo, id, mr); err != nil {
		return MergeRequest{}, err
	}
	return mr, nil
}

// MarkApproved transitions the merge request identified by (repo, id) from
// StatusOpen to StatusApproved and records mergedCommit — the commit its
// target branch was fast-forwarded to (see FastForwardMerge). Both fields
// are written together so a request can never end up "approved" on disk
// without also recording what it was actually merged to.
func (s *Store) MarkApproved(repo, id, mergedCommit string) (MergeRequest, error) {
	mr, err := s.Get(repo, id)
	if err != nil {
		return MergeRequest{}, err
	}
	if mr.Status != StatusOpen {
		return MergeRequest{}, ErrNotOpen
	}
	mr.Status = StatusApproved
	mr.MergedCommit = mergedCommit

	if err := s.overwrite(repo, id, mr); err != nil {
		return MergeRequest{}, err
	}
	return mr, nil
}

func (s *Store) overwrite(repo, id string, mr MergeRequest) error {
	path, err := s.path(repo, id)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(mr)
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
