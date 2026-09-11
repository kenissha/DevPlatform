package gitserver

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// AuthorRecorder is told the author address carried by each commit
// received during an authenticated push, so the platform can later offer
// it to that person for confirmation (see internal/gitemails).
//
// This is the only moment the link can be observed at all: a commit
// records an unverified signature and nothing else, while the push
// around it is authenticated. Neither half identifies a person on its
// own.
type AuthorRecorder interface {
	RecordSeen(subject, email string) error
}

// CommitLinker is told about each commit received during an
// authenticated push, so a commit that names a task in its message can be
// shown on that task's page (see internal/taskboard's CommitLink).
//
// This is the cheap half of a feature Jira sells as an installed
// integration: the git server is already decoding every incoming commit
// to read its author line, so reading the message costs one more pass
// over bytes that are already in hand.
//
// repo is the repository name as repostore names it (no ".git"), so the
// implementation can resolve that repository's own key prefix.
type CommitLinker interface {
	LinkCommitMessage(repo, hash, message, author string, at time.Time) error
}

// maxInspectedCommitSize caps how much of a commit object is buffered to
// find its author line. A commit is a few hundred bytes of headers plus a
// message; anything vastly larger is not worth holding in memory per
// object, and the header we want is at the top regardless.
const maxInspectedCommitSize = 1 << 20 // 1 MiB

// commitLoader wraps a transport.Loader so every commit object arriving
// through it is read once and reported to whichever observers are wired:
// the author address for subject, and any task keys the message names.
//
// One decorator with two observers rather than two decorators: reading a
// commit means buffering the whole object, and buffering the same bytes
// twice to answer two questions about them is waste with no upside. The
// observers stay independent — either may be nil.
//
// Observing is strictly a side effect: nothing here may reject or alter a
// push. Errors are logged and dropped rather than returned, because a
// failure to note an address or link a task must never cost someone their
// push.
type commitLoader struct {
	inner    transport.Loader
	recorder AuthorRecorder
	linker   CommitLinker
	subject  string
}

// newCommitLoader returns inner unchanged when there is nothing to
// observe for — no observers wired, or an unauthenticated request with no
// subject to attribute commits to. That keeps the no-op case free of a
// decorator that would buffer every object for nothing.
func newCommitLoader(inner transport.Loader, recorder AuthorRecorder, linker CommitLinker, subject string) transport.Loader {
	if subject == "" || (recorder == nil && linker == nil) {
		return inner
	}
	return &commitLoader{inner: inner, recorder: recorder, linker: linker, subject: subject}
}

func (l *commitLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &commitStorer{
		Storer:   st,
		recorder: l.recorder,
		linker:   l.linker,
		subject:  l.subject,
		repo:     repoNameFromURL(u),
	}, nil
}

// repoNameFromURL turns the path go-git resolved a push against back into
// the name repostore uses. The transport works in ".git" directories;
// every other package names the repository without it.
func repoNameFromURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	name := path.Base(strings.Trim(u.Path, "/"))
	if name == "." || name == "/" {
		return ""
	}
	return strings.TrimSuffix(name, ".git")
}

// commitStorer embeds a real storage.Storer so every method is delegated
// via interface embedding, except RawObjectWriter.
//
// RawObjectWriter is the method that matters, for the same reason
// scanningStorer overrides it: go-git v6's packfile parser calls it per
// object while unpacking an incoming push. It must embed the
// storage.Storer *interface* rather than a concrete storer, so the
// optional PackfileWriter fast path isn't promoted — that path writes an
// incoming packfile straight to disk without decoding objects, and would
// skip this inspection entirely. See scanningStorer's doc comment, which
// documents the same constraint in more detail.
type commitStorer struct {
	storage.Storer
	recorder AuthorRecorder
	linker   CommitLinker
	subject  string
	repo     string
}

func (s *commitStorer) RawObjectWriter(typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	real, err := s.Storer.RawObjectWriter(typ, sz)
	if err != nil {
		return nil, err
	}
	if typ != plumbing.CommitObject || sz > maxInspectedCommitSize {
		return real, nil
	}
	return &commitWriteCloser{
		real:    real,
		size:    sz,
		observe: s.observe,
	}, nil
}

