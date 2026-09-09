package common

import (
	"strings"
	"testing"
)

// setRequiredEnv sets every env var Load() requires unconditionally, so
// each test below only needs to vary the one thing it's testing.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NEBULA_DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("NEBULA_JWT_SECRET", "test-secret")
	t.Setenv("NEBULA_NODE_BOOTSTRAP_SECRET", "test-bootstrap-secret")
	t.Setenv("NEBULA_AGENT_TLS_CA_FILE", "/tmp/agent-ca.crt")
	t.Setenv("NEBULA_AGENT_CLIENT_TLS_CERT_FILE", "/tmp/client.crt")
	t.Setenv("NEBULA_AGENT_CLIENT_TLS_KEY_FILE", "/tmp/client.key")
}

func TestLoadValid(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want default 8080", cfg.HTTPPort)
	}
	if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		t.Errorf("expected TLS unset by default, got cert=%q key=%q", cfg.TLSCertFile, cfg.TLSKeyFile)
	}
}

func TestLoadRejectsInvalidHTTPPort(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("NEBULA_HTTP_PORT", "not-a-port")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEBULA_HTTP_PORT") {
		t.Fatalf("Load() error = %v, want an error mentioning NEBULA_HTTP_PORT", err)
	}
}

func TestLoadRejectsOutOfRangeHTTPPort(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("NEBULA_HTTP_PORT", "99999")

	if _, err := Load(); err == nil {
		t.Fatal("Load() with an out-of-range port: expected an error, got nil")
	}
}

func TestLoadRejectsTLSCertWithoutKey(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("NEBULA_TLS_CERT_FILE", "/tmp/api.crt")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEBULA_TLS_CERT_FILE") {
		t.Fatalf("Load() error = %v, want an error about TLS cert/key needing to be set together", err)
	}
}

func TestLoadAcceptsTLSCertAndKeyTogether(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("NEBULA_TLS_CERT_FILE", "/tmp/api.crt")
	t.Setenv("NEBULA_TLS_KEY_FILE", "/tmp/api.key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TLSCertFile != "/tmp/api.crt" || cfg.TLSKeyFile != "/tmp/api.key" {
		t.Errorf("unexpected TLS config: %+v", cfg)
	}
}

func TestLoadRequiresAgentClientCert(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("NEBULA_AGENT_CLIENT_TLS_CERT_FILE", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEBULA_AGENT_CLIENT_TLS_CERT_FILE") {
		t.Fatalf("Load() error = %v, want an error about the required client cert", err)
	}
}
