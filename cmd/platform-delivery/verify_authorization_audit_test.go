package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	"github.com/jonathanagustin/conduit/internal/governance/application"
	governancedomain "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func checkpointFile(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuditVerificationRequiresAnExplicitTrustMode(t *testing.T) {
	for _, args := range [][]string{nil, {"--bootstrap-checkpoint", "--checkpoint-file=x"}, {"--bootstrap-checkpoint", "extra"}, {"--bootstrap-checkpoint", "--timeout=0s"}, {"--unknown"}} {
		var output bytes.Buffer
		err := verifyAuthorizationAudit(t.Context(), func(string) string { return "" }, args, &output)
		if err == nil || !strings.Contains(err.Error(), "choose either") || output.Len() != 0 {
			t.Fatalf("arguments %v: %v, output %q", args, err, output.String())
		}
	}
}

func TestAuditCheckpointParsingIsStrictAndBounded(t *testing.T) {
	valid := authorizationAuditReceipt{AuditCheckpoint: governancedomain.AuditCheckpoint{Version: governancedomain.AuditChainVersion, Sequence: 1, Digest: strings.Repeat("a", 64)}, Verification: "BOOTSTRAP_INTERNAL_CONSISTENCY_ONLY"}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := readAuditCheckpoint(checkpointFile(t, encoded)); err != nil || parsed != valid.AuditCheckpoint {
		t.Fatalf("valid checkpoint: %#v, %v", parsed, err)
	}
	genesis, err := json.Marshal(governancedomain.EmptyAuditCheckpoint())
	if err != nil {
		t.Fatal(err)
	}
	for _, contents := range []string{string(genesis),
		"{}", string(encoded) + "{}", strings.TrimSuffix(string(encoded), "}") + `,"unknown":true}`,
		strings.Replace(string(encoded), `"sequence":1`, `"sequence":0`, 1),
		strings.Replace(string(encoded), strings.Repeat("a", 64), "invalid", 1),
		string(encoded) + strings.Repeat(" ", 70*1024) + "{}",
	} {
		if _, err := readAuditCheckpoint(checkpointFile(t, []byte(contents))); err == nil {
			t.Fatal("invalid or oversized checkpoint accepted")
		}
	}
}

func TestAuditVerificationNeverEchoesTheDatabaseCredential(t *testing.T) {
	const secret = "credential-do-not-disclose"
	dsn := "postgres://operator:" + secret + "%zz@127.0.0.1/database"
	path := filepath.Join(t.TempDir(), "dsn-secret")
	if err := os.WriteFile(path, []byte(dsn), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := verifyAuthorizationAudit(t.Context(), func(string) string { return path }, []string{"--bootstrap-checkpoint", "--timeout=100ms"}, &output)
	if err == nil || output.Len() != 0 || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), dsn) || strings.Contains(err.Error(), path) {
		t.Fatalf("credential-safe failure was not preserved: %v", err)
	}
}

func commandAuditEvent(id string) governancedomain.AuthorizationAuditEvent {
	event := governancedomain.NewAuthorizationCheckEvent(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "operator", "read", "resource", governancedomain.DecisionAllow, "allowed", nil)
	event.ID = id
	return event
}

func appendCommandAudit(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := auditpostgres.New(transaction).AppendAuthorizationAudit(t.Context(), commandAuditEvent(id)); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAuthorizationAuditCommandChecksAnIndependentPrefixAndLegacyAdoption(t *testing.T) {
	database, dsn := hubDatabase(t)
	appendCommandAudit(t, database, "first")
	var output bytes.Buffer
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--bootstrap-checkpoint"}, &output); err != nil {
		t.Fatal(err)
	}
	var initial authorizationAuditReceipt
	if err := json.Unmarshal(output.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Sequence != 1 || initial.Verification != "BOOTSTRAP_INTERNAL_CONSISTENCY_ONLY" {
		t.Fatalf("bootstrap receipt=%#v", initial)
	}
	retained := checkpointFile(t, bytes.Clone(output.Bytes()))
	appendCommandAudit(t, database, "second")
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--checkpoint-file", retained}, &output); err != nil {
		t.Fatal(err)
	}
	var verified authorizationAuditReceipt
	if err := json.Unmarshal(output.Bytes(), &verified); err != nil {
		t.Fatal(err)
	}
	if verified.Sequence != 2 || verified.Verification != "CHECKPOINT_PREFIX_MATCHED" {
		t.Fatalf("verified receipt=%#v", verified)
	}
	forged := initial
	forged.Digest = strings.Repeat("b", 64)
	encoded, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--checkpoint-file", checkpointFile(t, encoded)}, &output); err == nil || output.Len() != 0 {
		t.Fatal("forged checkpoint passed or emitted a receipt")
	}
	legacy, err := json.Marshal(map[string]any{"legacy": commandAuditEvent("legacy")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), "UPDATE conduit_repository_state SET state=jsonb_set(state,'{authorizationAudit}',$1::jsonb)", legacy); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--bootstrap-checkpoint"}, &output); err == nil || output.Len() != 0 || !strings.Contains(err.Error(), "unchained legacy") {
		t.Fatalf("legacy adoption was overclaimed: %v", err)
	}
	repository, err := controlplane.NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--checkpoint-file", retained}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &verified); err != nil || verified.Sequence != 3 {
		t.Fatalf("legacy adoption receipt=%#v, %v", verified, err)
	}
}

