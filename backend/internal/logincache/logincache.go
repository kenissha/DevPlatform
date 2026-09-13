// Package logincache is the credential devplatform-login leaves on the
// machine, and the one place that knows how to read it.
//
// It was a private file inside cmd/devplatform-login until a second
// program needed the same credential: cmd/devplatform-mcp acts as the
// person who logged in, and the whole point of that design is that it
// mints nothing of its own (see internal/apiauth). Two copies of a DPAPI
// call and a file path is exactly the kind of duplication that drifts —
// one side gains a field, the other silently reads the old shape.
//
// Windows-only, deliberately: DPAPI ties the file to one Windows account
// on one machine, so copying it elsewhere yields nothing. That is the
// property worth having, and there is no portable equivalent to fall back
// to — this platform runs on Windows.
package logincache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Credential is what's persisted (DPAPI-encrypted) between runs.
type Credential struct {
	Subject  string    `json:"subject"`
	Token    string    `json:"token"`
	CachedAt time.Time `json:"cachedAt"`
}

func Path() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "devplatform", "credential"), nil
}

// Load returns the cached credential, or (nil, nil) if there is
// none yet (missing file — the normal first-run state, not an error).
func Load() (*Credential, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	encrypted, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	plaintext, err := dpapiUnprotect(encrypted)
	if err != nil {
		// A cache file that no longer decrypts (copied from another
		// machine/user, or corrupted) is treated the same as "no cache" —
		// the next login attempt just creates a fresh one, rather than
		// this tool refusing to work at all.
		return nil, nil
	}
	var cred Credential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		return nil, nil
	}
	return &cred, nil
}

// Save encrypts and persists cred, replacing any previous cache.
func Save(cred Credential) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	plaintext, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	encrypted, err := dpapiProtect(plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o600)
}

// Clear removes the cached credential, if any — called on
// `erase` (git told us the credential it tried failed), so the next
// `get` starts a fresh login instead of handing out the same bad
// token again.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func dpapiProtect(plaintext []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(plaintext))}
	if len(plaintext) > 0 {
		in.Data = &plaintext[0]
	}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

func dpapiUnprotect(ciphertext []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(ciphertext))}
	if len(ciphertext) > 0 {
		in.Data = &ciphertext[0]
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

func blobToBytes(b windows.DataBlob) []byte {
	result := unsafe.Slice(b.Data, b.Size)
	cp := make([]byte, len(result))
	copy(cp, result)
	return cp
}