// observe runs once per commit object, with whatever payload arrived.
// complete says whether that payload reached the size the packfile
// declared. Each observer's failure is logged and swallowed on its own,
// so one broken store cannot take the other down with it — nor the push.
func (s *commitStorer) observe(raw []byte, complete bool) {
	if s.recorder != nil {
		if email, ok := authorEmail(raw); ok {
			if err := s.recorder.RecordSeen(s.subject, email); err != nil {
				log.Printf("gitserver: failed to record commit author %q for %q: %v", email, s.subject, err)
			}
		}
	}

	// Linking needs a complete object and the author recorder does not:
	// the author line sits at the top of a commit, but the hash is
	// computed over the whole payload, so an aborted push would produce a
	// name no commit will ever have — a task pointing at a commit that
	// does not exist. The author half still runs, because an address read
	// from a partial object is the same address.
	if s.linker == nil || s.repo == "" || !complete {
		return
	}
	message := commitMessage(raw)
	if message == "" {
		return
	}
	name, at := authorNameAndTime(raw)
	// The hash is computed here rather than read off go-git:
	// RawObjectWriter is handed a type and a size, not an object name, and
	// a git object's name is exactly the SHA-1 of its type/size header
	// followed by this payload.
	if err := s.linker.LinkCommitMessage(s.repo, objectHash(raw), message, name, at); err != nil {
		log.Printf("gitserver: failed to link commit in %q: %v", s.repo, err)
	}
}

// objectHash reproduces git's object naming: sha1 over "commit <len>",
// a NUL byte, then the payload.
//
// SHA-1 rather than SHA-256 because that is the object format the
// repositories this server hosts use. A repository created with a
// different format would produce names matching nothing, which shows up
// as a missing link rather than a wrong one.
func objectHash(raw []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "commit %d\x00", len(raw))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// commitMessage returns everything after a commit object's header block.
//
// A commit's payload is "key value" lines, a blank line, then the message
// — which is free text and may itself contain blank lines, so only the
// first separator counts.
func commitMessage(raw []byte) string {
	i := bytes.Index(raw, []byte("\n\n"))
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(string(raw[i+2:]))
}

// authorNameAndTime pulls the display name and timestamp out of the
// author header: "author Name <email> unixtime tz".
//
// A missing or unparsable time yields the zero Time rather than "now":
// the reader is better served by an obviously absent date than by one
// that silently claims the commit was written at the moment it was
// pushed.
func authorNameAndTime(raw []byte) (string, time.Time) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			break
		}
		rest, found := bytes.CutPrefix(line, []byte("author "))
		if !found {
			continue
		}
		open := bytes.IndexByte(rest, '<')
		if open < 0 {
			return "", time.Time{}
		}
		name := strings.TrimSpace(string(rest[:open]))

		closeIdx := bytes.IndexByte(rest[open+1:], '>')
		if closeIdx < 0 {
			return name, time.Time{}
		}
		fields := strings.Fields(string(rest[open+1+closeIdx+1:]))
		if len(fields) == 0 {
			return name, time.Time{}
		}
		secs, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return name, time.Time{}
		}
		return name, time.Unix(secs, 0).UTC()
	}
	return "", time.Time{}
}

// commitWriteCloser forwards every byte to the real writer untouched and
// keeps a copy, handing it to observe once the object is complete.
//
// Forwarding happens first, and the recorded result is never allowed to
// produce an error: unlike the secret scanner — which deliberately holds
// content back so flagged bytes never reach disk — this decorator only
// observes.
type commitWriteCloser struct {
	real      io.WriteCloser
	buf       bytes.Buffer
	size      int64
	inspected bool
	observe   func(raw []byte, complete bool)
}

func (w *commitWriteCloser) Write(p []byte) (int, error) {
	n, err := w.real.Write(p)
	if n > 0 {
		w.buf.Write(p[:n])
	}
	// sz gives the object's exact final size up front, so the object is
	// known to be complete as soon as the buffer reaches it.
	if !w.inspected && int64(w.buf.Len()) >= w.size {
		w.inspected = true
		w.observe(w.buf.Bytes(), true)
	}
	return n, err
}

func (w *commitWriteCloser) Close() error {
	if !w.inspected {
		w.inspected = true
		// Reaching Close without Write having already fired means the
		// object never reached its declared size.
		w.observe(w.buf.Bytes(), false)
	}
	return w.real.Close()
}

// authorEmail pulls the address out of a commit object's "author" header.
//
// A commit's payload is a short block of "key value" lines terminated by
// a blank line, of which "author Name <email> unixtime tz" is one. The
// committer line is deliberately ignored: the graph credits whoever
// wrote a change, which is how gitstats counts commits too — they differ
// whenever someone applies a patch or rebases another person's work.
func authorEmail(raw []byte) (string, bool) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			// End of the header block; the message body follows and can
			// contain anything, so stop rather than keep matching.
			break
		}
		rest, found := bytes.CutPrefix(line, []byte("author "))
		if !found {
			continue
		}
		open := bytes.IndexByte(rest, '<')
		if open < 0 {
			return "", false
		}
		close := bytes.IndexByte(rest[open+1:], '>')
		if close < 0 {
			return "", false
		}
		email := string(rest[open+1 : open+1+close])
		if email == "" {
			return "", false
		}
		return email, true
	}
	return "", false
}
