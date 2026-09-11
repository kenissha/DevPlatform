package repoimport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store runs imports and remembers how they went.
//
// Jobs live in memory and are lost on restart, deliberately. An import is
// something a person starts and watches finish over a minute or two; its
// record exists to drive that one screen. What survives a restart is the
// thing that matters — the repository itself, plus the audit entry saying
// who brought it in and what the history turned out to contain.
type Store struct {
	rootDir string
	cloner  Cloner

	mu   sync.Mutex
	jobs map[string]*Job
	// names guards against two imports racing for the same repository
	// name. The on-disk check cannot do it alone: both would look, both
	// would find nothing, and both would clone for two minutes before one
	// discovered the other.
	names map[string]bool
}

// NewStore returns a Store that writes repositories into rootDir — the
// same directory repostore.Store uses, because an imported repository is
// not a different kind of repository.
func NewStore(rootDir string, cloner Cloner) *Store {
	if cloner == nil {
		cloner = GitCloner{}
	}
	return &Store{
		rootDir: rootDir,
		cloner:  cloner,
		jobs:    map[string]*Job{},
		names:   map[string]bool{},
	}
}

// Start validates the request, claims the name, and begins the import in
// the background. It returns as soon as the job exists — the clone has not
// happened yet.
//
// token is never stored on the Job and never leaves this call: it is
// handed to the cloner and forgotten. An import is a one-off, so there is
// nothing a saved credential would buy that is worth being a credential
// the platform holds.
func (s *Store) Start(name, source, token string) (Job, error) {
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)

	if !validName.MatchString(name) {
		return Job{}, ErrInvalidName
	}
	if err := validateSource(source); err != nil {
		return Job{}, err
	}

	s.mu.Lock()
	if s.names[name] {
		s.mu.Unlock()
		return Job{}, ErrAlreadyExists
	}
	if _, err := os.Stat(s.repoPath(name)); err == nil {
		s.mu.Unlock()
		return Job{}, ErrAlreadyExists
	}

	id, err := newID()
	if err != nil {
		s.mu.Unlock()
		return Job{}, err
	}
	job := &Job{
		ID:   id,
		Name: name,
		// Stored clean: the panel shows this and the audit log keeps it,
		// and people paste URLs with tokens in them.
		Source:    sanitiseURL(source),
		Status:    StatusRunning,
		StartedAt: time.Now().UTC(),
	}
	s.jobs[id] = job
	s.names[name] = true
	s.mu.Unlock()

	go s.run(id, name, source, token)

	return *job, nil
}

// run does the work: clone into a staging directory, then move it into
// place. Never returns anything — the Job is how it reports.
func (s *Store) run(id, name, source, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	// Staged inside rootDir rather than the system temp directory so the
	// final step is a rename on the same volume. A cross-volume move would
	// be a copy, which is both slow for a repository this size and not
	// atomic — a crash mid-copy would leave a half-repository that
	// repostore.List happily reports as real.
	//
	// The staging name has no ".git" suffix, so List skips it while the
	// clone is in flight and the repository only appears once it is whole.
	staging := filepath.Join(s.rootDir, ".import-"+id)

	if err := os.MkdirAll(s.rootDir, 0o750); err != nil {
		s.fail(id, name, err.Error())
		return
	}
	defer os.RemoveAll(staging) // no-op once the rename below succeeds

	if err := s.cloner.Clone(ctx, source, token, staging); err != nil {
		s.fail(id, name, scrub(err.Error(), token, tokenIn(source)))
		return
	}

	commits, branches := countRefs(staging)
	if branches == 0 {
		s.fail(id, name, "kaynak depoda hiç dal yok — adres doğru mu?")
		return
	}

	// Scanned before the move, so a repository never becomes visible
	// without its findings already recorded.
	findings := ScanHistory(staging)

	if err := os.Rename(staging, s.repoPath(name)); err != nil {
		s.fail(id, name, fmt.Sprintf("depo yerine taşınamadı: %v", err))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.jobs[id]; job != nil {
		job.Status = StatusDone
		job.Finished = time.Now().UTC()
		job.Commits = commits
		job.Branches = branches
		job.Findings = findings
	}
}

// fail records the failure and releases the name so the person can correct
// the address and try again without restarting the server.
func (s *Store) fail(id, name, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.names, name)
	if job := s.jobs[id]; job != nil {
		job.Status = StatusFailed
		job.Finished = time.Now().UTC()
		job.Error = message
	}
}

// Get returns one job.
func (s *Store) Get(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return *job, nil
}

// List returns every job this process has run, newest first.
func (s *Store) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, *job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (s *Store) repoPath(name string) string {
	return filepath.Join(s.rootDir, name+".git")
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
