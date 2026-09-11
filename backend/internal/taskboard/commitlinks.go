package taskboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/atomicfile"
)

var ErrInvalidCommit = errors.New("taskboard: invalid commit hash")

// MaxCommitLinks caps how many commits one task keeps. A task that has
// been touched by two hundred commits is not a task somebody is trying to
// read the history of; keeping the most recent hundred keeps the file
// small and the page readable.
const MaxCommitLinks = 100

// CommitLink is a commit that named this task in its message.
//
// The whole point of the feature is that it costs the person nothing:
// they write "DEN-14 tarih filtresi düzeltildi" the way they already
// would, push, and the task page shows the work. Jira sells this as an
// integration you install and configure; here the git server already
// reads every commit on its way in (see internal/gitserver), so it is
// very nearly free.
type CommitLink struct {
	Hash string `json:"hash"`
	// Subject is the commit message's first line only. A task page wants
	// the one-line summary, and storing whole message bodies would put an
	// unbounded amount of text in a file read on every page view.
	Subject string `json:"subject"`
	// Author is the name from the commit's author line, not a platform
	// subject: a commit records what git was configured with, and pretending
	// that resolves to an account would be a guess. Since CLI login now
	// aligns the two (see cmd/devplatform-login), it is usually the same
	// person — but "usually" is not something to encode.
	Author string    `json:"author"`
	At     time.Time `json:"at"`
}

// LinkCommit records that commit named the task with the given key, and
// reports whether anything was actually written.
//
// Idempotent by hash: a re-push, a mirror, or a second branch carrying
// the same commit must not make the task look like it was worked on
// twice. Returns false with no error when the key matches no task —
// people mistype keys and mention tasks that were since deleted, and
// neither is a reason to fail a push.
func (s *Store) LinkCommit(repo, key string, link CommitLink) (bool, error) {
	if !validRepoName.MatchString(repo) {
		return false, ErrInvalidRepo
	}
	if !validCommitHash(link.Hash) {
		return false, ErrInvalidCommit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.taskByKeyLocked(repo, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	existing, err := s.readCommits(repo, task.ID)
	if err != nil {
		return false, err
	}
	for _, e := range existing {
		if e.Hash == link.Hash {
			return false, nil
		}
	}

	link.Subject = firstLine(link.Subject)
	existing = append(existing, link)

	// Newest first: a task page's question is "what has been done on this
	// lately", and the answer is at the top.
	sort.SliceStable(existing, func(i, j int) bool { return existing[i].At.After(existing[j].At) })
	if len(existing) > MaxCommitLinks {
		existing = existing[:MaxCommitLinks]
	}

	return true, s.writeCommits(repo, task.ID, existing)
}

// Commits returns the commits linked to a task, newest first.
func (s *Store) Commits(repo, id string) ([]CommitLink, error) {
	if !validRepoName.MatchString(repo) {
		return nil, ErrInvalidRepo
	}
	if !idPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	if _, err := s.Get(repo, id); err != nil {
		return nil, err
	}
	return s.readCommits(repo, id)
}

// TaskByKey finds a task by its readable key ("DEN-14"), case-insensitively.
func (s *Store) TaskByKey(repo, key string) (Task, error) {
	if !validRepoName.MatchString(repo) {
		return Task{}, ErrInvalidRepo
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskByKeyLocked(repo, key)
}

// A linear scan rather than a key→id index. The index would be a second
// thing to keep in step with the tasks themselves, and it would buy
// nothing: a repository's board is tens of files, and this runs once per
// key mentioned in a pushed commit, not per request.
//
// Callers must hold s.mu.
func (s *Store) taskByKeyLocked(repo, key string) (Task, error) {
	tasks, err := s.List(repo)
	if err != nil {
		return Task{}, err
	}
	for _, t := range tasks {
		if strings.EqualFold(t.Key, key) {
			return t, nil
		}
	}
	return Task{}, ErrNotFound
}

// Stored in a subdirectory for the same reason comments are: List decodes
// every *.json in a repository's directory as a Task, so a sibling file
// would take the whole board down. See comments.go.
func (s *Store) commitsPath(repo, id string) (string, error) {
	if !validRepoName.MatchString(repo) {
		return "", ErrInvalidRepo
	}
	if !idPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.rootDir, repo, "commits", id+".json"), nil
}

func (s *Store) readCommits(repo, id string) ([]CommitLink, error) {
	path, err := s.commitsPath(repo, id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []CommitLink{}, nil
		}
		return nil, err
	}
	links := []CommitLink{}
	if len(data) == 0 {
		return links, nil
	}
	if err := json.Unmarshal(data, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (s *Store) writeCommits(repo, id string, links []CommitLink) error {
	path, err := s.commitsPath(repo, id)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(links)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".commits-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return atomicfile.Rename(tmpName, path)
}

// validCommitHash accepts a full 40-character SHA-1 object name. Short
// hashes are rejected: they come from a machine here, not a person, and
// an abbreviated one cannot be deduplicated reliably.
func validCommitHash(h string) bool {
	if len(h) != 40 {
		return false
	}
	for _, r := range h {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// LinkCommitMessage records a pushed commit against every task its
// message names. It implements gitserver.CommitLinker.
//
// Structurally, not by importing gitserver: the git server should be able
// to observe commits without the task board knowing it exists, and the
// task board should be able to accept commit links without depending on
// how they were found.
//
// A message naming no task, an unknown key, or a repository that has
// never had a task all return nil — none of them is a problem, and this
// runs inside somebody's push.
func (s *Store) LinkCommitMessage(repo, hash, message, author string, at time.Time) error {
	prefix, err := s.PrefixFor(repo)
	if err != nil || prefix == "" {
		return err
	}

	keys := KeyRefsIn(message, prefix)
	if len(keys) == 0 {
		return nil
	}

	link := CommitLink{Hash: hash, Subject: message, Author: author, At: at}
	for _, key := range keys {
		if _, err := s.LinkCommit(repo, key, link); err != nil {
			return err
		}
	}
	return nil
}
