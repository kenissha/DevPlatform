package config

import (
	"os"
	"strconv"
)

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr   string
	DataDir      string
	GitUsername  string
	GitPassword  string
	JWTSecret    string
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	// BaseURL, if set, is prefixed onto a notification's relative link
	// (e.g. "/repos/x/tasks") when it's included in an outgoing email, so
	// the link is clickable from a mail client instead of only readable as
	// text. Empty by default — no sensible local-dev default exists, since
	// it depends on wherever this server is actually reachable from.
	BaseURL string
	// DeployTargetsFile points at a JSON file listing the (repo,
	// environment) pairs this server is allowed to deploy (see
	// deployment.LoadTargets). Empty by default: no target is deployable
	// until an admin deliberately creates this file, matching the design
	// doc's "sabit listeden" requirement — a deploy target is server-side
	// configuration, never something typed into the panel.
	DeployTargetsFile string
	// BackupDir, if set, turns on internal/backup's nightly job: every
	// repository is copied into this directory once a day at BackupHour.
	// Empty by default — no backup runs until an operator points this at a
	// real destination (ideally a different disk/machine than DataDir).
	BackupDir string
	// BackupHour is the server-local hour (0-23) the nightly backup runs
	// at. Only meaningful when BackupDir is set.
	BackupHour int
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
//
// The GitUsername/GitPassword "dev"/"dev" defaults exist only so the server
// boots without configuration during local development. They are not real
// credentials — every environment beyond a developer's own machine must set
// DEVPLATFORM_GIT_USERNAME and DEVPLATFORM_GIT_PASSWORD explicitly.
//
// JWTSecret's "dev-not-a-real-secret" default is the same kind of
// placeholder: internal/auth validates incoming JWTs (issued by an
// external identity system this platform trusts, rather than DevPlatform
// doing its own AD/LDAP login) against this HMAC secret. It must be set to
// the real shared secret configured on that external system via
// DEVPLATFORM_JWT_SECRET before this platform is reachable by anyone but a
// developer on their own machine.
//
// SMTPHost is the switch main.go checks to decide whether real mail is
// possible at all: empty (the default) means notify.NoopEmailSender stays
// in place and notifications remain panel-only, exactly today's behavior.
// Set it to turn on internal/notify.SMTPEmailSender. SMTPUsername/Password
// are optional on top of that — leave them empty for an anonymous internal
// relay that doesn't require AUTH; SMTPEmailSender only attempts AUTH when
// Username is set AND the server advertises support for it.
func Load() Config {
	return Config{
		ListenAddr:        getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:           getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		GitUsername:       getEnv("DEVPLATFORM_GIT_USERNAME", "dev"),
		GitPassword:       getEnv("DEVPLATFORM_GIT_PASSWORD", "dev"),
		JWTSecret:         getEnv("DEVPLATFORM_JWT_SECRET", "dev-not-a-real-secret"),
		SMTPHost:          getEnv("DEVPLATFORM_SMTP_HOST", ""),
		SMTPPort:          getEnv("DEVPLATFORM_SMTP_PORT", "25"),
		SMTPUsername:      getEnv("DEVPLATFORM_SMTP_USERNAME", ""),
		SMTPPassword:      getEnv("DEVPLATFORM_SMTP_PASSWORD", ""),
		SMTPFrom:          getEnv("DEVPLATFORM_SMTP_FROM", "devplatform@localhost"),
		BaseURL:           getEnv("DEVPLATFORM_BASE_URL", ""),
		DeployTargetsFile: getEnv("DEVPLATFORM_DEPLOY_TARGETS_FILE", ""),
		BackupDir:         getEnv("DEVPLATFORM_BACKUP_DIR", ""),
		BackupHour:        getEnvInt("DEVPLATFORM_BACKUP_HOUR", 2),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
