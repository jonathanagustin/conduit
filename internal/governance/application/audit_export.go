package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// AuditExportFormat is a Conduit-native JSONL archive of the stored ledger,
// including the exact payload bytes rather than a reconstructed projection.
const AuditExportFormat = "conduit.authorization-audit-export/v1"

const maxAuditExportLine = 16 * 1024 * 1024

type auditExportRecord struct {
	Sequence       uint64 `json:"sequence"`
	PreviousDigest string `json:"previousDigest"`
	EventID        string `json:"eventId"`
	ResourceRID    string `json:"resourceRid"`
	Origin         string `json:"origin"`
	Payload        []byte `json:"payload"`
	Digest         string `json:"digest"`
}

type auditExportLine struct {
	Format     string                  `json:"format"`
	Record     *auditExportRecord      `json:"record,omitempty"`
	Checkpoint *domain.AuditCheckpoint `json:"checkpoint,omitempty"`
}

func writeAuditExportLine(output io.Writer, line auditExportLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if len(encoded) >= maxAuditExportLine {
		return errors.New("authorization audit export record exceeds 16 MiB")
	}
	encoded = append(encoded, '\n')
	written, err := output.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}
	return err
}

func WriteAuditExportRecord(output io.Writer, record domain.AuditChainRecord) error {
	return writeAuditExportLine(output, auditExportLine{Format: AuditExportFormat, Record: &auditExportRecord{
		Sequence: record.Sequence, PreviousDigest: record.PreviousDigest, EventID: record.EventID,
		ResourceRID: record.ResourceRID, Origin: record.Origin, Payload: record.Payload, Digest: record.Digest,
	}})
}

// FinishAuditExport is written only after all stored rows and the head agree.
// A reader requires this terminal line, so a truncated prefix is not an archive.
func FinishAuditExport(output io.Writer, checkpoint domain.AuditCheckpoint) error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	return writeAuditExportLine(output, auditExportLine{Format: AuditExportFormat, Checkpoint: &checkpoint})
}

// VerifyAuditExport checks one record at a time and requires an exact terminal
// checkpoint and EOF. A nil trusted checkpoint proves internal consistency only.
func VerifyAuditExport(ctx context.Context, input io.Reader, trusted *domain.AuditCheckpoint) (domain.AuditCheckpoint, error) {
	if trusted != nil {
		if err := trusted.Validate(); err != nil {
			return domain.AuditCheckpoint{}, err
		}
	}
	current := domain.EmptyAuditCheckpoint()
	matched := trusted == nil || *trusted == current
	finished := false
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxAuditExportLine)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return domain.AuditCheckpoint{}, err
		}
		if finished {
			return domain.AuditCheckpoint{}, errors.New("data follows authorization audit export checkpoint")
		}
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		var line auditExportLine
		if err := decoder.Decode(&line); err != nil {
			return domain.AuditCheckpoint{}, errors.New("invalid authorization audit export line")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF || line.Format != AuditExportFormat || (line.Record == nil) == (line.Checkpoint == nil) {
			return domain.AuditCheckpoint{}, errors.New("invalid authorization audit export envelope")
		}
		if line.Checkpoint != nil {
			if *line.Checkpoint != current || !matched {
				return domain.AuditCheckpoint{}, errors.New("authorization audit export checkpoint mismatch")
			}
			finished = true
			continue
		}
		record := line.Record
		next, err := domain.VerifyAuditChainRecord(current, domain.AuditChainRecord{
			Sequence: record.Sequence, PreviousDigest: record.PreviousDigest, EventID: record.EventID,
			ResourceRID: record.ResourceRID, Origin: record.Origin, Payload: record.Payload, Digest: record.Digest,
		})
		if err != nil {
			return domain.AuditCheckpoint{}, err
		}
		current = next
		if trusted != nil && current.Sequence == trusted.Sequence {
			if current != *trusted {
				return domain.AuditCheckpoint{}, errors.New("authorization audit export does not match the independent checkpoint")
			}
			matched = true
		}
	}
	if err := scanner.Err(); err != nil {
		return domain.AuditCheckpoint{}, errors.New("authorization audit export cannot be read within its 16 MiB line bound")
	}
	if err := ctx.Err(); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if !finished {
		return domain.AuditCheckpoint{}, errors.New("authorization audit export is missing its terminal checkpoint")
	}
	return current, nil
}
