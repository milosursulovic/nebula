package agent

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds nebula-agent's configuration, loaded from the environment.
// Distinct from internal/common.Config (nebula-api's config) — the two
// binaries share no fields.
type Config struct {
	APIURL            string
	Hostname          string
	IP                string
	CPU               int
	MemoryMB          int
	DiskGB            int
	Port              string
	HeartbeatInterval time.Duration
	BootstrapSecret   string
	TLSCertFile       string
	TLSKeyFile        string
	HypervisorBackend string
	LibvirtURI        string
	DiskRoot          string
	OTLPEndpoint      string
}

// Load reads configuration from the environment and validates it, failing
// fast if required values are missing (same convention as
// internal/common.Load).
func Load() (Config, error) {
	apiURL := os.Getenv("NEBULA_API_URL")
	if apiURL == "" {
		return Config{}, fmt.Errorf("NEBULA_API_URL is required")
	}

	ip := os.Getenv("NEBULA_AGENT_IP")
	if ip == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_IP is required")
	}

	bootstrapSecret := os.Getenv("NEBULA_NODE_BOOTSTRAP_SECRET")
	if bootstrapSecret == "" {
		return Config{}, fmt.Errorf("NEBULA_NODE_BOOTSTRAP_SECRET is required")
	}

	tlsCertFile := os.Getenv("NEBULA_AGENT_TLS_CERT_FILE")
	if tlsCertFile == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_TLS_CERT_FILE is required")
	}
	tlsKeyFile := os.Getenv("NEBULA_AGENT_TLS_KEY_FILE")
	if tlsKeyFile == "" {
		return Config{}, fmt.Errorf("NEBULA_AGENT_TLS_KEY_FILE is required")
	}

	hostname := os.Getenv("NEBULA_AGENT_HOSTNAME")
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			return Config{}, fmt.Errorf("NEBULA_AGENT_HOSTNAME not set and os.Hostname failed: %w", err)
		}
		hostname = h
	}

	cpu, err := strconv.Atoi(getEnv("NEBULA_AGENT_CPU", ""))
	if err != nil || cpu <= 0 {
		return Config{}, fmt.Errorf("NEBULA_AGENT_CPU must be a positive integer")
	}
	memoryMB, err := strconv.Atoi(getEnv("NEBULA_AGENT_MEMORY_MB", ""))
	if err != nil || memoryMB <= 0 {
		return Config{}, fmt.Errorf("NEBULA_AGENT_MEMORY_MB must be a positive integer")
	}
	diskGB, err := strconv.Atoi(getEnv("NEBULA_AGENT_DISK_GB", ""))
	if err != nil || diskGB <= 0 {
		return Config{}, fmt.Errorf("NEBULA_AGENT_DISK_GB must be a positive integer")
	}

	interval, err := time.ParseDuration(getEnv("NEBULA_AGENT_HEARTBEAT_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		return Config{}, fmt.Errorf("NEBULA_AGENT_HEARTBEAT_INTERVAL must be a positive duration")
	}

	return Config{
		APIURL:            apiURL,
		Hostname:          hostname,
		IP:                ip,
		CPU:               cpu,
		MemoryMB:          memoryMB,
		DiskGB:            diskGB,
		Port:              getEnv("NEBULA_AGENT_PORT", "7071"),
		HeartbeatInterval: interval,
		BootstrapSecret:   bootstrapSecret,
		TLSCertFile:       tlsCertFile,
		TLSKeyFile:        tlsKeyFile,
		HypervisorBackend: getEnv("NEBULA_AGENT_HYPERVISOR", "mock"),
		LibvirtURI:        getEnv("NEBULA_AGENT_LIBVIRT_URI", "qemu:///system"),
		DiskRoot:          getEnv("NEBULA_AGENT_DISK_ROOT", "/var/lib/nebula/disks"),
		OTLPEndpoint:      getEnv("NEBULA_OTLP_ENDPOINT", "http://jaeger:4318"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
