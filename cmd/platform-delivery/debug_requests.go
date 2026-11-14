package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/debugsessions/adapters/filestore"
	debugpostgres "github.com/jonathanagustin/conduit/internal/debugsessions/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
)

// openDebugSessionJournal opens metadata persistence only. It supplies no broker,
// credential, Pod admission or execution authority.
func openDebugSessionJournal(ctx context.Context, getenv func(string) string) (ports.Journal, func(), error) {
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
		journal, err := filestore.New(filepath.Join(directory, "debug-session-journal"))
		if err != nil {
			return nil, empty, err
		}
		return journal, func() { _ = journal.Close() }, nil
	case "postgres":
		dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
		if err != nil {
			return nil, empty, err
		}
		journal, err := debugpostgres.New(ctx, dsn)
		if err != nil {
			return nil, empty, errors.New("initialize PostgreSQL debug-session journal failed")
		}
		return journal, func() { _ = journal.Close() }, nil
	default:
		return nil, empty, errors.New("REPOSITORY_KIND must be file or postgres")
	}
}
