package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

func exportFixture(t *testing.T) ([]byte, []domain.AuditCheckpoint) {
	t.Helper()
	var output bytes.Buffer
	checkpoints := []domain.AuditCheckpoint{domain.EmptyAuditCheckpoint()}
	for index, id := range []string{"first", "second", "third"} {
		event := domain.NewAuthorizationCheckEvent(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "operator", "read", "resource", domain.DecisionAllow, "<retained bytes>", nil)
		event.ID = id
		origin := domain.AuditOriginLive
		if index == 0 {
			origin = domain.AuditOriginLegacy
		}
		record, err := domain.NewAuditChainRecord(checkpoints[index], event, origin)
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteAuditExportRecord(&output, record); err != nil {
			t.Fatal(err)
		}
		checkpoints = append(checkpoints, domain.AuditCheckpoint{Version: domain.AuditChainVersion, Sequence: record.Sequence, Digest: record.Digest})
	}
	if err := FinishAuditExport(&output, checkpoints[3]); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), checkpoints
}

func TestAuditExportPreservesStoredChainAndIndependentPrefix(t *testing.T) {
	archive, checkpoints := exportFixture(t)
	for _, trusted := range []*domain.AuditCheckpoint{nil, &checkpoints[0], &checkpoints[1], &checkpoints[3]} {
		tip, err := VerifyAuditExport(t.Context(), bytes.NewReader(archive), trusted)
		if err != nil || tip != checkpoints[3] {
			t.Fatalf("verified=%#v, error=%v", tip, err)
		}
	}
	// Inspect transport bytes independently of the export's reader. The legacy
	// origin and base64-decoded JSON bytes must remain inputs to the stored hash.
	var line struct {
		Format string `json:"format"`
		Record struct {
			Origin  string `json:"origin"`
			Payload []byte `json:"payload"`
			Digest  string `json:"digest"`
		} `json:"record"`
	}
	if err := json.Unmarshal(bytes.Split(archive, []byte{'\n'})[0], &line); err != nil {
		t.Fatal(err)
	}
	if line.Format != AuditExportFormat || line.Record.Origin != domain.AuditOriginLegacy || line.Record.Digest != checkpoints[1].Digest || !bytes.Contains(line.Record.Payload, []byte(`\u003cretained bytes\u003e`)) {
		t.Fatalf("stored evidence changed: %#v", line)
	}
}

func TestAuditExportRefusesTamperingTruncationAndConflictingTrust(t *testing.T) {
	archive, checkpoints := exportFixture(t)
	lines := strings.Split(strings.TrimSuffix(string(archive), "\n"), "\n")
	for name, changed := range map[string]string{
		"empty":                "",
		"missing footer":       strings.Join(lines[:3], "\n"),
		"missing middle":       strings.Join([]string{lines[0], lines[2], lines[3]}, "\n"),
		"duplicate event":      strings.Join([]string{lines[0], lines[0], lines[1], lines[2], lines[3]}, "\n"),
		"wrong footer":         strings.Join(lines[:3], "\n") + "\n" + strings.Replace(lines[3], `"sequence":3`, `"sequence":9`, 1),
		"changed origin":       strings.Replace(string(archive), `"origin":"LEGACY_IMPORT"`, `"origin":"LIVE"`, 1),
		"changed identity":     strings.Replace(string(archive), `"eventId":"first"`, `"eventId":"forged"`, 1),
		"unknown format":       strings.Replace(string(archive), AuditExportFormat, "other", 1),
		"unknown field":        strings.Replace(string(archive), `"record":{`, `"unknown":true,"record":{`, 1),
		"two values per line":  lines[0] + "{}\n" + strings.Join(lines[1:], "\n"),
		"both envelope bodies": strings.Replace(string(archive), `"record":{`, `"checkpoint":{},"record":{`, 1),
		"trailing event":       string(archive) + lines[0] + "\n",
		"trailing blank line":  string(archive) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyAuditExport(t.Context(), strings.NewReader(changed), nil); err == nil {
				t.Fatal("invalid archive verified")
			}
		})
	}
	for _, trusted := range []domain.AuditCheckpoint{
		{Version: domain.AuditChainVersion, Sequence: 1, Digest: strings.Repeat("a", 64)},
		{Version: domain.AuditChainVersion, Sequence: 4, Digest: strings.Repeat("b", 64)},
		{Version: "unknown", Sequence: 1, Digest: checkpoints[1].Digest},
	} {
		if _, err := VerifyAuditExport(t.Context(), bytes.NewReader(archive), &trusted); err == nil {
			t.Fatalf("conflicting checkpoint verified: %#v", trusted)
		}
	}
}

func TestAuditExportEmptyLedgerAndReadBounds(t *testing.T) {
	var archive bytes.Buffer
	if err := FinishAuditExport(&archive, domain.EmptyAuditCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyAuditExport(t.Context(), &archive, nil); err != nil || got != domain.EmptyAuditCheckpoint() {
		t.Fatalf("empty ledger=%#v, %v", got, err)
	}
	if _, err := VerifyAuditExport(t.Context(), strings.NewReader(strings.Repeat("x", maxAuditExportLine)), nil); err == nil {
		t.Fatal("oversized line accepted")
	}
	if _, err := VerifyAuditExport(t.Context(), &brokenAuditReader{}, nil); err == nil {
		t.Fatal("reader failure accepted")
	}
	contents, _ := exportFixture(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyAuditExport(cancelled, bytes.NewReader(contents), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled verification=%v", err)
	}
}

type brokenAuditReader struct{}

func (*brokenAuditReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type shortAuditWriter struct{}

func (*shortAuditWriter) Write([]byte) (int, error) { return 0, nil }

func TestAuditExportPropagatesShortWrites(t *testing.T) {
	if err := FinishAuditExport(&shortAuditWriter{}, domain.EmptyAuditCheckpoint()); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write=%v", err)
	}
	if err := FinishAuditExport(io.Discard, domain.AuditCheckpoint{}); err == nil {
		t.Fatal("invalid terminal checkpoint exported")
	}
}

type cancellingAuditReader struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (reader *cancellingAuditReader) Read(buffer []byte) (int, error) {
	count, err := reader.Reader.Read(buffer)
	if err == io.EOF {
		reader.cancel()
	}
	return count, err
}

func TestAuditExportCancellationAtEOFDoesNotIssueAReceipt(t *testing.T) {
	archive, _ := exportFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := &cancellingAuditReader{Reader: bytes.NewReader(archive), cancel: cancel}
	if _, err := VerifyAuditExport(ctx, input, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation while checking EOF returned success: %v", err)
	}
}