func TestPostgresAuthorizationAuditCommandDoesNotRepairTheDatabase(t *testing.T) {
	database, dsn := hubDatabase(t)
	var before int64
	if err := database.QueryRowContext(t.Context(), "SELECT generation FROM conduit_repository_state WHERE singleton=TRUE").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), "DROP TABLE conduit_authorization_audit_events; DROP TABLE conduit_authorization_audit_head"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--bootstrap-checkpoint"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("missing audit tables were accepted")
	}
	var after int64
	var recreated sql.NullString
	if err := database.QueryRowContext(t.Context(), "SELECT generation, to_regclass('conduit_authorization_audit_head')::text FROM conduit_repository_state WHERE singleton=TRUE").Scan(&after, &recreated); err != nil {
		t.Fatal(err)
	}
	if before != after || recreated.Valid {
		t.Fatal("verification repaired or changed the database")
	}
}

func TestAuditArchiveVerificationUsesNoDatabaseAndRequiresCompleteEvidence(t *testing.T) {
	var archive bytes.Buffer
	record, err := governancedomain.NewAuditChainRecord(governancedomain.EmptyAuditCheckpoint(), commandAuditEvent("offline"), governancedomain.AuditOriginLegacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.WriteAuditExportRecord(&archive, record); err != nil {
		t.Fatal(err)
	}
	tip := governancedomain.AuditCheckpoint{Version: governancedomain.AuditChainVersion, Sequence: 1, Digest: record.Digest}
	if err := application.FinishAuditExport(&archive, tip); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "archive.jsonl")
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	noDatabase := func(string) string {
		t.Fatal("offline archive verification requested a database credential")
		return ""
	}
	var output bytes.Buffer
	if err := verifyAuthorizationAudit(t.Context(), noDatabase, []string{"--bootstrap-checkpoint", "--archive-file", path}, &output); err != nil {
		t.Fatal(err)
	}
	var receipt authorizationAuditReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.AuditCheckpoint != tip || receipt.Verification != "BOOTSTRAP_INTERNAL_CONSISTENCY_ONLY" {
		t.Fatalf("offline bootstrap=%#v, %v", receipt, err)
	}
	trusted := checkpointFile(t, bytes.Clone(output.Bytes()))
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), noDatabase, []string{"--checkpoint-file", trusted, "--archive-file", path}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.AuditCheckpoint != tip || receipt.Verification != "CHECKPOINT_PREFIX_MATCHED" {
		t.Fatalf("offline trusted=%#v, %v", receipt, err)
	}
	for name, args := range map[string][]string{
		"missing archive": {"--bootstrap-checkpoint", "--archive-file", filepath.Join(t.TempDir(), "secret-missing")},
		"directory":       {"--bootstrap-checkpoint", "--archive-file", t.TempDir()},
		"both directions": {"--bootstrap-checkpoint", "--archive-file", path, "--export-file", "other"},
	} {
		t.Run(name, func(t *testing.T) {
			output.Reset()
			if err := verifyAuthorizationAudit(t.Context(), noDatabase, args, &output); err == nil || output.Len() != 0 || strings.Contains(err.Error(), "secret-missing") {
				t.Fatalf("invalid archive arguments=%v, output=%q", err, output.String())
			}
		})
	}
	truncated := bytes.Split(archive.Bytes(), []byte{'\n'})[0]
	if err := os.WriteFile(path, truncated, 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), noDatabase, []string{"--checkpoint-file", trusted, "--archive-file", path}, &output); err == nil || output.Len() != 0 {
		t.Fatal("truncated archive emitted a trusted receipt")
	}
}

func TestPostgresAuthorizationAuditExportIsPrivateCompleteAndDoesNotOverwrite(t *testing.T) {
	database, dsn := hubDatabase(t)
	appendCommandAudit(t, database, "first")
	var initial bytes.Buffer
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--bootstrap-checkpoint"}, &initial); err != nil {
		t.Fatal(err)
	}
	trusted := checkpointFile(t, initial.Bytes())
	appendCommandAudit(t, database, "second")
	directory := t.TempDir()
	path := filepath.Join(directory, "audit.jsonl")
	args := []string{"--checkpoint-file", trusted, "--export-file", path}
	var output bytes.Buffer
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), args, &output); err != nil {
		t.Fatal(err)
	}
	var receipt authorizationAuditReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.Sequence != 2 || receipt.Verification != "CHECKPOINT_PREFIX_MATCHED" {
		t.Fatalf("export receipt=%#v, %v", receipt, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private archive mode: %v, %v", info, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := readAuditCheckpoint(trusted)
	if err != nil {
		t.Fatal(err)
	}
	if tip, err := application.VerifyAuditExport(t.Context(), bytes.NewReader(contents), &checkpoint); err != nil || tip != receipt.AuditCheckpoint {
		t.Fatalf("exported stored ledger=%#v, %v", tip, err)
	}
	output.Reset()
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), args, &output); err == nil || output.Len() != 0 {
		t.Fatal("existing archive overwritten or success receipt emitted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, after) {
		t.Fatal("existing archive bytes changed")
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 1 || files[0].Name() != "audit.jsonl" {
		t.Fatalf("staged audit payloads leaked: %v, %v", files, err)
	}
	// A wrong independent anchor must leave no published or staged archive.
	forged := receipt
	forged.Digest = strings.Repeat("a", 64)
	encoded, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	rejected := filepath.Join(directory, "rejected.jsonl")
	if err := verifyAuthorizationAudit(t.Context(), environment(t, dsn), []string{"--checkpoint-file", checkpointFile(t, encoded), "--export-file", rejected}, &output); err == nil || output.Len() != 0 {
		t.Fatal("forged anchor exported a ledger or receipt")
	}
	files, err = os.ReadDir(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("failed export leaked files: %v, %v", files, err)
	}
}
