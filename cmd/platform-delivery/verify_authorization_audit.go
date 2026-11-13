package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

const verifyAuthorizationAuditCommand = "verify-authorization-audit"

type authorizationAuditReceipt struct {
	domain.AuditCheckpoint
	Verification string `json:"verification"`
}

func readAuditCheckpoint(path string) (domain.AuditCheckpoint, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.AuditCheckpoint{}, errors.New("checkpoint file cannot be read")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(contents) > 64*1024 {
		return domain.AuditCheckpoint{}, errors.New("checkpoint file exceeds its readable 64 KiB bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var receipt authorizationAuditReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return domain.AuditCheckpoint{}, errors.New("checkpoint file must contain a valid checkpoint receipt")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return domain.AuditCheckpoint{}, errors.New("checkpoint file must contain exactly one receipt")
	}
	if err := receipt.AuditCheckpoint.Validate(); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if receipt.Sequence == 0 {
		return domain.AuditCheckpoint{}, errors.New("trusted checkpoint must contain at least one audit event")
	}
	return receipt.AuditCheckpoint, nil
}

// This command never constructs a repository or runs migrations. Both chain
// and legacy-state inspection use the same read-only repeatable snapshot.
func verifyAuthorizationAudit(ctx context.Context, getenv func(string) string, args []string, output io.Writer) error {
	if getenv == nil || output == nil {
		return errors.New("audit verification requires configuration and an output writer")
	}
	flags := flag.NewFlagSet(verifyAuthorizationAuditCommand, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var checkpointFile string
	var bootstrap bool
	var timeout time.Duration
	flags.StringVar(&checkpointFile, "checkpoint-file", "", "independently retained checkpoint receipt")
	flags.BoolVar(&bootstrap, "bootstrap-checkpoint", false, "check internal consistency and create an initial checkpoint")
	flags.DurationVar(&timeout, "timeout", 2*time.Minute, "verification deadline")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || timeout <= 0 || bootstrap == (checkpointFile != "") {
		return errors.New("choose either --checkpoint-file or --bootstrap-checkpoint and a positive timeout")
	}
	var expected *domain.AuditCheckpoint
	if !bootstrap {
		checkpoint, err := readAuditCheckpoint(checkpointFile)
		if err != nil {
			return err
		}
		expected = &checkpoint
	}
	dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
	if err != nil {
		return errors.New("POSTGRES_DSN_FILE must name a readable single-line database credential")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("open audit verification database")
	}
	defer database.Close()
	if err := database.PingContext(bounded); err != nil {
		return errors.New("connect to audit verification database")
	}
	transaction, err := database.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return errors.New("begin read-only audit verification snapshot")
	}
	defer transaction.Rollback()
	var pending bool
	if err := transaction.QueryRowContext(bounded, `SELECT EXISTS (
 SELECT 1 FROM conduit_repository_state WHERE singleton = TRUE AND
 COALESCE(state->'authorizationAudit','{}'::jsonb) NOT IN ('{}'::jsonb,'null'::jsonb)
)`).Scan(&pending); err != nil {
		return errors.New("read legacy audit adoption status")
	}
	if pending {
		return errors.New("unchained legacy audit records remain; complete the Hub audit adoption before verifying")
	}
	tip, err := auditpostgres.New(transaction).Verify(bounded, expected)
	if err != nil {
		return errors.New("authorization audit chain verification failed")
	}
	if err := transaction.Commit(); err != nil {
		return errors.New("complete audit verification snapshot")
	}
	mode := "CHECKPOINT_PREFIX_MATCHED"
	if bootstrap {
		mode = "BOOTSTRAP_INTERNAL_CONSISTENCY_ONLY"
	}
	return json.NewEncoder(output).Encode(authorizationAuditReceipt{AuditCheckpoint: tip, Verification: mode})
}
