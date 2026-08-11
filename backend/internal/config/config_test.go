package config

import (
	"os"
	"testing"
)

func TestLoad_UsesDefaultsWhenEnvNotSet(t *testing.T) {
	os.Unsetenv("DEVPLATFORM_LISTEN_ADDR")
	os.Unsetenv("DEVPLATFORM_DATA_DIR")

	cfg := Load()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "./data")
	}
}

func TestLoad_ReadsFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_LISTEN_ADDR", ":9090")
	os.Setenv("DEVPLATFORM_DATA_DIR", "/tmp/devplatform")
	defer os.Unsetenv("DEVPLATFORM_LISTEN_ADDR")
	defer os.Unsetenv("DEVPLATFORM_DATA_DIR")

	cfg := Load()

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.DataDir != "/tmp/devplatform" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/tmp/devplatform")
	}
}

func TestLoad_ReadsGitCredentialsFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_GIT_USERNAME", "devuser")
	os.Setenv("DEVPLATFORM_GIT_PASSWORD", "devpass")
	defer os.Unsetenv("DEVPLATFORM_GIT_USERNAME")
	defer os.Unsetenv("DEVPLATFORM_GIT_PASSWORD")

	cfg := Load()

	if cfg.GitUsername != "devuser" {
		t.Errorf("GitUsername = %q, want %q", cfg.GitUsername, "devuser")
	}
	if cfg.GitPassword != "devpass" {
		t.Errorf("GitPassword = %q, want %q", cfg.GitPassword, "devpass")
	}
}

func TestLoad_ReadsJWTSecretFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_JWT_SECRET", "super-secret")
	defer os.Unsetenv("DEVPLATFORM_JWT_SECRET")

	cfg := Load()

	if cfg.JWTSecret != "super-secret" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "super-secret")
	}
}
