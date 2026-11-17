package auditpostgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/governance/application"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

func exportSnapshot(t *testing.T, database *sql.DB, trusted *domain.AuditCheckpoint, output io.Writer) (domain.AuditCheckpoint, error) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	tip, err := New(transaction).Export(t.Context(), trusted, output)
	if err != nil {
		return domain.AuditCheckpoint{}, err
	}
	return tip, transaction.Commit()
}

// This fixture deliberately stores valid JSON with a different byte encoding
// from json.Marshal. Its hash is computed independently so reserializing the
// event while exporting cannot accidentally satisfy the exact-byte assertion.
func appendNoncanonicalExportFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	previous := domain.AuditCheckpoint{Version: domain.AuditChainVersion}
	if err := transaction.QueryRowContext(t.Context(), "SELECT sequence, digest FROM conduit_authorization_audit_head WHERE singleton=TRUE FOR UPDATE").Scan(&previous.Sequence, &previous.Digest); err != nil {
		t.Fatal(err)
	}
	record, err := domain.NewAuditChainRecord(previous, eventForTest("noncanonical"), domain.AuditOriginLive)
	if err != nil {
		t.Fatal(err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, record.Payload, "", "  "); err != nil {
		t.Fatal(err)
	}
	record.Payload = append(indented.Bytes(), '\n')
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("conduit.authorization-audit-chain/v1"))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], record.Sequence)
	_, _ = hasher.Write(length[:])
	for _, value := range [][]byte{[]byte(record.PreviousDigest), []byte(record.EventID), []byte(record.ResourceRID), []byte(record.Origin), record.Payload} {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(value)
	}
	record.Digest = hex.EncodeToString(hasher.Sum(nil))
	if _, err := domain.VerifyAuditChainRecord(previous, record); err != nil {
		t.Fatalf("noncanonical fixture is not a valid stored record: %v", err)
	}
	if _, err := transaction.ExecContext(t.Context(), `INSERT INTO conduit_authorization_audit_events
 (sequence, previous_digest, event_id, resource_rid, origin, payload, digest) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		record.Sequence, record.PreviousDigest, record.EventID, record.ResourceRID, record.Origin, record.Payload, record.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_head SET sequence=$1, digest=$2 WHERE singleton=TRUE", record.Sequence, record.Digest); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditExportRetainsEveryStoredFieldAndNoncanonicalPayload(t *testing.T) {
	database, _ := databaseForTest(t)
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if err := New(transaction).ImportLegacy(t.Context(), []domain.AuthorizationAuditEvent{eventForTest("imported")}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	trusted, err := verify(t, database, nil)
	if err != nil {
		t.Fatal(err)
	}
	appendEvent(t, database, eventForTest("live"))
	appendNoncanonicalExportFixture(t, database)
	var output bytes.Buffer
	tip, err := exportSnapshot(t, database, &trusted, &output)
	if err != nil || tip.Sequence != 3 {
		t.Fatalf("export tip=%#v, error=%v", tip, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	rows, err := database.QueryContext(t.Context(), `SELECT sequence, previous_digest, event_id, resource_rid, origin, payload, digest
 FROM conduit_authorization_audit_events ORDER BY sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var stored domain.AuditChainRecord
		if err := rows.Scan(&stored.Sequence, &stored.PreviousDigest, &stored.EventID, &stored.ResourceRID, &stored.Origin, &stored.Payload, &stored.Digest); err != nil {
			t.Fatal(err)
		}
		var line struct {
			Format string                   `json:"format"`
			Record *domain.AuditChainRecord `json:"record"`
		}
		if err := decoder.Decode(&line); err != nil {
			t.Fatal(err)
		}
		if line.Format != "conduit.authorization-audit-export/v1" || line.Record == nil || !reflect.DeepEqual(*line.Record, stored) {
			t.Fatalf("export did not preserve stored record %d", stored.Sequence)
		}
		if stored.Sequence == 1 && stored.Origin != domain.AuditOriginLegacy {
			t.Fatal("fixture did not exercise an imported record")
		}
		if stored.EventID == "noncanonical" && !bytes.Contains(stored.Payload, []byte("\n  ")) {
			t.Fatal("fixture did not exercise noncanonical stored bytes")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("compared %d stored records", count)
	}
	var terminal struct {
		Format     string                 `json:"format"`
		Checkpoint domain.AuditCheckpoint `json:"checkpoint"`
	}
	if err := decoder.Decode(&terminal); err != nil || terminal.Format != "conduit.authorization-audit-export/v1" || terminal.Checkpoint != tip {
		t.Fatalf("terminal checkpoint=%#v, error=%v", terminal, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("data after terminal checkpoint: %v", err)
	}
	if verified, err := application.VerifyAuditExport(t.Context(), bytes.NewReader(output.Bytes()), &trusted); err != nil || verified != tip {
		t.Fatalf("original stored bytes do not verify offline: %#v, %v", verified, err)
	}
}

type failingExportWriter struct {
	bytes.Buffer
	writes int
	failAt int
	err    error
}

func (writer *failingExportWriter) Write(value []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, writer.err
	}
	return writer.Buffer.Write(value)
}

func TestAuditExportPropagatesWriterFailuresWithoutACompleteArchive(t *testing.T) {
	database, _ := databaseForTest(t)
	appendEvent(t, database, eventForTest("first"))
	appendEvent(t, database, eventForTest("second"))
	want := errors.New("archive sink unavailable")
	// Each record is one write; the third write is the terminal checkpoint.
	for _, failAt := range []int{1, 2, 3} {
		writer := &failingExportWriter{failAt: failAt, err: want}
		if _, err := exportSnapshot(t, database, nil, writer); !errors.Is(err, want) {
			t.Fatalf("write %d did not propagate sink error: %v", failAt, err)
		}
		if _, err := application.VerifyAuditExport(t.Context(), bytes.NewReader(writer.Bytes()), nil); err == nil {
			t.Fatalf("write %d failure left a complete archive", failAt)
		}
	}
	if _, err := exportSnapshot(t, database, nil, nil); err == nil {
		t.Fatal("nil export writer accepted")
	}
}

func TestAuditExportCorruptHeadWithholdsTerminalCheckpoint(t *testing.T) {
	database, _ := databaseForTest(t)
	appendEvent(t, database, eventForTest("first"))
	if _, err := database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_head SET digest=$1 WHERE singleton=TRUE", domain.EmptyAuditCheckpoint().Digest); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := exportSnapshot(t, database, nil, &output); err == nil {
		t.Fatal("corrupt stored head exported successfully")
	}
	// Checking both actual output and offline refusal constrains the contract
	// even though records may have been staged before the final head check.
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	records := 0
	for {
		var line map[string]json.RawMessage
		if err := decoder.Decode(&line); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if _, exists := line["checkpoint"]; exists {
			t.Fatal("corrupt head emitted a terminal checkpoint")
		}
		records++
	}
	if records != 1 {
		t.Fatalf("fixture did not reach the final stored-head check: records=%d", records)
	}
	if _, err := application.VerifyAuditExport(context.Background(), bytes.NewReader(output.Bytes()), nil); err == nil {
		t.Fatal("corrupt-head output verified as a complete archive")
	}
}
