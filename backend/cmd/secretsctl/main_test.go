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
