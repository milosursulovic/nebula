package common

import (
	"fmt"
	"os"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	HTTPPort    string
	DatabaseURL string
	JWTSecret   string
}

// Load reads configuration from the environment and validates it.
// It fails fast if required values are missing.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:    getEnv("NEBULA_HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("NEBULA_DATABASE_URL"),
		JWTSecret:   os.Getenv("NEBULA_JWT_SECRET"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("NEBULA_DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("NEBULA_JWT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
