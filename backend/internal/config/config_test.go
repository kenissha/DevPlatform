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

func TestLoad_ReadsSMTPSettingsFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_SMTP_HOST", "smtp.example.com")
	os.Setenv("DEVPLATFORM_SMTP_PORT", "587")
	os.Setenv("DEVPLATFORM_SMTP_FROM", "devplatform@example.com")
	defer os.Unsetenv("DEVPLATFORM_SMTP_HOST")
	defer os.Unsetenv("DEVPLATFORM_SMTP_PORT")
	defer os.Unsetenv("DEVPLATFORM_SMTP_FROM")

	cfg := Load()

	if cfg.SMTPHost != "smtp.example.com" {
		t.Errorf("SMTPHost = %q, want %q", cfg.SMTPHost, "smtp.example.com")
	}
	if cfg.SMTPPort != "587" {
		t.Errorf("SMTPPort = %q, want %q", cfg.SMTPPort, "587")
	}
	if cfg.SMTPFrom != "devplatform@example.com" {
		t.Errorf("SMTPFrom = %q, want %q", cfg.SMTPFrom, "devplatform@example.com")
	}
}
