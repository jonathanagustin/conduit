package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/productassets/adapters/filestore"
	assetpostgres "github.com/jonathanagustin/conduit/internal/productassets/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
)

// openProductAssets follows the repository's durability mode, independently of
// delivery snapshots. Opening the store performs no catalogue publication.
func openProductAssets(ctx context.Context, getenv func(string) string) (ports.Store, func(), error) {
	empty := func() {}
	if getenv == nil {
		return nil, empty, errors.New("environment reader is required")
	}
	switch strings.TrimSpace(getenv("REPOSITORY_KIND")) {
	case "", "file":
		directory := strings.TrimSpace(getenv("DATA_DIRECTORY"))
		if directory == "" {
			directory = ".platform-delivery"
		}
		store, err := filestore.New(filepath.Join(directory, "product-assets"))
		if err != nil {
			return nil, empty, err
		}
		return store, func() { _ = store.Close() }, nil
	case "postgres":
		dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
		if err != nil {
			return nil, empty, err
		}
		store, err := assetpostgres.New(ctx, dsn)
		if err != nil {
			return nil, empty, errors.New("initialize PostgreSQL download-asset component failed")
		}
		return store, func() { _ = store.Close() }, nil
	default:
		return nil, empty, errors.New("REPOSITORY_KIND must be file or postgres")
	}
}
