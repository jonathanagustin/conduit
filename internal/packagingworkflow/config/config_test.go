package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyAddressDisablesTheIntegration(t *testing.T) {
	cfg, err := FromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("empty address enabled Temporal")
	}
}

func TestAddressEnablesNamespaceAndTaskQueueDefaults(t *testing.T) {
	cfg, err := FromEnv(func(key string) string {
		if key == "PACKAGING_TEMPORAL_ADDRESS" {
			return "temporal.example:7233"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Namespace != "default" || cfg.TaskQueue != "packaging-localhost" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestAPIKeyIsReadFromAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-key")
	if err := os.WriteFile(path, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromEnv(func(key string) string {
		switch key {
		case "PACKAGING_TEMPORAL_ADDRESS":
			return "127.0.0.1:7233"
		case "PACKAGING_TEMPORAL_API_KEY_FILE":
			return path
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "secret-token" {
		t.Fatalf("api key = %q", cfg.APIKey)
	}
}
