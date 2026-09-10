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
	"strings"
	"time"
)

var (
	ErrInvalidRepo     = errors.New("taskboard: invalid repository name")
	ErrInvalidID       = errors.New("taskboard: invalid task id")
	ErrNotFound        = errors.New("taskboard: not found")
	ErrInvalidStatus   = errors.New("taskboard: invalid status")
	ErrEmptyTitle      = errors.New("taskboard: title must not be empty")
	ErrInvalidPriority = errors.New("taskboard: invalid priority")
	ErrInvalidDueDate  = errors.New("taskboard: due date must be YYYY-MM-DD")
)

// validRepoName mirrors repostore's and mergerequest's own name
// validation. Duplicated rather than imported so this package's on-disk
// path-building stays safe against path traversal on its own.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// idPattern matches only IDs this package itself generates (see newID),
// so an ID coming from a URL path parameter can be validated before it is
// ever joined into a filesystem path.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Status is a task's place in the board, in board order.
//
// StatusTodo was added after the original three (2026-09-07): a task
// starting life as StatusInProgress claimed work had begun the moment
// somebody wrote it down, which made "yapılıyor" mean nothing — every
// task ever created sat there. Written-down and started are different
// facts, and the board is only useful when it can tell them apart.
//
// Existing tasks keep whatever status they were saved with; nothing needs
// migrating, because the set of valid values only grew.
type Status string

const (
	StatusTodo         Status = "todo"
	StatusInProgress   Status = "in_progress"
	StatusAwaitingTest Status = "awaiting_test"
	StatusDone         Status = "done"
)

func (s Status) valid() bool {
	return s == StatusTodo || s == StatusInProgress || s == StatusAwaitingTest || s == StatusDone
}

// Task is a unit of work tracked on one repository's board.
type Task struct {
	ID string `json:"id"`
	// Key is the human-readable name — "DEN-14" — that a task can be
	// referred to by in conversation, in a commit message, or in a URL.
	// Assigned once at creation and never reused, even after a delete.
	// Empty on tasks written before keys existed (see Store.Get).
	Key         string `json:"key,omitempty"`
	Repo        string `json:"repo"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AssignedTo  string `json:"assignedTo"`
	Author      string `json:"author"`
	Status      Status `json:"status"`
	// Urgent is the original boolean, kept only so tasks written before
	// Priority existed still decode and can be upgraded (see
	// upgradePriority). Nothing sets it any more; read Priority instead.
	Urgent   bool     `json:"urgent,omitempty"`
	Priority Priority `json:"priority,omitempty"`
	// DueDate is a calendar day as "YYYY-MM-DD", not a timestamp. A due
	// date is a day in the reader's calendar, and storing an instant would
	// make "due Friday" land on Thursday for anybody in a different zone.
	// Empty means no date set.
	DueDate   string    `json:"dueDate,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Overdue reports whether the task has a due date that has already passed
// and is not finished. Done work is never overdue — chasing something
// that is already delivered is noise.
func (t Task) Overdue(today string) bool {
	return t.DueDate != "" && t.Status != StatusDone && t.DueDate < today
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

// Create persists a new task for repo, always starting StatusTodo, and
// returns it with its generated ID and CreatedAt populated. Moving it on
// from there is a deliberate act — see Status.
func (s *Store) Create(repo, title, description, assignedTo, author string) (Task, error) {
	if !validRepoName.MatchString(repo) {
		return Task{}, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Task{}, err
	}

	// Allocated before the task is written; see nextKey for why a burned
	// number is preferable to a duplicated one.
	key, err := s.nextKey(repo)
	if err != nil {
		return Task{}, err
	}

	task := Task{
		Key:         key,
		Repo:        repo,
		Title:       title,
		Description: description,
		AssignedTo:  assignedTo,
		Author:      author,
		Status:      StatusTodo,
		Priority:    PriorityNormal,
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
// validDueDate accepts exactly "YYYY-MM-DD" and only real calendar days,
// so "2026-02-31" is rejected rather than silently normalised.
func validDueDate(v string) bool {
	parsed, err := time.Parse("2006-01-02", v)
	return err == nil && parsed.Format("2006-01-02") == v
}

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
	// Upgraded on read, not by rewriting the file: a task saved before
	// Priority existed reads back with one, and nothing on disk has to be
	// migrated. The same pattern the git-token and git-email stores use.
	return upgradePriority(task), nil
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
		tasks = append(tasks, upgradePriority(task))
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
// Changes is a partial update: every field is a pointer, and a nil one
// leaves that part of the task alone. A struct rather than a parameter per
// field — five positional pointers is a call nobody can read, and adding a
// sixth would mean touching every caller again.
type Changes struct {
	Title       *string
	Description *string
	Status      *Status
	Priority    *Priority
	// DueDate accepts "YYYY-MM-DD" to set one and "" to clear it — the
	// pointer distinguishes "leave alone" (nil) from "remove" (empty).
	DueDate    *string
	AssignedTo *string
}

func (s *Store) Update(repo, id string, c Changes) (Task, error) {
	if c.Status != nil && !c.Status.valid() {
		return Task{}, ErrInvalidStatus
	}
	// A task with no title is unusable: it is the only thing rendered on a
	// board card, so an empty one becomes a card nobody can identify or
	// search for. Rejected here rather than in the handler so no caller can
	// write one by going around the API.
	if c.Title != nil && strings.TrimSpace(*c.Title) == "" {
		return Task{}, ErrEmptyTitle
	}
	if c.Priority != nil && !c.Priority.valid() {
		return Task{}, ErrInvalidPriority
	}
	if c.DueDate != nil && *c.DueDate != "" && !validDueDate(*c.DueDate) {
		return Task{}, ErrInvalidDueDate
	}

	task, err := s.Get(repo, id)
	if err != nil {
		return Task{}, err
	}
	if c.Title != nil {
		task.Title = strings.TrimSpace(*c.Title)
	}
	if c.Description != nil {
		task.Description = strings.TrimSpace(*c.Description)
	}
	if c.Status != nil {
		task.Status = *c.Status
	}
	if c.Priority != nil {
		task.Priority = *c.Priority
		// The old flag is no longer the source of truth; clearing it stops
		// upgradePriority from ever second-guessing an explicit choice.
		task.Urgent = false
	}
	if c.DueDate != nil {
		task.DueDate = *c.DueDate
	}
	if c.AssignedTo != nil {
		task.AssignedTo = *c.AssignedTo
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

// Delete removes a task for good. Deliberately a real delete rather than
// an archive flag: this board tracks in-flight work for a handful of
// people, and a hidden pile of soft-deleted rows is a maintenance cost
// nobody here would ever collect on. The audit log keeps the record that
// it happened, which is the part that has to survive.
//
// Deleting a task that isn't there returns ErrNotFound rather than
// succeeding quietly, so a caller working from a stale board learns their
// view is out of date instead of seeing a phantom success.
func (s *Store) Delete(repo, id string) error {
	path, err := s.path(repo, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
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
