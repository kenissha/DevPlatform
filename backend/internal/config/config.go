package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr  string
	DataDir     string
	GitUsername string
	GitPassword string
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
//
// The GitUsername/GitPassword "dev"/"dev" defaults exist only so the server
// boots without configuration during local development. They are not real
// credentials — every environment beyond a developer's own machine must set
// DEVPLATFORM_GIT_USERNAME and DEVPLATFORM_GIT_PASSWORD explicitly.
func Load() Config {
	return Config{
		ListenAddr:  getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:     getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		GitUsername: getEnv("DEVPLATFORM_GIT_USERNAME", "dev"),
		GitPassword: getEnv("DEVPLATFORM_GIT_PASSWORD", "dev"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
