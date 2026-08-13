package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const ValueEvidenceAPIVersion = "optimization.platform.example/value/v1alpha1"

type ValueAssessmentRecord struct {
	APIVersion   string             `json:"apiVersion"`
	Kind         string             `json:"kind"`
	ID           string             `json:"id"`
	SnapshotID   string             `json:"snapshotId"`
	CreatedAt    string             `json:"createdAt"`
	Criteria     []ValueCriterion   `json:"criteria"`
	Alternatives []ValueAlternative `json:"alternatives"`
	Assessment   ValueAssessment    `json:"assessment"`
}

func NewValueAssessmentRecord(snapshotID string, criteria []ValueCriterion, alternatives []ValueAlternative, now time.Time) (ValueAssessmentRecord, error) {
	if !validContentID(snapshotID, "snap_") {
		return ValueAssessmentRecord{}, errors.New("value assessment requires content-addressed twin snapshot id")
	}
	criteria, alternatives = canonicalValueInputs(criteria, alternatives)
	assessment, err := AssessValue(criteria, alternatives)
	if err != nil {
		return ValueAssessmentRecord{}, err
	}
	record := ValueAssessmentRecord{APIVersion: ValueEvidenceAPIVersion, Kind: "ValueAssessment", SnapshotID: snapshotID, CreatedAt: now.UTC().Format(time.RFC3339Nano), Criteria: criteria, Alternatives: alternatives, Assessment: assessment}
	id, err := valueRecordID(record)
	if err != nil {
		return ValueAssessmentRecord{}, err
	}
	record.ID = id
	return record, nil
}

func canonicalValueInputs(criteria []ValueCriterion, alternatives []ValueAlternative) ([]ValueCriterion, []ValueAlternative) {
	criteria = append([]ValueCriterion(nil), criteria...)
	alternatives = append([]ValueAlternative(nil), alternatives...)
	sort.Slice(criteria, func(i, j int) bool { return criteria[i].ID < criteria[j].ID })
	sort.Slice(alternatives, func(i, j int) bool { return alternatives[i].ID < alternatives[j].ID })
	for i := range alternatives {
		alternatives[i].Assumptions = append([]string(nil), alternatives[i].Assumptions...)
		sort.Strings(alternatives[i].Assumptions)
	}
	return criteria, alternatives
}

func VerifyValueAssessmentRecord(record ValueAssessmentRecord) error {
	if record.APIVersion != ValueEvidenceAPIVersion || record.Kind != "ValueAssessment" || record.SnapshotID == "" {
		return errors.New("unsupported or incomplete value assessment record")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.CreatedAt); err != nil {
		return fmt.Errorf("value assessment createdAt: %w", err)
	}
	assessment, err := AssessValue(record.Criteria, record.Alternatives)
	if err != nil {
		return fmt.Errorf("replay value assessment: %w", err)
	}
	if !reflect.DeepEqual(assessment, record.Assessment) {
		return errors.New("stored value assessment does not match replay")
	}
	id, err := valueRecordID(record)
	if err != nil {
		return err
	}
	if id != record.ID {
		return errors.New("value assessment id does not match content")
	}
	return nil
}

func valueRecordID(record ValueAssessmentRecord) (string, error) {
	payload := struct {
		SnapshotID   string             `json:"snapshotId"`
		Criteria     []ValueCriterion   `json:"criteria"`
		Alternatives []ValueAlternative `json:"alternatives"`
		Assessment   ValueAssessment    `json:"assessment"`
	}{record.SnapshotID, record.Criteria, record.Alternatives, record.Assessment}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode value assessment identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "val_" + hex.EncodeToString(digest[:]), nil
}

type FileValueAssessmentStore struct{ Directory string }

func (store FileValueAssessmentStore) Save(record ValueAssessmentRecord) error {
	if err := VerifyValueAssessmentRecord(record); err != nil {
		return err
	}
	if store.Directory == "" {
		return errors.New("value assessment store directory is required")
	}
	if err := os.MkdirAll(store.Directory, 0o700); err != nil {
		return fmt.Errorf("create value assessment store: %w", err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	final := filepath.Join(store.Directory, record.ID+".json")
	if existing, readErr := os.ReadFile(final); readErr == nil {
		if string(existing) == string(encoded) {
			return nil
		}
		return errors.New("immutable value assessment id already contains different bytes")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	temporary, err := os.CreateTemp(store.Directory, ".value-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := writeValueAssessmentTemporary(temporary, encoded); err != nil {
		return err
	}
	if err := os.Link(temporaryName, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return store.Save(record)
		}
		return fmt.Errorf("commit value assessment: %w", err)
	}
	directory, err := os.Open(store.Directory)
	if err == nil {
		err = directory.Sync()
		_ = directory.Close()
	}
	return err
}

type valueAssessmentTemporary interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func writeValueAssessmentTemporary(temporary valueAssessmentTemporary, encoded []byte) error {
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	written, err := temporary.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func (store FileValueAssessmentStore) Get(id string) (ValueAssessmentRecord, error) {
	if !validValueRecordID(id) {
		return ValueAssessmentRecord{}, errors.New("invalid value assessment id")
	}
	encoded, err := os.ReadFile(filepath.Join(store.Directory, id+".json"))
	if err != nil {
		return ValueAssessmentRecord{}, err
	}
	var record ValueAssessmentRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return ValueAssessmentRecord{}, err
	}
	if err := VerifyValueAssessmentRecord(record); err != nil {
		return ValueAssessmentRecord{}, err
	}
	return record, nil
}
func (store FileValueAssessmentStore) List() ([]ValueAssessmentRecord, error) {
	entries, err := os.ReadDir(store.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return []ValueAssessmentRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			id := strings.TrimSuffix(entry.Name(), ".json")
			if validValueRecordID(id) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	records := make([]ValueAssessmentRecord, 0, len(ids))
	for _, id := range ids {
		record, loadErr := store.Get(id)
		if loadErr != nil {
			return nil, loadErr
		}
		records = append(records, record)
	}
	return records, nil
}
func validValueRecordID(id string) bool {
	if len(id) != 68 || !strings.HasPrefix(id, "val_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "val_"))
	return err == nil
}
