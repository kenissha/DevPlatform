package config

import (
	"os"
	"strconv"
)

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr   string
	DataDir      string
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
	// AllowedSitesFile points at a small, ops-edited JSON array of IIS
	// site names (see internal/iishelper.LoadAllowedSites) — the only
	// sites a deploy target's siteName may ever name. Empty by default:
	// no site is approved until an operator deliberately creates this
	// file. Deploy target *content* (which repo/environment maps to
	// which of these sites) is panel-managed (see
	// internal/deployment.TargetStore) — this file is deliberately the one
	// piece that stays outside the panel's reach, see
	// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
	// "Güvenlik" section.
	AllowedSitesFile string
	// BackupDir, if set, turns on internal/backup's nightly job: every
	// repository is copied into this directory once a day at BackupHour.
	// Empty by default — no backup runs until an operator points this at a
	// real destination (ideally a different disk/machine than DataDir).
	BackupDir string
	// BackupHour is the server-local hour (0-23) the nightly backup runs
	// at. Only meaningful when BackupDir is set.
	BackupHour int
	// FrontendDir, if set, points at the built frontend's static files
	// (frontend/dist after `npm run build`) and turns on serving them
	// from this same process/origin as the API — the frontend's own code
	// assumes it's always talking to its own origin (no CORS setup, no
	// separate API base URL), so in production the frontend and API must
	// be served from the same address. Empty by default: local
	// development instead runs the frontend through Vite's dev server,
	// which proxies API calls to this backend (see frontend/vite.config.ts).
	FrontendDir string
	// LoginCLIPath, if set, points at a built devplatform-login.exe on
	// disk (see backend/cmd/devplatform-login) and turns on serving it
	// (and a matching install.ps1) so people can set it up with one
	// command — `irm https://<host>/devplatform-login/install.ps1 | iex`
	// — instead of the exe being manually copied machine to machine.
	// Empty by default: no binary is served until an operator points
	// this at one, the same "nothing until deliberately configured"
	// pattern as AllowedSitesFile/BackupDir.
	LoginCLIPath string
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
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
		ListenAddr:        listenAddr(),
		DataDir:           getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		JWTSecret:         getEnv("DEVPLATFORM_JWT_SECRET", "dev-not-a-real-secret"),
		SMTPHost:          getEnv("DEVPLATFORM_SMTP_HOST", ""),
		SMTPPort:          getEnv("DEVPLATFORM_SMTP_PORT", "25"),
		SMTPUsername:      getEnv("DEVPLATFORM_SMTP_USERNAME", ""),
		SMTPPassword:      getEnv("DEVPLATFORM_SMTP_PASSWORD", ""),
		SMTPFrom:          getEnv("DEVPLATFORM_SMTP_FROM", "devplatform@localhost"),
		BaseURL:           getEnv("DEVPLATFORM_BASE_URL", ""),
		AllowedSitesFile:  getEnv("DEVPLATFORM_ALLOWED_SITES_FILE", ""),
		BackupDir:         getEnv("DEVPLATFORM_BACKUP_DIR", ""),
		BackupHour:        getEnvInt("DEVPLATFORM_BACKUP_HOUR", 2),
		FrontendDir:       getEnv("DEVPLATFORM_FRONTEND_DIR", ""),
		LoginCLIPath:      getEnv("DEVPLATFORM_LOGIN_CLI_PATH", ""),
	}
}

// listenAddr resolves the address to serve on, preferring IIS's
// HTTP_PLATFORM_PORT over DEVPLATFORM_LISTEN_ADDR.
//
// When IIS's httpPlatformHandler launches this process it picks a free
// port, passes it as HTTP_PLATFORM_PORT, and forwards requests arriving
// at the site's own binding to that port. Ignoring it and listening on a
// fixed port of our own would mean traffic reaching us without ever
// passing through IIS — IIS would see an application with no traffic,
// idle it out, and then have no way to wake it, since the next request
// would again bypass it. That failure mode cost a full debugging session
// on 2026-08-14; see docs/DURUM.md.
//
// Outside IIS (local development, or running as a Windows service) the
// variable is unset and DEVPLATFORM_LISTEN_ADDR applies as before.
func listenAddr() string {
	if port := os.Getenv("HTTP_PLATFORM_PORT"); port != "" {
		return ":" + port
	}
	return getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080")
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
