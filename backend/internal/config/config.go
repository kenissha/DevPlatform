package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr  string
	DataDir     string
	GitUsername string
	GitPassword string
	JWTSecret   string
	SMTPHost    string
	SMTPPort    string
	SMTPFrom    string
	// DeployTargetsFile points at a JSON file listing the (repo,
	// environment) pairs this server is allowed to deploy (see
	// deployment.LoadTargets). Empty by default: no target is deployable
	// until an admin deliberately creates this file, matching the design
	// doc's "sabit listeden" requirement — a deploy target is server-side
	// configuration, never something typed into the panel.
	DeployTargetsFile string
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
// SMTPHost/SMTPPort/SMTPFrom are unused placeholders: internal/notify's
// NoopEmailSender doesn't read them, and no real EmailSender implementation
// exists yet to send through them. They exist now only so operators can
// start setting DEVPLATFORM_SMTP_* in their environment ahead of a future
// plan that wires a real SMTP-backed EmailSender to these values — unlike
// GitUsername/GitPassword, there's no sensible non-empty local-dev default
// for an SMTP host, so they default to "".
func Load() Config {
	return Config{
		ListenAddr:        getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:           getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		GitUsername:       getEnv("DEVPLATFORM_GIT_USERNAME", "dev"),
		GitPassword:       getEnv("DEVPLATFORM_GIT_PASSWORD", "dev"),
		JWTSecret:         getEnv("DEVPLATFORM_JWT_SECRET", "dev-not-a-real-secret"),
		SMTPHost:          getEnv("DEVPLATFORM_SMTP_HOST", ""),
		SMTPPort:          getEnv("DEVPLATFORM_SMTP_PORT", ""),
		SMTPFrom:          getEnv("DEVPLATFORM_SMTP_FROM", ""),
		DeployTargetsFile: getEnv("DEVPLATFORM_DEPLOY_TARGETS_FILE", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
