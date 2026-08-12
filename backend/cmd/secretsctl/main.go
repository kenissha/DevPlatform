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
