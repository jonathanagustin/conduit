package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/spaces/adapters/filestore"
	spacepostgres "github.com/jonathanagustin/conduit/internal/spaces/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/spaces/ports"
)

// openSpaces follows the delivery repository's durability mode. It uses an
// independent component so legacy snapshot writers cannot discard grouping.
func openSpaces(ctx context.Context, getenv func(string) string) (ports.Store, func(), error) {
	empty := func() {}
	if getenv == nil {
		return nil, empty, errors.New("environment reader is required")
	}
	kind := strings.TrimSpace(getenv("REPOSITORY_KIND"))
	switch kind {
	case "", "file":
		directory := strings.TrimSpace(getenv("DATA_DIRECTORY"))
		if directory == "" {
			directory = ".platform-delivery"
		}
		store, err := filestore.New(filepath.Join(directory, "spaces"))
		return store, empty, err
	case "postgres":
		dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
		if err != nil {
			return nil, empty, err
		}
		store, err := spacepostgres.New(ctx, dsn)
		if err != nil {
			return nil, empty, errors.New("initialize PostgreSQL Space component failed")
		}
		return store, func() { _ = store.Close() }, nil
	default:
		return nil, empty, fmt.Errorf("REPOSITORY_KIND must be file or postgres, got %q", kind)
	}
}
