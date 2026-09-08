package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	HTTPPort            string
	DatabaseURL         string
	JWTSecret           string
	WorkerCount         int
	KafkaBrokers        []string
	SchedulerStrategy   string
	AgentPort           string
	NodeBootstrapSecret string
	AgentTLSCAFile      string
}

// Load reads configuration from the environment and validates it.
// It fails fast if required values are missing.
func Load() (Config, error) {
	workerCount, err := strconv.Atoi(getEnv("NEBULA_WORKER_COUNT", "3"))
	if err != nil || workerCount <= 0 {
		return Config{}, fmt.Errorf("NEBULA_WORKER_COUNT must be a positive integer")
	}

	cfg := Config{
		HTTPPort:            getEnv("NEBULA_HTTP_PORT", "8080"),
		DatabaseURL:         os.Getenv("NEBULA_DATABASE_URL"),
		JWTSecret:           os.Getenv("NEBULA_JWT_SECRET"),
		WorkerCount:         workerCount,
		KafkaBrokers:        strings.Split(getEnv("NEBULA_KAFKA_BROKERS", "kafka:9092"), ","),
		SchedulerStrategy:   getEnv("NEBULA_SCHEDULER_STRATEGY", "weighted"),
		AgentPort:           getEnv("NEBULA_AGENT_PORT", "7071"),
		NodeBootstrapSecret: os.Getenv("NEBULA_NODE_BOOTSTRAP_SECRET"),
		AgentTLSCAFile:      os.Getenv("NEBULA_AGENT_TLS_CA_FILE"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("NEBULA_DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("NEBULA_JWT_SECRET is required")
	}
	if cfg.NodeBootstrapSecret == "" {
		return Config{}, fmt.Errorf("NEBULA_NODE_BOOTSTRAP_SECRET is required")
	}
	if cfg.AgentTLSCAFile == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_TLS_CA_FILE is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
