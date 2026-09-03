package main

import (
	"testing"
	"time"
)

func TestDPAPI_ProtectThenUnprotect_RoundTrips(t *testing.T) {
	plaintext := []byte(`{"subject":"dev-1","token":"abc123"}`)

	encrypted, err := dpapiProtect(plaintext)
	if err != nil {
		t.Fatalf("dpapiProtect returned error: %v", err)
	}
	if string(encrypted) == string(plaintext) {
		t.Fatal("dpapiProtect returned the plaintext unchanged")
	}

	decrypted, err := dpapiUnprotect(encrypted)
	if err != nil {
		t.Fatalf("dpapiUnprotect returned error: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip = %q, want %q", decrypted, plaintext)
	}
}

func TestSaveCache_ThenLoadCache_RoundTrips(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	want := cachedCredential{Subject: "dev-1", Token: "abc123", CachedAt: time.Now().UTC().Truncate(time.Second)}
	if err := saveCache(want); err != nil {
		t.Fatalf("saveCache returned error: %v", err)
	}

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got == nil {
		t.Fatal("loadCache returned nil after saveCache")
	}
	if got.Subject != want.Subject || got.Token != want.Token {
		t.Errorf("loadCache = %+v, want %+v", got, want)
	}
}

func TestLoadCache_MissingFileReturnsNilNotError(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got != nil {
		t.Errorf("loadCache = %+v, want nil (no cache file yet)", got)
	}
}

func TestClearCache_ThenLoadCache_ReturnsNil(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := saveCache(cachedCredential{Subject: "dev-1", Token: "abc123", CachedAt: time.Now()}); err != nil {
		t.Fatalf("saveCache returned error: %v", err)
	}

	if err := clearCache(); err != nil {
		t.Fatalf("clearCache returned error: %v", err)
	}

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got != nil {
		t.Errorf("loadCache after clearCache = %+v, want nil", got)
	}
}

func TestClearCache_NoCacheFileIsNotAnError(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	if err := clearCache(); err != nil {
		t.Errorf("clearCache with no cache file returned error: %v", err)
	}
}
