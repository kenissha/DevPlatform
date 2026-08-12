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

func TestLoad_SMTPHostDefaultsToEmptyMeaningNoRealMail(t *testing.T) {
	os.Unsetenv("DEVPLATFORM_SMTP_HOST")

	cfg := Load()

	// main.go's decision to wire a real SMTPEmailSender hinges entirely on
	// this being non-empty — an accidental non-empty default here would
	// make every fresh deployment attempt to send real mail to a server
	// that was never configured.
	if cfg.SMTPHost != "" {
		t.Errorf("SMTPHost = %q, want \"\" by default", cfg.SMTPHost)
	}
}

func TestLoad_ReadsSMTPCredentialsAndBaseURLFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_SMTP_USERNAME", "devplatform")
	os.Setenv("DEVPLATFORM_SMTP_PASSWORD", "s3cret")
	os.Setenv("DEVPLATFORM_BASE_URL", "https://devplatform.internal")
	defer os.Unsetenv("DEVPLATFORM_SMTP_USERNAME")
	defer os.Unsetenv("DEVPLATFORM_SMTP_PASSWORD")
	defer os.Unsetenv("DEVPLATFORM_BASE_URL")

	cfg := Load()

	if cfg.SMTPUsername != "devplatform" {
		t.Errorf("SMTPUsername = %q, want %q", cfg.SMTPUsername, "devplatform")
	}
	if cfg.SMTPPassword != "s3cret" {
		t.Errorf("SMTPPassword = %q, want %q", cfg.SMTPPassword, "s3cret")
	}
	if cfg.BaseURL != "https://devplatform.internal" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://devplatform.internal")
	}
}
