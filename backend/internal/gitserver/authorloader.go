package gitserver

import (
	"bytes"
	"io"
	"log"
	"net/url"

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

// maxInspectedCommitSize caps how much of a commit object is buffered to
// find its author line. A commit is a few hundred bytes of headers plus a
// message; anything vastly larger is not worth holding in memory per
// object, and the header we want is at the top regardless.
const maxInspectedCommitSize = 1 << 20 // 1 MiB

// authorLoader wraps a transport.Loader so every commit object arriving
// through it has its author address reported for subject.
//
// Recording is strictly a side effect: nothing here may reject or alter
// a push. Errors are logged and dropped rather than returned, because a
// failure to note an address must never cost someone their push.
type authorLoader struct {
	inner    transport.Loader
	recorder AuthorRecorder
	subject  string
}

// newAuthorLoader returns inner unchanged when there is nothing to
// record against — no recorder wired, or an unauthenticated request with
// no subject to attribute commits to. That keeps the no-op case free of
// a decorator that would buffer every object for nothing.
func newAuthorLoader(inner transport.Loader, recorder AuthorRecorder, subject string) transport.Loader {
	if recorder == nil || subject == "" {
		return inner
	}
	return &authorLoader{inner: inner, recorder: recorder, subject: subject}
}

func (l *authorLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &authorStorer{Storer: st, recorder: l.recorder, subject: l.subject}, nil
}

// authorStorer embeds a real storage.Storer so every method is delegated
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
type authorStorer struct {
	storage.Storer
	recorder AuthorRecorder
	subject  string
}

func (s *authorStorer) RawObjectWriter(typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	real, err := s.Storer.RawObjectWriter(typ, sz)
	if err != nil {
		return nil, err
	}
	if typ != plumbing.CommitObject || sz > maxInspectedCommitSize {
		return real, nil
	}
	return &authorWriteCloser{
		real: real,
		size: sz,
		record: func(email string) {
			if err := s.recorder.RecordSeen(s.subject, email); err != nil {
				log.Printf("gitserver: failed to record commit author %q for %q: %v", email, s.subject, err)
			}
		},
	}, nil
}

// authorWriteCloser forwards every byte to the real writer untouched and
// keeps a copy, reading the author line once the object is complete.
//
// Forwarding happens first, and the recorded result is never allowed to
// produce an error: unlike the secret scanner — which deliberately holds
// content back so flagged bytes never reach disk — this decorator only
// observes.
type authorWriteCloser struct {
	real      io.WriteCloser
	buf       bytes.Buffer
	size      int64
	inspected bool
	record    func(email string)
}

func (w *authorWriteCloser) Write(p []byte) (int, error) {
	n, err := w.real.Write(p)
	if n > 0 {
		w.buf.Write(p[:n])
	}
	// sz gives the object's exact final size up front, so the object is
	// known to be complete as soon as the buffer reaches it.
	if !w.inspected && int64(w.buf.Len()) >= w.size {
		w.inspected = true
		w.inspect()
	}
	return n, err
}

func (w *authorWriteCloser) Close() error {
	if !w.inspected {
		w.inspected = true
		w.inspect()
	}
	return w.real.Close()
}

func (w *authorWriteCloser) inspect() {
	if email, ok := authorEmail(w.buf.Bytes()); ok {
		w.record(email)
	}
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
