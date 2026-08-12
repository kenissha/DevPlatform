# Secrets Vault Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an administrator store a real secrets file (e.g. `appsettings.Production.json`) on the server, encrypted at rest, and have `internal/deploy`'s `Pipeline` automatically decrypt and inject it into a release directory during deploy — closing the "Secrets deposu" step named in the design doc's Faz 2.

**Architecture:** A new `internal/secretsvault` package: AES-256-GCM encrypt/decrypt primitives, a key loaded once from the `DEVPLATFORM_SECRETS_KEY` environment variable (never from a file — see the design doc's "neden Windows DPAPI değil" note for why), and a file-based `Store` mirroring `internal/deploy`'s existing repo/environment folder convention. A small standalone CLI (`cmd/secretsctl`, matching `cmd/deploydemo`'s existing "throwaway admin tool" pattern) lets the administrator encrypt a plaintext file into the vault and deletes the plaintext afterward. `internal/deploy/deploy.go`'s `Pipeline.Deploy` gains one new optional parameter to decrypt and write the right secrets file into the release directory between the build and IIS-swap steps.

**Tech Stack:** Go 1.22+ (backend), standard library only (`crypto/aes`, `crypto/cipher`, `crypto/rand`) — no new external dependencies.

## Global Constraints

- **The encryption key is never read from or written to a file.** It comes exclusively from the `DEVPLATFORM_SECRETS_KEY` environment variable, base64-encoded, decoding to exactly 32 bytes (AES-256). This is a deliberate design decision (portability across servers, see the design doc) — do not add a file-based key fallback.
- **No admin UI, no HTTP endpoint for uploading/reading secrets.** The administrator interacts with the vault only via the `secretsctl` CLI, run directly on the server. Nothing in this plan exposes plaintext or ciphertext secrets over HTTP.
- **AES-256-GCM specifically** (not CBC or another mode) — GCM is authenticated encryption: it detects tampering/corruption, not just confidentiality. A wrong key or altered ciphertext must fail decryption cleanly, not silently return garbage.
- Follow this codebase's established conventions: sentinel errors via `errors.Is`, regexp-validated `repo`/`environment` names before any path join (matching `repostore`/`taskboard`/`internal/deploy`'s own `VersionStore`), `0o750`/`0o640` permissions, one clear responsibility per file.
- Commit after every task; each commit must leave `go build ./...`, `go vet ./...`, and `go test ./...` (from `backend/`) passing. Comments in English, commit messages Conventional-Commits-ish (`feat:`/`test:`), in English.
- Unlike the previous `deploy` plan's `IISSwapper`, nothing in this plan requires Administrator privileges or a real IIS site — every piece here (encryption, file storage, the CLI, and the `Pipeline.Deploy` wiring) is fully testable with ordinary file I/O. No manual/elevated verification step is needed in this plan.

---

### Task 1: `internal/secretsvault` — AES-256-GCM encrypt/decrypt + key loading

**Files:**
- Create: `backend/internal/secretsvault/crypto.go`
- Test: `backend/internal/secretsvault/crypto_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `secretsvault.LoadKey() ([]byte, error)`, `secretsvault.Encrypt(plaintext, key []byte) ([]byte, error)`, `secretsvault.Decrypt(ciphertext, key []byte) ([]byte, error)`, sentinel errors `ErrKeyNotConfigured`, `ErrInvalidKeyLength`, `ErrDecryptionFailed`. Task 2's `Store` calls `Encrypt`/`Decrypt` directly; `cmd/secretsctl` (Task 3) and `Pipeline` (Task 4) call `LoadKey`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/secretsvault/crypto_test.go`:
```go
package secretsvault

import (
	"os"
	"testing"
)

func testKey() []byte {
	// 32 bytes, arbitrary fixed test value — never a real key.
	return []byte("01234567890123456789012345678901"[:32])
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := testKey()
	plaintext := []byte(`{"connectionString": "test-value-not-real"}`)

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncrypt_ProducesDifferentCiphertextEachTime(t *testing.T) {
	key := testKey()
	plaintext := []byte("same input")

	first, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("first Encrypt failed: %v", err)
	}
	second, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("second Encrypt failed: %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("encrypting the same plaintext twice must produce different ciphertext (fresh random nonce each time) — identical output means the nonce isn't varying, which breaks GCM's security guarantee")
	}
}

func TestDecrypt_FailsWithWrongKey(t *testing.T) {
	ciphertext, err := Encrypt([]byte("secret data"), testKey())
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	wrongKey := []byte("99999999999999999999999999999999"[:32])
	if _, err := Decrypt(ciphertext, wrongKey); err == nil {
		t.Fatal("expected Decrypt with the wrong key to fail, it succeeded")
	}
}

func TestDecrypt_FailsWithTamperedCiphertext(t *testing.T) {
	key := testKey()
	ciphertext, err := Encrypt([]byte("secret data"), key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF // flip the last byte

	if _, err := Decrypt(tampered, key); err == nil {
		t.Fatal("expected Decrypt of tampered ciphertext to fail, it succeeded")
	}
}

func TestLoadKey_ReadsFromEnv(t *testing.T) {
	key := testKey()
	encoded := base64StdEncode(key)
	os.Setenv("DEVPLATFORM_SECRETS_KEY", encoded)
	defer os.Unsetenv("DEVPLATFORM_SECRETS_KEY")

	loaded, err := LoadKey()
	if err != nil {
		t.Fatalf("LoadKey returned error: %v", err)
	}
	if string(loaded) != string(key) {
		t.Errorf("LoadKey returned %q, want %q", loaded, key)
	}
}

func TestLoadKey_ErrorsWhenUnset(t *testing.T) {
	os.Unsetenv("DEVPLATFORM_SECRETS_KEY")
	if _, err := LoadKey(); err != ErrKeyNotConfigured {
		t.Fatalf("err = %v, want ErrKeyNotConfigured", err)
	}
}

func TestLoadKey_ErrorsOnWrongLength(t *testing.T) {
	os.Setenv("DEVPLATFORM_SECRETS_KEY", base64StdEncode([]byte("too-short")))
	defer os.Unsetenv("DEVPLATFORM_SECRETS_KEY")

	if _, err := LoadKey(); err != ErrInvalidKeyLength {
		t.Fatalf("err = %v, want ErrInvalidKeyLength", err)
	}
}
```
Add a small test-only helper at the bottom of the same file:
```go
func base64StdEncode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
```
Add `"encoding/base64"` to the imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/secretsvault/... -v` from `backend/`.
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `crypto.go`**

Create `backend/internal/secretsvault/crypto.go`:
```go
// Package secretsvault encrypts and stores real secrets (production
// connection strings, service credentials) at rest, separately from git
// and outside any HTTP-reachable path. The encryption key never lives in
// a file on disk — it comes from the DEVPLATFORM_SECRETS_KEY environment
// variable at process start, so a copy of the encrypted files alone
// (a leaked backup, a misconfigured share, another process on the same
// server) is not enough to read their contents. See the design doc's
// "Secrets Deposu — Somutlaştırma Kararları" section for the full
// rationale, including why this is a self-managed key rather than
// Windows DPAPI (DPAPI ties the key to one specific machine, which would
// break moving to a different server).
package secretsvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrKeyNotConfigured = errors.New("secretsvault: DEVPLATFORM_SECRETS_KEY is not set")
	ErrInvalidKeyLength = errors.New("secretsvault: key must decode to exactly 32 bytes (AES-256)")
	ErrDecryptionFailed = errors.New("secretsvault: decryption failed (wrong key, or the data is corrupted/tampered)")
)

