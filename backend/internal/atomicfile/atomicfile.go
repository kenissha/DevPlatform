// Package atomicfile carries the one file-replacement detail every
// JSON-on-disk store in this codebase needs to get right on this
// platform.
//
// The stores all write a temp file and rename it over the target, which
// is the standard way to make a replacement atomic. On the deployment
// machine that rename intermittently fails with "Access is denied" even
// though nothing is wrong with either file: an on-access virus scanner
// still holds the just-created temp file open. The same scanner was
// already caught quarantining freshly built binaries — see docs/DURUM.md's
// 2026-09-03 entry.
//
// This lived as a private copy in internal/gitemails, then a second in
// internal/repodesc, and the third store to be bitten (internal/
// displaynames, 2026-09-07) is what turned it into a package. Every store
// that replaces a file should call Rename rather than os.Rename.
package atomicfile

import (
	"os"
	"time"
)

// attempts and delay are deliberately small and finite. The scanner's
// lock clears in milliseconds, so a handful of short retries turns a
// spurious failure into an imperceptible delay — while a genuine
// permission problem still surfaces quickly instead of hanging.
const (
	attempts = 8
	delay    = 15 * time.Millisecond
)

// Rename replaces to with from, retrying briefly on failure.
//
// It returns the last error rather than swallowing it: a real problem
// (wrong permissions, a path that doesn't exist) must still be reported
// to the caller, which is what tells a store its write did not land.
func Rename(from, to string) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}
