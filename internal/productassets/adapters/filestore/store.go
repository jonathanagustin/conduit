// Package filestore installs private immutable asset declarations atomically.
// File keys are hashes of coordinates, never supplied archive paths.
package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
)

type Store struct{ root *os.Root }

var _ ports.Store = (*Store)(nil)

func New(directory string) (*Store, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}
func (store *Store) Close() error { return store.root.Close() }
func key(coordinate string) string {
	digest := sha256.Sum256([]byte(coordinate))
	return hex.EncodeToString(digest[:]) + ".json"
}
func (store *Store) Prepare(ctx context.Context, record domain.Prepared) (domain.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return domain.Prepared{}, err
	}
	if err := record.Validate(); err != nil {
		return domain.Prepared{}, err
	}
	document, err := json.Marshal(record)
	if err != nil {
		return domain.Prepared{}, err
	}
	if len(document)+1 > domain.MaxDocumentBytes {
		return domain.Prepared{}, domain.ErrInvalid
	}
	name := ".assets-" + rand.Text() + ".tmp"
	temporary, err := store.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return domain.Prepared{}, err
	}
	defer store.root.Remove(name)
	if _, err := temporary.Write(append(document, '\n')); err != nil {
		_ = temporary.Close()
		return domain.Prepared{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return domain.Prepared{}, err
	}
	if err := temporary.Close(); err != nil {
		return domain.Prepared{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Prepared{}, err
	}
	if err := store.root.Link(name, key(record.Manifest.Coordinate())); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return domain.Prepared{}, err
		}
		existing, err := store.Get(ctx, record.Manifest.Coordinate())
		if err != nil {
			return domain.Prepared{}, err
		}
		if existing.Digest != record.Digest {
			return domain.Prepared{}, domain.ErrConflict
		}
		return existing, nil
	}
	directory, err := store.root.Open(".")
	if err != nil {
		return domain.Prepared{}, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return domain.Prepared{}, err
	}
	return domain.Clone(record), nil
}
func (store *Store) Get(ctx context.Context, coordinate string) (domain.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return domain.Prepared{}, err
	}
	if !domain.ValidCoordinate(coordinate) {
		return domain.Prepared{}, domain.ErrNotFound
	}
	info, err := store.root.Lstat(key(coordinate))
	if errors.Is(err, os.ErrNotExist) {
		return domain.Prepared{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Prepared{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > domain.MaxDocumentBytes {
		return domain.Prepared{}, domain.ErrCorrupt
	}
	file, err := store.root.Open(key(coordinate))
	if err != nil {
		return domain.Prepared{}, err
	}
	defer file.Close()
	document, err := io.ReadAll(io.LimitReader(file, domain.MaxDocumentBytes+1))
	if err != nil {
		return domain.Prepared{}, err
	}
	return domain.DecodePrepared(coordinate, document)
}