// LoadKey reads and base64-decodes the encryption key from the
// DEVPLATFORM_SECRETS_KEY environment variable.
func LoadKey() ([]byte, error) {
	encoded := os.Getenv("DEVPLATFORM_SECRETS_KEY")
	if encoded == "" {
		return nil, ErrKeyNotConfigured
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secretsvault: DEVPLATFORM_SECRETS_KEY is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	return key, nil
}

// Encrypt encrypts plaintext with key using AES-256-GCM. The returned
// value is a random nonce prepended to the ciphertext — the nonce isn't
// secret, it only must never repeat for the same key, and prepending it
// keeps it alongside the data it belongs to instead of needing separate
// storage.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretsvault: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretsvault: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secretsvault: failed to generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt. It fails — deliberately, via GCM's built-in
// authentication — if key is wrong or ciphertext was altered in any way;
// it never silently returns corrupted plaintext.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretsvault: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretsvault: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrDecryptionFailed
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/secretsvault/... -v`
Expected: PASS, all 7 tests.

- [ ] **Step 5: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/secretsvault/crypto.go backend/internal/secretsvault/crypto_test.go
git commit -m "feat: AES-256-GCM encrypt/decrypt primitives and key loading for secretsvault"
```

---

### Task 2: `internal/secretsvault` — file-based Store

**Files:**
- Create: `backend/internal/secretsvault/store.go`
- Test: `backend/internal/secretsvault/store_test.go`

**Interfaces:**
- Consumes: `Encrypt`/`Decrypt` (Task 1).
- Produces: `secretsvault.Store` with `NewStore(rootDir string, key []byte) *Store`, `(*Store) Put(repo, environment string, plaintext []byte) error`, `(*Store) Get(repo, environment string) ([]byte, error)`, sentinel errors `ErrInvalidRepo`, `ErrNotFound`. Task 3 (`secretsctl`) calls `Put`; Task 4 (`Pipeline`) calls `Get`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/secretsvault/store_test.go`:
```go
package secretsvault

import (
	"testing"
)

func TestPutGet_RoundTrip(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	original := []byte(`{"connectionString": "test-only-value"}`)
	if err := store.Put("sample", "test", original); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	got, err := store.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("Get = %q, want %q", got, original)
	}
}

