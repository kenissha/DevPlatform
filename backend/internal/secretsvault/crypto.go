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
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
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
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
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
