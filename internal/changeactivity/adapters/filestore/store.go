// Package filestore persists private append-only discussion records.
package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"io"
	"os"
	"strings"
)

type Store struct{ root *os.Root }

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
func (s *Store) Close() error { return s.root.Close() }
func prefix(requestID string) string {
	digest := sha256.Sum256([]byte(requestID))
	return hex.EncodeToString(digest[:]) + "_"
}
func (s *Store) Append(ctx context.Context, c domain.Comment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	document, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(document)+1 > domain.MaxDocumentBytes {
		return domain.ErrInvalid
	}
	name := ".comment-" + rand.Text() + ".tmp"
	file, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer s.root.Remove(name)
	if _, err := file.Write(append(document, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Link(name, prefix(c.RequestID)+c.ID+".json"); err != nil {
		if errors.Is(err, os.ErrExist) {
			return domain.ErrConflict
		}
		return err
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Store) List(ctx context.Context, requestID string) ([]domain.Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !domain.ValidRequestID(requestID) {
		return nil, domain.ErrInvalid
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Comment, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix(requestID)) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, prefix(requestID)), ".json")
		if !strings.HasSuffix(name, ".json") || !domain.ValidCommentID(id) {
			return nil, domain.ErrCorrupt
		}
		info, err := s.root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > domain.MaxDocumentBytes {
			return nil, domain.ErrCorrupt
		}
		file, err := s.root.Open(name)
		if err != nil {
			return nil, err
		}
		document, readErr := io.ReadAll(io.LimitReader(file, domain.MaxDocumentBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		c, err := domain.DecodeComment(requestID, id, document)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}
