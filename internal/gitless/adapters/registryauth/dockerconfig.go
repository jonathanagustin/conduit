// Package registryauth reads registry credentials from a mounted Docker
// configuration file.
//
// It is deliberately a file adapter rather than an environment adapter. The
// Hub may need the same credential for source resolution, Cosign verification,
// and publishing the manifests Flux consumes; mounting one Secret keeps those
// consumers on one rotation boundary without copying its contents into process
// arguments, logs, or desired state.
package registryauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dockerConfigName is the file name cosign -- and every other client that reads
// DOCKER_CONFIG -- joins onto that directory.
const dockerConfigName = "config.json"

// DockerConfigDirectory returns the directory to hand cosign as DOCKER_CONFIG
// for a credential file, or explains why that file cannot be one.
//
// It is a check and not a conversion, and the reason it has to exist is a
// failure mode with no symptom. cosign reads DOCKER_CONFIG as a DIRECTORY and
// looks for config.json inside it; pointed at a directory with no config.json
// it does not complain, it authenticates anonymously. So a Hub mounting its
// credential as, say, `.dockerconfigjson` -- the key a Kubernetes
// dockerconfigjson Secret projects by default -- would start cleanly, resolve
// and publish with credentials through the native adapters, and verify
// anonymously, which fails against a private registry with a 401 that names
// neither the cause nor the fix.
//
// Refusing at configuration time instead makes that a startup error naming both.
func DockerConfigDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("registry credential file path is empty")
	}
	if base := filepath.Base(path); base != dockerConfigName {
		return "", fmt.Errorf(
			"registry credential file must be named %s, not %s: Cosign reads the DIRECTORY named by "+
				"DOCKER_CONFIG and looks for %s inside it, so any other name would leave signature "+
				"verification authenticating anonymously while resolution and publication did not",
			dockerConfigName, base, dockerConfigName)
	}
	directory, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve the registry credential directory: %w", err)
	}
	return directory, nil
}

// DockerConfig supplies Basic credentials for registry clients. It satisfies
// both ociresolve.Credentials and ocipublish.Credentials without either adapter
// depending on this concrete implementation.
type DockerConfig struct {
	auths map[string]credential
}

type credential struct {
	username string
	password string
}

type dockerConfigDocument struct {
	Auths map[string]dockerAuth `json:"auths"`
}

type dockerAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Load reads a Docker configuration from path. A configured file must contain
// at least one usable credential; an empty or helper-only document would make
// the Hub appear configured while every private-registry operation failed on
// its first reconcile pass.
func Load(path string) (*DockerConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("registry credential file path is empty")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry credential file: %w", err)
	}
	var document dockerConfigDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("decode registry credential file: %w", err)
	}
	auths := make(map[string]credential, len(document.Auths))
	for rawHost, entry := range document.Auths {
		host := normalizeHost(rawHost)
		if host == "" {
			continue
		}
		username, password, err := decode(entry)
		if err != nil {
			return nil, fmt.Errorf("decode credentials for %s: %w", host, err)
		}
		if username == "" && password == "" {
			continue
		}
		auths[host] = credential{username: username, password: password}
	}
	if len(auths) == 0 {
		return nil, errors.New("registry credential file contains no usable auth entries")
	}
	return &DockerConfig{auths: auths}, nil
}

func decode(entry dockerAuth) (string, string, error) {
	if entry.Auth == "" {
		return entry.Username, entry.Password, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return "", "", errors.New("auth is not valid base64")
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found {
		return "", "", errors.New("decoded auth has no username separator")
	}
	return username, password, nil
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/v1/")
	return strings.TrimRight(value, "/")
}

// Basic returns credentials for exactly host. Once a Docker configuration is
// explicitly selected, an absent host is an error rather than an anonymous
// fallback: silently changing authentication mode is the unsafe direction for
// a mounted production credential to fail.
func (config *DockerConfig) Basic(_ context.Context, host string) (string, string, error) {
	entry, ok := config.auths[normalizeHost(host)]
	if !ok {
		return "", "", fmt.Errorf("registry credential file has no entry for %s", host)
	}
	return entry.username, entry.password, nil
}
