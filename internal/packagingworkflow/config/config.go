// Package config reads the Hub's packaging Temporal settings. An empty
// address leaves the integration disabled, which is how a small install stays
// unchanged.
package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// Config is the Temporal client and worker configuration.
type Config struct {
	Enabled    bool
	Address    string
	Namespace  string
	TaskQueue  string
	TLS        TLS
	APIKey     string
	FixtureDir string
	DataDir    string
}

// TLS is optional mutual TLS material. File paths are configuration. The
// key bytes are loaded at dial time and are not stored here.
type TLS struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

// FromEnv reads process environment. PACKAGING_TEMPORAL_ADDRESS empty means
// disabled. The API key is read from a file, not taken as a value that would
// be printed back.
func FromEnv(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	address := strings.TrimSpace(getenv("PACKAGING_TEMPORAL_ADDRESS"))
	cfg := Config{
		Enabled:    address != "",
		Address:    address,
		Namespace:  first(getenv("PACKAGING_TEMPORAL_NAMESPACE"), "default"),
		TaskQueue:  first(getenv("PACKAGING_TEMPORAL_TASK_QUEUE"), "packaging-localhost"),
		FixtureDir: strings.TrimSpace(getenv("PACKAGING_FIXTURE_DIR")),
		DataDir:    strings.TrimSpace(getenv("DATA_DIRECTORY")),
		TLS: TLS{
			CertFile:   strings.TrimSpace(getenv("PACKAGING_TEMPORAL_TLS_CERT_FILE")),
			KeyFile:    strings.TrimSpace(getenv("PACKAGING_TEMPORAL_TLS_KEY_FILE")),
			CAFile:     strings.TrimSpace(getenv("PACKAGING_TEMPORAL_TLS_CA_FILE")),
			ServerName: strings.TrimSpace(getenv("PACKAGING_TEMPORAL_SERVER_NAME")),
		},
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return Config{}, fmt.Errorf("PACKAGING_TEMPORAL_TLS_CERT_FILE and PACKAGING_TEMPORAL_TLS_KEY_FILE must be set together")
	}
	keyFile := strings.TrimSpace(getenv("PACKAGING_TEMPORAL_API_KEY_FILE"))
	if keyFile != "" {
		body, err := os.ReadFile(keyFile)
		if err != nil {
			return Config{}, fmt.Errorf("read PACKAGING_TEMPORAL_API_KEY_FILE: %w", err)
		}
		cfg.APIKey = strings.TrimSpace(string(body))
		if cfg.APIKey == "" || strings.ContainsAny(cfg.APIKey, "\r\n") {
			return Config{}, fmt.Errorf("PACKAGING_TEMPORAL_API_KEY_FILE must contain one line")
		}
	}
	return cfg, nil
}

// ClientTLS builds a tls.Config when client certificates or a CA are set.
func (c Config) ClientTLS() (*tls.Config, error) {
	if c.TLS.CertFile == "" && c.TLS.CAFile == "" {
		return nil, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLS.ServerName}
	if c.TLS.CertFile != "" {
		certificate, err := tls.LoadX509KeyPair(c.TLS.CertFile, c.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load packaging Temporal client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if c.TLS.CAFile != "" {
		pem, err := os.ReadFile(c.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read packaging Temporal CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("PACKAGING_TEMPORAL_TLS_CA_FILE contained no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

func first(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