func TestGet_ReturnsErrNotFoundWhenMissing(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if _, err := store.Get("sample", "test"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPut_OverwritesExisting(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	store.Put("sample", "test", []byte("first version"))
	if err := store.Put("sample", "test", []byte("second version")); err != nil {
		t.Fatalf("second Put returned error: %v", err)
	}

	got, err := store.Get("sample", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "second version" {
		t.Errorf("Get = %q, want %q", got, "second version")
	}
}

func TestPut_RejectsPathTraversalRepo(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if err := store.Put("../escape", "test", []byte("x")); err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestPut_RejectsPathTraversalEnvironment(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	if err := store.Put("sample", "../escape", []byte("x")); err != ErrInvalidRepo {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestDifferentRepoEnvironmentPairs_DontCollide(t *testing.T) {
	store := NewStore(t.TempDir(), testKey())

	store.Put("repo-a", "test", []byte("a-test"))
	store.Put("repo-a", "production", []byte("a-prod"))
	store.Put("repo-b", "test", []byte("b-test"))

	got, err := store.Get("repo-a", "test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "a-test" {
		t.Errorf("Get(repo-a, test) = %q, want %q", got, "a-test")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/secretsvault/... -run "TestPutGet|TestGet_ReturnsErrNotFound|TestPut_|TestDifferentRepo" -v`
Expected: FAIL — `Store`/`NewStore` don't exist yet.

- [ ] **Step 3: Implement `store.go`**

Create `backend/internal/secretsvault/store.go`:
```go
package secretsvault

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrInvalidRepo = errors.New("secretsvault: invalid repository or environment name")
	ErrNotFound    = errors.New("secretsvault: not found")
)

// validName mirrors the same allow-list pattern used throughout this
// codebase (repostore, taskboard, internal/deploy's VersionStore) for any
// identifier that becomes part of a filesystem path.
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Store persists encrypted secrets on disk, one file per (repo,
// environment) pair, organized the same way internal/deploy's
// VersionStore organizes release directories.
type Store struct {
	rootDir string
	key     []byte
}

// NewStore returns a Store rooted at rootDir, encrypting with key.
// rootDir does not need to exist yet.
func NewStore(rootDir string, key []byte) *Store {
	return &Store{rootDir: rootDir, key: key}
}

// Put encrypts plaintext and writes it to disk for (repo, environment),
// overwriting any existing file for that pair.
func (s *Store) Put(repo, environment string, plaintext []byte) error {
	path, err := s.path(repo, environment)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	encrypted, err := Encrypt(plaintext, s.key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o640)
}

// Get reads and decrypts the secrets file for (repo, environment).
// Returns ErrNotFound if nothing has been Put for that pair yet.
func (s *Store) Get(repo, environment string) ([]byte, error) {
	path, err := s.path(repo, environment)
	if err != nil {
		return nil, err
	}
	encrypted, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return Decrypt(encrypted, s.key)
}

func (s *Store) path(repo, environment string) (string, error) {
	if !validName.MatchString(repo) || !validName.MatchString(environment) {
		return "", ErrInvalidRepo
	}
	return filepath.Join(s.rootDir, repo, environment, "appsettings.json.enc"), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/secretsvault/... -v`
Expected: PASS, all tests across Task 1-2.

- [ ] **Step 5: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/secretsvault/store.go backend/internal/secretsvault/store_test.go
git commit -m "feat: file-based Store for encrypted secrets, keyed by repo and environment"
```

---

### Task 3: `cmd/secretsctl` — administrator CLI to encrypt a plaintext file into the vault

**Files:**
- Create: `backend/cmd/secretsctl/main.go`
- Test: `backend/cmd/secretsctl/main_test.go`

**Interfaces:**
- Consumes: `secretsvault.LoadKey`, `secretsvault.NewStore`, `(*secretsvault.Store).Put` (Tasks 1-2).
- Produces: a standalone binary. Nothing else in this plan imports `cmd/secretsctl` (it's a tool, not a library) — Task 4 does not depend on it.

- [ ] **Step 1: Extract the testable logic into a small function**

`main` functions with `flag.Parse()`/`os.Exit` aren't easily unit-tested. Put the actual logic in a plain function `main.go` can call, matching the shape of `internal/deploy`'s `Pipeline.Deploy` (a testable function, thinly wrapped by `main`).

Create `backend/cmd/secretsctl/main.go`:
```go
// Command secretsctl lets an administrator encrypt a plaintext secrets
// file (e.g. a real appsettings.Production.json) into DevPlatform's
// secrets vault, then deletes the plaintext source. Run this directly on
// the server — the plaintext file never needs to leave it, and this tool
// never sends anything over the network.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
)

func main() {
	repo := flag.String("repo", "", "repository name (required)")
	environment := flag.String("environment", "", "environment name, e.g. test or production (required)")
	file := flag.String("file", "", "path to the plaintext secrets file to encrypt (required)")
	dataDir := flag.String("data-dir", "./data", "DevPlatform data directory (secrets are stored under <data-dir>/secrets)")
	flag.Parse()

	if *repo == "" || *environment == "" || *file == "" {
		log.Fatal("usage: secretsctl -repo <name> -environment <name> -file <path> [-data-dir <path>]")
	}

	key, err := secretsvault.LoadKey()
	if err != nil {
		log.Fatalf("failed to load encryption key: %v", err)
	}

	if err := encryptAndStore(*repo, *environment, *file, *dataDir, key); err != nil {
		log.Fatal(err)
	}

	log.Printf("encrypted and stored secrets for repo=%s environment=%s; plaintext source deleted", *repo, *environment)
}

// encryptAndStore reads the plaintext file at filePath, encrypts it into
// the vault rooted at dataDir+"/secrets", and deletes the plaintext
// source on success. Separated from main so it's directly unit-testable.
func encryptAndStore(repo, environment, filePath, dataDir string, key []byte) error {
	plaintext, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	store := secretsvault.NewStore(dataDir+"/secrets", key)
	if err := store.Put(repo, environment, plaintext); err != nil {
		return err
	}

	if err := os.Remove(filePath); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 2: Write the failing test**

Create `backend/cmd/secretsctl/main_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
)

func TestEncryptAndStore_StoresAndDeletesPlaintext(t *testing.T) {
	work := t.TempDir()
	plaintextPath := filepath.Join(work, "appsettings.Production.json")
	content := []byte(`{"connectionString": "test-only-value"}`)
	if err := os.WriteFile(plaintextPath, content, 0o644); err != nil {
		t.Fatalf("failed to write test fixture: %v", err)
	}

	dataDir := filepath.Join(work, "data")
	key := []byte("01234567890123456789012345678901"[:32])

	if err := encryptAndStore("sample", "production", plaintextPath, dataDir, key); err != nil {
		t.Fatalf("encryptAndStore returned error: %v", err)
	}

	// The plaintext source must be gone.
	if _, err := os.Stat(plaintextPath); !os.IsNotExist(err) {
		t.Error("expected plaintext source file to be deleted")
	}

	// The vault must have it, decryptable back to the original content.
	store := secretsvault.NewStore(dataDir+"/secrets", key)
	got, err := store.Get("sample", "production")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("stored content = %q, want %q", got, content)
	}
}

func TestEncryptAndStore_LeavesPlaintextInPlaceIfFileMissing(t *testing.T) {
	work := t.TempDir()
	key := []byte("01234567890123456789012345678901"[:32])

	err := encryptAndStore("sample", "production", filepath.Join(work, "does-not-exist.json"), filepath.Join(work, "data"), key)
	if err == nil {
		t.Fatal("expected an error for a missing source file, got nil")
	}
}
```

- [ ] **Step 3: Run to verify it fails, then run again to verify it passes**

Run: `go test ./cmd/secretsctl/... -v` from `backend/`.
Expected first run (before Step 1's code exists, if you're following strict TDD ordering — write this test first, watch it fail to compile, then write `main.go`): FAIL.
After `main.go` is in place: PASS, both tests.

(This task's step ordering is Implement-then-test in the file listing above only because `main.go` and its test are naturally written together for a small CLI — follow strict TDD in practice: write `main_test.go` first, confirm it fails to compile against a nonexistent `encryptAndStore`, then write `main.go`.)

- [ ] **Step 4: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`. Also run `go build ./cmd/secretsctl` specifically to confirm the binary compiles.
Expected: all succeed.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/secretsctl
git commit -m "feat: secretsctl CLI to encrypt a plaintext secrets file into the vault"
```

---

### Task 4: Wire secrets injection into `Pipeline.Deploy`

**Files:**
- Modify: `backend/internal/deploy/deploy.go`
- Modify: `backend/internal/deploy/deploy_test.go`
- Modify: `backend/cmd/deploydemo/main.go`

**Interfaces:**
- Consumes: `secretsvault.Store` (`Get` method) from Tasks 1-2.
- Produces: `Pipeline.Deploy`'s signature changes (one new parameter) — this is a breaking change to that function's existing callers, all of which are updated in this same task. `NewPipeline`'s signature also changes (one new parameter, `secrets *secretsvault.Store`, which may be `nil`).

- [ ] **Step 1: Read the current `deploy.go` and `deploy_test.go`**

This task modifies an existing, already-shipped, already-tested file — read both files in full first so your changes match the current exact structure (the previous plan's final state, including the `keepVersions < 1` guard and `ErrPruneFailed`/`releaseStore` interface from earlier fixes).

- [ ] **Step 2: Write the failing tests for secrets injection**

Add to `backend/internal/deploy/deploy_test.go`:
```go
func TestPipeline_Deploy_InjectsSecretsWhenConfigured(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	key := []byte("01234567890123456789012345678901"[:32])
	secrets := secretsvault.NewStore(t.TempDir(), key)
	if err := secrets.Put("sample", "test", []byte(`{"connectionString": "test-only"}`)); err != nil {
		t.Fatalf("failed to seed secrets: %v", err)
	}

	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), secrets)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 5, "appsettings.Production.json")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(releaseDir, "appsettings.Production.json"))
	if err != nil {
		t.Fatalf("expected secrets file in release dir: %v", err)
	}
	if string(content) != `{"connectionString": "test-only"}` {
		t.Errorf("secrets file content = %q, want the seeded value", content)
	}
}

func TestPipeline_Deploy_SkipsSecretsWhenTargetEmpty(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	key := []byte("01234567890123456789012345678901"[:32])
	secrets := secretsvault.NewStore(t.TempDir(), key)
	// Deliberately not seeding any secrets for "sample"/"test" — proves
	// Deploy never even tries to read them when secretsTarget is empty.
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), secrets)

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 5, "")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(releaseDir, "appsettings.Production.json")); !os.IsNotExist(err) {
		t.Error("expected no secrets file to be written when secretsTarget is empty")
	}
}

