package gitserver

import (
	"bytes"
	"fmt"
	"io"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"

	"github.com/kenissha/DevPlatform/backend/internal/secretscan"
)

// maxScannedBlobSize caps how much of a single blob's content is buffered
// for secret scanning. Secrets are textual and small; blobs larger than
// this are passed through unscanned rather than buffered in memory —
// buffering an unbounded blob (e.g. a large binary asset) in RAM per push
// is a real resource risk with no corresponding security benefit, since
// secrets essentially never hide inside multi-megabyte binaries.
const maxScannedBlobSize = 1 << 20 // 1 MiB

// scanningLoader wraps a transport.Loader so every storer it returns
// scans new blob content for known secret patterns before accepting it.
type scanningLoader struct {
	inner transport.Loader
}

func newScanningLoader(inner transport.Loader) transport.Loader {
	return &scanningLoader{inner: inner}
}

func (l *scanningLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &scanningStorer{Storer: st}, nil
}

// scanningStorer embeds a real storage.Storer so every method is delegated
// automatically via Go interface embedding, except RawObjectWriter, which
// is overridden to scan new blob content before it's committed to storage.
//
// RawObjectWriter, not SetEncodedObject, is the method that matters here:
// go-git v6's packfile parser calls storage.RawObjectWriter per object
// while unpacking an incoming push (confirmed by reading the parser's
// source during planning — see the design/plan docs). A decorator that
// only overrode SetEncodedObject would compile and pass a naive test, but
// would never see real push content in production.
//
// This also depends on embedding storage.Storer as an *interface* field
// (matching protectedStorer's existing pattern in protectedloader.go), not
// the concrete filesystem storer type. The concrete filesystem storer also
// implements the optional storer.PackfileWriter fast path, which writes an
// incoming packfile's raw bytes straight to disk without decoding
// individual objects — bypassing per-object inspection entirely. Since
// that method isn't declared on the storage.Storer interface, embedding
// the interface (not the concrete type) means it isn't promoted here, so
// go-git falls back to the per-object path instead. This is load-bearing:
// if a future refactor embeds a concrete storer type here instead of the
// interface, scanning silently stops running.
type scanningStorer struct {
	storage.Storer
}

func (s *scanningStorer) RawObjectWriter(typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	real, err := s.Storer.RawObjectWriter(typ, sz)
	if err != nil {
		return nil, err
	}
	if typ != plumbing.BlobObject || sz > maxScannedBlobSize {
		return real, nil
	}
	return &scanningWriteCloser{real: real, size: sz}, nil
}

// scanningWriteCloser buffers a blob's content and scans it for a secret
// once the full object has arrived, before ever forwarding a byte to the
// real writer — flagged content never reaches disk, even partially.
//
// The scan-and-reject happens in Write, not Close, even though the content
// is only complete once all writes have landed. This is load-bearing, not
// a style choice: go-git v6.0.0-alpha.5's packfile scanner discards the
// error returned by this writer's Close (see scanner.go's and parser.go's
// `defer func() { _ = w.Close() }()`), so an error returned from Close can
// never fail the push — confirmed by instrumenting both call sites during
// implementation. The error returned by Write, on the other hand, does
// propagate (scanner.go checks `ioutil.CopyBufferPool(mw, zr)`'s error and
// returns it up through Parse). RawObjectWriter's sz parameter tells us the
// object's exact final size up front, so once the buffer reaches that size
// the object is known to be complete and can be scanned inside Write.
type scanningWriteCloser struct {
	real    io.WriteCloser
	buf     bytes.Buffer
	size    int64
	scanned bool
}

func (w *scanningWriteCloser) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if err != nil {
		return n, err
	}
	if !w.scanned && int64(w.buf.Len()) >= w.size {
		w.scanned = true
		if name, ok := secretscan.Scan(w.buf.Bytes()); ok {
			return n, fmt.Errorf("gitserver: push rejected, content matches known secret pattern %q", name)
		}
	}
	return n, nil
}

func (w *scanningWriteCloser) Close() error {
	if !w.scanned {
		if name, ok := secretscan.Scan(w.buf.Bytes()); ok {
			return fmt.Errorf("gitserver: push rejected, content matches known secret pattern %q", name)
		}
	}
	if _, err := w.real.Write(w.buf.Bytes()); err != nil {
		return err
	}
	return w.real.Close()
}
