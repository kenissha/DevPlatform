// Package taskboard implements the "Görev/talep panosu" from the design
// doc: task creation/assignment, status tracking (yapılıyor / test
// bekliyor / bitti), and urgent flagging, scoped per repository the same
// way merge requests are.
package taskboard

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
	"time"
)

var (
	ErrInvalidRepo   = errors.New("taskboard: invalid repository name")
	ErrInvalidID     = errors.New("taskboard: invalid task id")
	ErrNotFound      = errors.New("taskboard: not found")
	ErrInvalidStatus = errors.New("taskboard: invalid status")
)

// validRepoName mirrors repostore's and mergerequest's own name
// validation. Duplicated rather than imported so this package's on-disk
// path-building stays safe against path traversal on its own.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// idPattern matches only IDs this package itself generates (see newID),
// so an ID coming from a URL path parameter can be validated before it is
// ever joined into a filesystem path.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Status is a task's place in the board, exactly the three states named
// in the design doc — there is no "backlog"/"todo" state before this: a
// task starts life already StatusInProgress.
type Status string

const (
	StatusInProgress   Status = "in_progress"
	StatusAwaitingTest Status = "awaiting_test"
	StatusDone         Status = "done"
)

func (s Status) valid() bool {
	return s == StatusInProgress || s == StatusAwaitingTest || s == StatusDone
}

// Task is a unit of work tracked on one repository's board.
type Task struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AssignedTo  string    `json:"assignedTo"`
	Author      string    `json:"author"`
	Status      Status    `json:"status"`
	Urgent      bool      `json:"urgent"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Store persists tasks as one JSON file per task under rootDir, grouped in
// a per-repo subdirectory — the same flat-file approach repostore and
// mergerequest already use.
type Store struct {
	rootDir string
}

// NewStore returns a Store rooted at rootDir. rootDir does not need to
// exist yet.
func NewStore(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// Create persists a new task for repo, always starting StatusInProgress,
// and returns it with its generated ID and CreatedAt populated.
func (s *Store) Create(repo, title, description, assignedTo, author string) (Task, error) {
	if !validRepoName.MatchString(repo) {
		return Task{}, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Task{}, err
	}

	task := Task{
		Repo:        repo,
		Title:       title,
		Description: description,
		AssignedTo:  assignedTo,
		Author:      author,
		Status:      StatusInProgress,
		CreatedAt:   time.Now().UTC(),
	}

	// Retry on the astronomically unlikely chance a random ID collides
	// with an existing file; O_EXCL makes the check-then-create atomic.
	for attempt := 0; attempt < 5; attempt++ {
		id, err := newID()
		if err != nil {
			return Task{}, err
		}
		task.ID = id

		path := filepath.Join(dir, id+".json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return Task{}, err
		}
		err = json.NewEncoder(f).Encode(task)
		closeErr := f.Close()
		if err != nil {
			return Task{}, err
		}
		if closeErr != nil {
			return Task{}, closeErr
		}
		return task, nil
	}
	return Task{}, fmt.Errorf("taskboard: failed to allocate a unique id after 5 attempts")
}

// Get returns the task identified by (repo, id).
func (s *Store) Get(repo, id string) (Task, error) {
	path, err := s.path(repo, id)
	if err != nil {
		return Task{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}

	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// List returns every task for repo, newest first.
func (s *Store) List(repo string) ([]Task, error) {
	if !validRepoName.MatchString(repo) {
		return nil, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}

	tasks := []Task{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// Update applies a partial update to the task identified by (repo, id):
// each non-nil field is changed, everything else is left as-is. This
// board has no per-field authorization of its own (any authenticated user
// may update any task) — deliberately simpler than merge requests'
// Admin-gated approve/reject, matching the design doc's lighter-weight
// framing of the task board versus the review-gated merge flow.
func (s *Store) Update(repo, id string, status *Status, urgent *bool, assignedTo *string) (Task, error) {
	if status != nil && !status.valid() {
		return Task{}, ErrInvalidStatus
	}

	task, err := s.Get(repo, id)
	if err != nil {
		return Task{}, err
	}
	if status != nil {
		task.Status = *status
	}
	if urgent != nil {
		task.Urgent = *urgent
	}
	if assignedTo != nil {
		task.AssignedTo = *assignedTo
	}

	path, err := s.path(repo, id)
	if err != nil {
		return Task{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return Task{}, err
	}
	err = json.NewEncoder(f).Encode(task)
	closeErr := f.Close()
	if err != nil {
		return Task{}, err
	}
	if closeErr != nil {
		return Task{}, closeErr
	}
	return task, nil
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
