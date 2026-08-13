package optimization

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileTwinStore struct{ Directory string }

func (store FileTwinStore) SaveFact(fact TemporalFact) error {
	if err := VerifyTemporalFact(fact); err != nil {
		return err
	}
	return store.save("facts", fact.ID, fact)
}

func (store FileTwinStore) SaveSnapshot(snapshot TwinSnapshot) error {
	if err := VerifyTwinSnapshot(snapshot); err != nil {
		return err
	}
	return store.save("snapshots", snapshot.ID, snapshot)
}

func (store FileTwinStore) GetFact(id string) (TemporalFact, error) {
	var fact TemporalFact
	if !validContentID(id, "fact_") {
		return fact, errors.New("invalid temporal fact id")
	}
	if err := store.get("facts", id, &fact); err != nil {
		return fact, err
	}
	return fact, VerifyTemporalFact(fact)
}

func (store FileTwinStore) GetSnapshot(id string) (TwinSnapshot, error) {
	var snapshot TwinSnapshot
	if !validContentID(id, "snap_") {
		return snapshot, errors.New("invalid twin snapshot id")
	}
	if err := store.get("snapshots", id, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, VerifyTwinSnapshot(snapshot)
}

func (store FileTwinStore) save(kind, id string, value any) error {
	if store.Directory == "" {
		return errors.New("twin store directory is required")
	}
	directory := filepath.Join(store.Directory, kind)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	// Use compact JSON. MarshalIndent rewrites bytes inside nested RawMessage
	// values, which can invalidate content-addressed value digests after a
	// save/load round trip even though the semantic JSON is unchanged.
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	final := filepath.Join(directory, id+".json")
	if existing, readErr := os.ReadFile(final); readErr == nil {
		if string(existing) == string(encoded) {
			return nil
		}
		return errors.New("immutable twin record contains different bytes")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	temporary, err := os.CreateTemp(directory, ".twin-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(encoded)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Link(name, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return store.save(kind, id, value)
		}
		return fmt.Errorf("commit twin record: %w", err)
	}
	dir, err := os.Open(directory)
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	return err
}

func (store FileTwinStore) get(kind, id string, value any) error {
	if store.Directory == "" {
		return errors.New("twin store directory is required")
	}
	encoded, err := os.ReadFile(filepath.Join(store.Directory, kind, id+".json"))
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("immutable twin record has trailing input")
	}
	return nil
}

func validContentID(id, prefix string) bool {
	if len(id) != len(prefix)+64 || !strings.HasPrefix(id, prefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, prefix))
	return err == nil
}
