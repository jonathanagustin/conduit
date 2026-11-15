package main

import (
	"strings"

	"github.com/jonathanagustin/conduit/internal/adminstatus/adapters/filesystem"
	"github.com/jonathanagustin/conduit/internal/adminstatus/ports"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// Only the local file repository supplies an observable data filesystem.
// Remote databases, object stores and registries need their own telemetry port.
func adminStorageForRepository(repository controlplane.Repository, getenv func(string) string) map[string]ports.CapacityReader {
	if getenv == nil {
		return nil
	}
	probes := map[string]ports.CapacityReader{}
	if _, ok := repository.(*controlplane.FileRepository); ok {
		path := strings.TrimSpace(getenv("DATA_DIRECTORY"))
		if path == "" {
			path = ".platform-delivery"
		}
		probes["data"] = filesystem.Reader{Path: path}
	}
	for key, env := range map[string]string{"registry": "ADMIN_REGISTRY_DATA_DIRECTORY", "objectStore": "ADMIN_OBJECT_STORE_DATA_DIRECTORY"} {
		if path := strings.TrimSpace(getenv(env)); path != "" {
			probes[key] = filesystem.Reader{Path: path}
		}
	}
	return probes
}
