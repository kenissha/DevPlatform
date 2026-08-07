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
