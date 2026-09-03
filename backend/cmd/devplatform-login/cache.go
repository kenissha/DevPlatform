package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// cachedCredential is what's persisted (DPAPI-encrypted) between runs.
type cachedCredential struct {
	Subject  string    `json:"subject"`
	Token    string    `json:"token"`
	CachedAt time.Time `json:"cachedAt"`
}

func cachePath() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "devplatform", "credential"), nil
}

// loadCache returns the cached credential, or (nil, nil) if there is
// none yet (missing file — the normal first-run state, not an error).
func loadCache() (*cachedCredential, error) {
	path, err := cachePath()
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
	var cred cachedCredential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		return nil, nil
	}
	return &cred, nil
}

// saveCache encrypts and persists cred, replacing any previous cache.
func saveCache(cred cachedCredential) error {
	path, err := cachePath()
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

// clearCache removes the cached credential, if any — called on
// `erase` (git told us the credential it tried failed), so the next
// `get` starts a fresh login instead of handing out the same bad
// token again.
func clearCache() error {
	path, err := cachePath()
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
