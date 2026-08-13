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

const SubstratePreflightEvidenceAPIVersion = "optimization.platform.example/substrate-preflight/v1alpha2"

type SubstratePreflightRecord struct {
	APIVersion  string                       `json:"apiVersion"`
	Kind        string                       `json:"kind"`
	ID          string                       `json:"id"`
	CreatedAt   string                       `json:"createdAt"`
	Environment string                       `json:"environment"`
	Proposal    string                       `json:"proposal"`
	Request     SubstratePreflightRequest    `json:"request"`
	Assessment  SubstratePreflightAssessment `json:"assessment"`
}

func NewSubstratePreflightRecord(environment, proposal string, request SubstratePreflightRequest, now time.Time) (SubstratePreflightRecord, error) {
	if !validEvidenceCoordinate(environment) || !validEvidenceCoordinate(proposal) {
		return SubstratePreflightRecord{}, errors.New("substrate preflight requires safe environment and proposal coordinates")
	}
	snapshot, err := time.Parse(time.RFC3339, request.SnapshotAt)
	if err != nil {
		return SubstratePreflightRecord{}, fmt.Errorf("snapshotAt must be RFC3339: %w", err)
	}
	if now.IsZero() || now.Before(snapshot) {
		return SubstratePreflightRecord{}, errors.New("substrate preflight creation time must not predate its snapshot")
	}
	request = canonicalSubstratePreflightRequest(request)
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		return SubstratePreflightRecord{}, err
	}
	record := SubstratePreflightRecord{
		APIVersion: SubstratePreflightEvidenceAPIVersion,
		Kind:       "SubstratePreflight", CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Environment: environment, Proposal: proposal, Request: request, Assessment: assessment,
	}
	record.ID, err = substratePreflightRecordID(record)
	return record, err
}

func VerifySubstratePreflightRecord(record SubstratePreflightRecord) error {
	if record.APIVersion != SubstratePreflightEvidenceAPIVersion || record.Kind != "SubstratePreflight" || !validContentID(record.ID, "spf_") {
		return errors.New("unsupported or invalid substrate preflight record")
	}
	created, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("substrate preflight createdAt: %w", err)
	}
	if !validEvidenceCoordinate(record.Environment) || !validEvidenceCoordinate(record.Proposal) {
		return errors.New("substrate preflight has unsafe environment or proposal coordinate")
	}
	canonical := canonicalSubstratePreflightRequest(record.Request)
	if !reflect.DeepEqual(canonical, record.Request) {
		return errors.New("substrate preflight request is not in canonical order")
	}
	replayed, err := NewSubstratePreflightRecord(record.Environment, record.Proposal, record.Request, created)
	if err != nil {
		return fmt.Errorf("replay substrate preflight: %w", err)
	}
	if !reflect.DeepEqual(replayed.Assessment, record.Assessment) {
		return errors.New("substrate preflight assessment does not match replay")
	}
	if replayed.ID != record.ID {
		return errors.New("substrate preflight id does not match content")
	}
	return nil
}

func canonicalSubstratePreflightRequest(request SubstratePreflightRequest) SubstratePreflightRequest {
	request.Dependencies = append([]SubstrateDependencyEvidence(nil), request.Dependencies...)
	sort.Slice(request.Dependencies, func(i, j int) bool { return request.Dependencies[i].ID < request.Dependencies[j].ID })
	request.Capacity.Storage.Nodes = append([]NodeStorageEvidence(nil), request.Capacity.Storage.Nodes...)
	sort.Slice(request.Capacity.Storage.Nodes, func(i, j int) bool {
		return request.Capacity.Storage.Nodes[i].NodeID < request.Capacity.Storage.Nodes[j].NodeID
	})
	request.Demand.Storage.Images = append([]ImageStorageDemand(nil), request.Demand.Storage.Images...)
	sort.Slice(request.Demand.Storage.Images, func(i, j int) bool {
		return request.Demand.Storage.Images[i].Digest < request.Demand.Storage.Images[j].Digest
	})
	request.Demand.Storage.Placements = append([]NodeStorageDemand(nil), request.Demand.Storage.Placements...)
	for index := range request.Demand.Storage.Placements {
		request.Demand.Storage.Placements[index].ImageDigests = append([]string(nil), request.Demand.Storage.Placements[index].ImageDigests...)
		sort.Strings(request.Demand.Storage.Placements[index].ImageDigests)
	}
	sort.Slice(request.Demand.Storage.Placements, func(i, j int) bool {
		return request.Demand.Storage.Placements[i].NodeID < request.Demand.Storage.Placements[j].NodeID
	})
	return request
}

func substratePreflightRecordID(record SubstratePreflightRecord) (string, error) {
	payload := struct {
		Environment string                       `json:"environment"`
		Proposal    string                       `json:"proposal"`
		Request     SubstratePreflightRequest    `json:"request"`
		Assessment  SubstratePreflightAssessment `json:"assessment"`
	}{record.Environment, record.Proposal, record.Request, record.Assessment}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode substrate preflight identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "spf_" + hex.EncodeToString(digest[:]), nil
}

func validEvidenceCoordinate(value string) bool {
	if value == "" || len(value) > 256 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.Contains(value, "../") || strings.Contains(value, "/..") || strings.Contains(value, "//") {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:/@-", character) {
			continue
		}
		return false
	}
	return true
}

type FileSubstratePreflightStore struct{ Directory string }

func (store FileSubstratePreflightStore) Save(record SubstratePreflightRecord) error {
	if err := VerifySubstratePreflightRecord(record); err != nil {
		return err
	}
	if store.Directory == "" {
		return errors.New("substrate preflight store directory is required")
	}
	if err := os.MkdirAll(store.Directory, 0o700); err != nil {
		return fmt.Errorf("create substrate preflight store: %w", err)
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
		return errors.New("immutable substrate preflight id already contains different bytes")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	temporary, err := os.CreateTemp(store.Directory, ".substrate-preflight-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := writeSubstratePreflightTemporary(temporary, encoded); err != nil {
		return err
	}
	if err := os.Link(name, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return store.Save(record)
		}
		return fmt.Errorf("commit substrate preflight: %w", err)
	}
	directory, err := os.Open(store.Directory)
	if err == nil {
		err = directory.Sync()
		_ = directory.Close()
	}
	return err
}

func writeSubstratePreflightTemporary(temporary *os.File, encoded []byte) error {
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

func (store FileSubstratePreflightStore) Get(id string) (SubstratePreflightRecord, error) {
	if !validContentID(id, "spf_") {
		return SubstratePreflightRecord{}, errors.New("invalid substrate preflight id")
	}
	if store.Directory == "" {
		return SubstratePreflightRecord{}, errors.New("substrate preflight store directory is required")
	}
	encoded, err := os.ReadFile(filepath.Join(store.Directory, id+".json"))
	if err != nil {
		return SubstratePreflightRecord{}, err
	}
	var record SubstratePreflightRecord
	if err := decodeStoredJSON(encoded, &record); err != nil {
		return SubstratePreflightRecord{}, err
	}
	if err := VerifySubstratePreflightRecord(record); err != nil {
		return SubstratePreflightRecord{}, err
	}
	return record, nil
}

func decodeStoredJSON(encoded []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("stored JSON contains trailing JSON value")
		}
		return err
	}
	return nil
}
