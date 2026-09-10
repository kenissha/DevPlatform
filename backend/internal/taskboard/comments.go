package taskboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/atomicfile"
)

// Comments are where the back-and-forth about a piece of work happens —
// the thing that otherwise ends up in a chat window where it is lost the
// next day and invisible to whoever picks the task up later.
//
// All of a task's comments live in one file rather than one file each.
// They are always read together (a comment on its own means nothing), the
// count per task is small, and one file means a comment can never be
// half-written across two places.

var (
	ErrEmptyComment   = errors.New("taskboard: comment must not be empty")
	ErrCommentTooLong = errors.New("taskboard: comment is too long")
	ErrCommentNotOurs = errors.New("taskboard: not the comment's author")
)

// MaxCommentLength caps a single comment. Generous enough for a real
// explanation, small enough that one paste cannot make a task's file
// unreadable. Counted in runes, not bytes: Turkish is multi-byte in
// UTF-8, and a byte limit would silently allow half as much text.
const MaxCommentLength = 4000

type Comment struct {
	ID       string     `json:"id"`
	Author   string     `json:"author"`
	Body     string     `json:"body"`
	At       time.Time  `json:"at"`
	EditedAt *time.Time `json:"editedAt,omitempty"`
}

// Comments returns a task's comments, oldest first — a conversation reads
// top to bottom, unlike a log.
func (s *Store) Comments(repo, id string) ([]Comment, error) {
	if _, err := s.path(repo, id); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadComments(repo, id)
}

// AddComment appends one comment and returns it.
func (s *Store) AddComment(repo, id, author, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, ErrEmptyComment
	}
	if len([]rune(body)) > MaxCommentLength {
		return Comment{}, ErrCommentTooLong
	}
	if _, err := s.path(repo, id); err != nil {
		return Comment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// The task has to exist: a comment on a deleted task would be a file
	// nothing ever reads and nobody can find.
	if _, err := s.Get(repo, id); err != nil {
		return Comment{}, err
	}

	list, err := s.loadComments(repo, id)
	if err != nil {
		return Comment{}, err
	}

	commentID, err := newID()
	if err != nil {
		return Comment{}, err
	}
	comment := Comment{ID: commentID, Author: author, Body: body, At: time.Now().UTC()}
	list = append(list, comment)

	if err := s.saveComments(repo, id, list); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

// EditComment replaces a comment's body. Only its author may edit it, and
// the edit is stamped: a conversation people can silently rewrite is not
// a record of what was said.
func (s *Store) EditComment(repo, id, commentID, author, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, ErrEmptyComment
	}
	if len([]rune(body)) > MaxCommentLength {
		return Comment{}, ErrCommentTooLong
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadComments(repo, id)
	if err != nil {
		return Comment{}, err
	}
	for i, c := range list {
		if c.ID != commentID {
			continue
		}
		if c.Author != author {
			return Comment{}, ErrCommentNotOurs
		}
		now := time.Now().UTC()
		list[i].Body = body
		list[i].EditedAt = &now
		if err := s.saveComments(repo, id, list); err != nil {
			return Comment{}, err
		}
		return list[i], nil
	}
	return Comment{}, ErrNotFound
}

// DeleteComment removes one comment. allowAny is what the handler passes
// for an admin; everyone else may only remove their own.
func (s *Store) DeleteComment(repo, id, commentID, author string, allowAny bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadComments(repo, id)
	if err != nil {
		return err
	}
	for i, c := range list {
		if c.ID != commentID {
			continue
		}
		if !allowAny && c.Author != author {
			return ErrCommentNotOurs
		}
		return s.saveComments(repo, id, append(list[:i:i], list[i+1:]...))
	}
	return ErrNotFound
}

// deleteComments removes a task's whole conversation, called when the
// task itself goes — otherwise the file lingers with no way to reach it.
// Callers must hold s.mu.
func (s *Store) deleteComments(repo, id string) error {
	path, err := s.commentsPath(repo, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// commentsPath puts a task's conversation in a "comments" subdirectory,
// deliberately not beside the task file.
//
// List enumerates every *.json in the repo's directory and decodes each
// one as a Task. A sibling "<id>.comments.json" would be picked up by
// that scan, fail to decode, and take the whole board down with it. A
// subdirectory is skipped by List's existing IsDir check, so the two
// cannot collide however either one changes later.
//
// The repo/id validation is borrowed from path, so a traversal attempt is
// rejected here exactly as it is for the task itself.
func (s *Store) commentsPath(repo, id string) (string, error) {
	if _, err := s.path(repo, id); err != nil {
		return "", err
	}
	return filepath.Join(s.rootDir, repo, "comments", id+".json"), nil
}

func (s *Store) loadComments(repo, id string) ([]Comment, error) {
	path, err := s.commentsPath(repo, id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No conversation yet is not an error.
			return []Comment{}, nil
		}
		return nil, err
	}
	list := []Comment{}
	if len(data) == 0 {
		return list, nil
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) saveComments(repo, id string, list []Comment) error {
	path, err := s.commentsPath(repo, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".comments-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// See internal/atomicfile for why this is not os.Rename.
	return atomicfile.Rename(tmpName, path)
}
