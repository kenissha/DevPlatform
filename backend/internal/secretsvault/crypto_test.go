package secretsvault

import (
	"encoding/base64"
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

func base64StdEncode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
