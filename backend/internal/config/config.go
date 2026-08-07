package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr string
	DataDir    string
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
func Load() Config {
	return Config{
		ListenAddr: getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:    getEnv("DEVPLATFORM_DATA_DIR", "./data"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
