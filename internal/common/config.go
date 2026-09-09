package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	HTTPPort               string
	DatabaseURL            string
	JWTSecret              string
	WorkerCount            int
	KafkaBrokers           []string
	SchedulerStrategy      string
	AgentPort              string
	NodeBootstrapSecret    string
	AgentTLSCAFile         string
	AgentClientTLSCertFile string
	AgentClientTLSKeyFile  string
	OTLPEndpoint           string
	RedisAddr              string
	TLSCertFile            string // optional — set with TLSKeyFile to serve HTTPS
	TLSKeyFile             string // instead of plain HTTP; unset by default
}

// Load reads configuration from the environment and validates it.
// It fails fast if required values are missing or structurally invalid.
func Load() (Config, error) {
	workerCount, err := strconv.Atoi(getEnv("NEBULA_WORKER_COUNT", "3"))
	if err != nil || workerCount <= 0 {
		return Config{}, fmt.Errorf("NEBULA_WORKER_COUNT must be a positive integer")
	}

	httpPort := getEnv("NEBULA_HTTP_PORT", "8080")
	if p, err := strconv.Atoi(httpPort); err != nil || p < 1 || p > 65535 {
		return Config{}, fmt.Errorf("NEBULA_HTTP_PORT must be a valid port number (1-65535)")
	}

	cfg := Config{
		HTTPPort:               httpPort,
		DatabaseURL:            os.Getenv("NEBULA_DATABASE_URL"),
		JWTSecret:              os.Getenv("NEBULA_JWT_SECRET"),
		WorkerCount:            workerCount,
		KafkaBrokers:           strings.Split(getEnv("NEBULA_KAFKA_BROKERS", "kafka:9092"), ","),
		SchedulerStrategy:      getEnv("NEBULA_SCHEDULER_STRATEGY", "weighted"),
		AgentPort:              getEnv("NEBULA_AGENT_PORT", "7071"),
		NodeBootstrapSecret:    os.Getenv("NEBULA_NODE_BOOTSTRAP_SECRET"),
		AgentTLSCAFile:         os.Getenv("NEBULA_AGENT_TLS_CA_FILE"),
		AgentClientTLSCertFile: os.Getenv("NEBULA_AGENT_CLIENT_TLS_CERT_FILE"),
		AgentClientTLSKeyFile:  os.Getenv("NEBULA_AGENT_CLIENT_TLS_KEY_FILE"),
		OTLPEndpoint:           getEnv("NEBULA_OTLP_ENDPOINT", "http://jaeger:4318"),
		RedisAddr:              getEnv("NEBULA_REDIS_ADDR", "redis:6379"),
		TLSCertFile:            os.Getenv("NEBULA_TLS_CERT_FILE"),
		TLSKeyFile:             os.Getenv("NEBULA_TLS_KEY_FILE"),
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
	// Phase 18: mTLS is mandatory (closes the Phase 10/section 51 "mTLS
	// later" deferral) — nebula-api's own client identity when it dials
	// nebula-agent, required the same way the CA file above already is.
	if cfg.AgentClientTLSCertFile == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_CLIENT_TLS_CERT_FILE is required")
	}
	if cfg.AgentClientTLSKeyFile == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_CLIENT_TLS_KEY_FILE is required")
	}
	// Unlike agent mTLS, nebula-api's own HTTP TLS stays opt-in — every
	// existing plain `curl http://localhost:8080/...` example and the
	// CLI's default URL keep working unchanged when these are unset.
	// Structural validation only: both or neither, not one alone.
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return Config{}, fmt.Errorf("NEBULA_TLS_CERT_FILE and NEBULA_TLS_KEY_FILE must be set together, or not at all")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
