// Package audit implements the design doc's "Audit log" requirement:
// every meaningful action (task created, request approved/rejected,
// deploy, rollback) recorded "kalıcı ve değiştirilemez şekilde".
//
// Storage is a single append-only JSON Lines file. Append-only is the
// immutability mechanism, not a detail: the writer only ever opens with
// O_APPEND and never seeks, truncates, or rewrites, so an existing line
// cannot be altered through this package at all. (Someone with filesystem
// access can still edit the file — real tamper-evidence would need
// hash-chaining or an off-box sink, which is out of scope for a
// two-person internal platform and is called out here rather than
// silently implied.)
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Action is the kind of event recorded. Values are stable strings: they
// end up in a file that outlives any given build, so they are never
// renumbered or renamed.
type Action string

const (
	ActionRepoCreated Action = "repo.created"
	ActionTaskCreated Action = "task.created"
	ActionTaskUpdated Action = "task.updated"
	ActionMROpened    Action = "merge_request.opened"
	ActionMRApproved  Action = "merge_request.approved"
	ActionMRRejected  Action = "merge_request.rejected"
)

// Event is one recorded action.
type Event struct {
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  Action    `json:"action"`
	Repo    string    `json:"repo,omitempty"`
	Target  string    `json:"target,omitempty"`
	Summary string    `json:"summary,omitempty"`
}

// Logger appends events to a file. The zero value is not usable; use New.
// A nil *Logger is usable and does nothing — handlers hold one as an
// optional dependency, so tests and callers that don't care about audit
// need no wiring.
type Logger struct {
	mu   sync.Mutex
	path string
}

// New returns a Logger writing to path. Parent directories are created on
// first write, not here, so constructing a Logger never fails.
func New(path string) *Logger {
	return &Logger{path: path}
}

// Log appends one event stamped with the current time. Failures are
// reported to the caller but are never fatal to the action being audited:
// callers deliberately ignore the error rather than fail a merge because
// a log line couldn't be written. Returning it keeps that a decision at
// the call site instead of a silent swallow here.
func (l *Logger) Log(actor string, action Action, repo, target, summary string) error {
	if l == nil {
		return nil
	}

	event := Event{
		At:      time.Now().UTC(),
		Actor:   actor,
		Action:  action,
		Repo:    repo,
		Target:  target,
		Summary: summary,
	}

	line, err := json.Marshal(event)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o750); err != nil {
		return err
	}
	// O_APPEND only: never O_TRUNC, never a seek. Concurrent appends of a
	// single short line are atomic on both POSIX and Windows, so the
	// mutex is belt-and-braces for this process rather than the whole
	// guarantee.
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// List returns the most recent events, newest first, capped at limit.
func (l *Logger) List(limit int) ([]Event, error) {
	if l == nil {
		return []Event{}, nil
	}
	if limit < 1 {
		limit = 1
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing has happened yet — an empty log, not an error.
			return []Event{}, nil
		}
		return nil, err
	}
	defer f.Close()

	events := []Event{}
	scanner := bufio.NewScanner(f)
	// Events are short, but a long summary shouldn't truncate the scan
	// and silently drop the rest of the file.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			// A corrupt line must not hide every event after it.
			continue
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Newest first, capped.
	reverse(events)
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}