func TestPipeline_Deploy_ErrorsWhenSecretsTargetGivenButNoStoreConfigured(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil) // no secrets store

	_, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 5, "appsettings.Production.json")
	if err == nil {
		t.Fatal("expected an error when secretsTarget is set but no secrets store is configured")
	}
}
```
Add `"github.com/kenissha/DevPlatform/backend/internal/secretsvault"` to `deploy_test.go`'s imports.

Update the three PRE-EXISTING `pipeline.Deploy(...)` calls already in this file (from the previous plan: `TestPipeline_Deploy_BuildsVersionsAndSwaps`, `TestPipeline_Deploy_PrunesOldReleases`, `TestPipeline_Deploy_RejectsNonPositiveKeepVersions`) and the existing `NewPipeline(...)` calls to match the new signatures — add `nil` as the 4th argument to each `NewPipeline` call (no secrets store needed for those pre-existing tests) and `""` as the 7th argument to each `Deploy` call (no secrets injection needed). Also update the `fakePruneFailingStore`-based test from the earlier fix round the same way if it constructs `Pipeline` directly rather than through `NewPipeline`.

- [ ] **Step 3: Run to verify the new tests fail**

Run: `go test ./internal/deploy/... -v` from `backend/`.
Expected: FAIL to compile — `NewPipeline`/`Deploy` don't accept the new parameters yet, and the pre-existing calls you haven't updated yet also won't compile. (You'll fix the pre-existing call sites as part of Step 4's implementation, since Go requires the whole package to compile before any test can run — that's expected here, not a sign of doing something wrong.)

- [ ] **Step 4: Update `deploy.go`**

Read the current file first (per Step 1) to see its exact present state, then make these changes:

1. Add the import `"github.com/kenissha/DevPlatform/backend/internal/secretsvault"`.
2. Add a `secrets *secretsvault.Store` field to the `Pipeline` struct.
3. Add a `secrets *secretsvault.Store` parameter to `NewPipeline`, storing it on the new field. It may be `nil`.
4. Change `Deploy`'s signature to add a `secretsTarget string` parameter at the end:
   ```go
   func (p *Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int, secretsTarget string) (string, error) {
   ```
5. Insert secrets injection between the existing `Build` step and the existing `SetPhysicalPath` step (matching the design doc's stated order: build → secrets copy → swap):
   ```go
   	if secretsTarget != "" {
   		if p.secrets == nil {
   			return "", fmt.Errorf("deploy: secretsTarget %q given but no secrets store is configured", secretsTarget)
   		}
   		plaintext, err := p.secrets.Get(repo, environment)
   		if err != nil {
   			return "", fmt.Errorf("deploy: failed to load secrets: %w", err)
   		}
   		if err := os.WriteFile(filepath.Join(releaseDir, secretsTarget), plaintext, 0o640); err != nil {
   			return "", fmt.Errorf("deploy: failed to write secrets into release: %w", err)
   		}
   	}
   ```
   Place this immediately after the `Build` call succeeds and before the `SetPhysicalPath` call — read the current function body to find the exact right spot, don't guess at line numbers. Add `"os"` and `"path/filepath"` to the imports if not already present.

- [ ] **Step 5: Update `cmd/deploydemo/main.go`**

Read the current file first. Update its `NewPipeline(...)` call to pass `nil` as the new 4th argument (the demo tool has no real secrets configured), and its `pipeline.Deploy(...)` call to pass `""` as the new 7th argument (no secrets injection for the demo).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, all tests — the 3 new secrets-injection tests plus every pre-existing test in this package (now updated to compile against the new signatures).

- [ ] **Step 7: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`. Also run `go build ./cmd/deploydemo` specifically to confirm it still compiles.
Expected: all succeed.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/deploy/deploy.go backend/internal/deploy/deploy_test.go backend/cmd/deploydemo/main.go
git commit -m "feat: inject decrypted secrets into the release directory during Pipeline.Deploy"
```

- [ ] **Step 9: Push**

```bash
git push origin main
```

---

## Self-Review Notes

- **Spec coverage:** Covers the design doc's "Secrets Deposu — Somutlaştırma Kararları" end to end: no admin UI (CLI only), two-layer protection (this plan implements the encryption layer; folder-permission hardening on the real server is an operational step for whoever runs `secretsctl`, not code), key never on disk (env var only), repo/environment folder structure matching `internal/deploy`'s existing convention, and the deploy-flow wiring (build → secrets → swap) in the exact order the original design specified.
- **Placeholder scan:** No TBD/TODO. Task 3's step ordering note (implement-then-test file listing, TDD-in-practice instruction) is an explicit, deliberate clarification, not a placeholder.
- **Type consistency:** `Store.Put`/`Get`'s signatures (Task 2) are used identically by `cmd/secretsctl` (Task 3) and by `Pipeline` (Task 4, via `p.secrets.Get`). `Encrypt`/`Decrypt` (Task 1) are used identically by `Store` (Task 2). `Pipeline.Deploy`'s new `secretsTarget` parameter and `NewPipeline`'s new `secrets` parameter are threaded consistently through every call site touched in Task 4 (the package's own tests and `deploydemo`).
- **Security:** the core property — encrypted at rest, key never persisted to any file, decryption failures are loud (GCM authentication) rather than silent — is tested directly (wrong-key and tampered-ciphertext tests in Task 1). No HTTP surface exposes secrets at any point in this plan. Path-traversal defense on `repo`/`environment` matches the established codebase-wide pattern, tested in Task 2. `Pipeline.Deploy` fails loudly (returns an error) rather than silently skipping when a caller asks for secrets injection but none is configured — a caller-side misconfiguration surfaces immediately rather than deploying an app missing its real config.
- **Scope:** deliberately does not add folder-ACL automation (the administrator is expected to have already secured the data directory, consistent with how the original design doc frames "sadece platformun ve Yönetici'nin erişebildiği klasör" as a server-configuration fact, not something this Go code enforces) and does not wire this into any HTTP-triggered approval workflow yet — that remains a future plan, same as the previous `deploy` plan's own explicit scope boundary.
