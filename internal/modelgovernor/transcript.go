package modelgovernor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// What an audit event says about the call's transcript.
const (
	TranscriptOff              = "off"
	TranscriptStored           = "stored"
	TranscriptOmittedRecordCap = "omitted_record_cap"
	TranscriptOmittedRunCap    = "omitted_run_cap"
	TranscriptFailed           = "failed"
)

// ActionModelCall is the audit action of one model call. ResourcePrefix and
// the run id are the audited resource, so one run's events list together.
const (
	ActionModelCall        = "ai.model_call"
	ActionTranscriptExport = "ai.transcript_export"
	ResourcePrefix         = "ai-run/"
)

// CallAudit is the audit event for one model call. It holds digests and ids.
// It never holds prompt or response text.
type CallAudit struct {
	Owner      string
	Capability string
	Transport  string
	RunID      string
	SessionID  string
	// Round counts the run's model calls from 1.
	Round          int
	PromptSHA256   string
	ResponseSHA256 string
	// PreviousChainSHA256 is the ChainSHA256 of the run's previous call, or
	// empty on the first. ChainSHA256 is ChainDigest over this call.
	PreviousChainSHA256 string
	ChainSHA256         string
	ConfiguredModel     string
	ProviderModel       string
	Outcome             string
	Attempts            int
	At                  time.Time
	// Transcript is one of the Transcript* values.
	Transcript string
	// StoredPromptSHA256 and StoredResponseSHA256 are set only when a
	// transcript was stored and redaction changed that side's bytes.
	StoredPromptSHA256   string
	StoredResponseSHA256 string
	// EventID is the audit log's id for the event, when it is read back.
	EventID string
}

// Auditor writes the event. An error fails the model call.
type Auditor interface {
	RecordModelCall(ctx context.Context, event CallAudit) error
}

// TranscriptRecord is one stored exchange. Prompt and Response are the bytes
// after redaction. The Sent/Received digests are of the bytes that crossed
// the wire and are the ones the audit event holds; the Stored digests are of
// the bytes in this record. They are equal exactly when redaction changed
// nothing.
type TranscriptRecord struct {
	RunID                  string
	SessionID              string
	Owner                  string
	Capability             string
	Round                  int
	SentPromptSHA256       string
	ReceivedResponseSHA256 string
	StoredPromptSHA256     string
	StoredResponseSHA256   string
	Prompt                 []byte
	Response               []byte
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

// RunSummary is one run in the transcript store, without its text.
type RunSummary struct {
	RunID      string    `json:"runId"`
	SessionID  string    `json:"sessionId,omitempty"`
	Owner      string    `json:"owner"`
	Capability string    `json:"capability"`
	Rounds     int       `json:"rounds"`
	Bytes      int64     `json:"bytes"`
	FirstAt    time.Time `json:"firstAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// RunFilter narrows RunSummaries. Empty fields match everything.
type RunFilter struct {
	Owner, SessionID string
	Limit            int
}

// ErrTranscriptRunFull is a Put refused because the run's stored bytes would
// pass the cap.
var ErrTranscriptRunFull = errors.New("transcript run size cap reached")

// TranscriptStore keeps exchanges until they expire. A record past ExpiresAt
// is never returned, whether or not the sweeper has deleted it yet.
type TranscriptStore interface {
	// Put stores one record unless the run's bytes would pass runCapBytes.
	Put(ctx context.Context, record TranscriptRecord, runCapBytes int64) error
	// Run returns a run's unexpired records ordered by round.
	Run(ctx context.Context, runID string, now time.Time) ([]TranscriptRecord, error)
	Runs(ctx context.Context, filter RunFilter, now time.Time) ([]RunSummary, error)
	// DeleteExpired removes every record with ExpiresAt at or before now.
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// Sweeper deletes expired transcripts on a timer. The delete is one
// idempotent statement, so two replicas sweeping at once delete each record
// once between them and neither fails.
type Sweeper struct {
	Store    TranscriptStore
	Clock    Clock
	Interval time.Duration
	OnError  func(error)
	// Swept is told how many records each pass deleted. Tests read it.
	Swept func(int64)
}

// Run sweeps once immediately and then every Interval until ctx ends.
func (sweeper Sweeper) Run(ctx context.Context) {
	clock := sweeper.Clock
	if clock == nil {
		clock = systemClock{}
	}
	interval := sweeper.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	for {
		deleted, err := sweeper.Store.DeleteExpired(ctx, clock.Now())
		if err != nil && sweeper.OnError != nil && ctx.Err() == nil {
			sweeper.OnError(err)
		}
		if err == nil && sweeper.Swept != nil {
			sweeper.Swept(deleted)
		}
		select {
		case <-ctx.Done():
			return
		case <-clock.After(interval):
		}
	}
}

// Fidelity says what a stored side of an exchange is, relative to the wire.
const (
	// FidelityExact: the stored bytes hash to the digest the audit log holds
	// for what was sent or received.
	FidelityExact = "exact"
	// FidelityRedacted: redaction changed the bytes. They hash to the stored
	// digest, which the audit log also holds, and not to the sent digest.
	FidelityRedacted = "redacted"
)

// Exchange is one round of an export.
type Exchange struct {
	Round      int    `json:"round"`
	Capability string `json:"capability"`
	// AuditEventID is the audit event the round was checked against.
	AuditEventID string `json:"auditEventId,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	// Transcript is what the audit event says was kept.
	Transcript string `json:"transcript"`
	// Present is false when no unexpired record exists for the round.
	Present                bool   `json:"present"`
	Prompt                 string `json:"prompt,omitempty"`
	Response               string `json:"response,omitempty"`
	SentPromptSHA256       string `json:"sentPromptSha256"`
	ReceivedResponseSHA256 string `json:"receivedResponseSha256"`
	StoredPromptSHA256     string `json:"storedPromptSha256,omitempty"`
	StoredResponseSHA256   string `json:"storedResponseSha256,omitempty"`
	// PromptFidelity and ResponseFidelity are FidelityExact or
	// FidelityRedacted, for a present record.
	PromptFidelity   string `json:"promptFidelity,omitempty"`
	ResponseFidelity string `json:"responseFidelity,omitempty"`
	ChainSHA256      string `json:"chainSha256,omitempty"`
}

// Export is one run's exchanges and the result of checking them against the
// audit log.
type Export struct {
	RunID     string     `json:"runId"`
	SessionID string     `json:"sessionId,omitempty"`
	Owner     string     `json:"owner,omitempty"`
	Exchanges []Exchange `json:"exchanges"`
	// Verified is true when the audit chain recomputes and every present
	// record matches its audit event. It says nothing about rounds that are
	// absent: Complete does.
	Verified bool `json:"verified"`
	// Complete is true when every audited round has a present record.
	Complete   bool     `json:"complete"`
	Mismatches []string `json:"mismatches"`
	// Missing names audited rounds with no present record, and why when the
	// audit event says.
	Missing []string `json:"missing"`
}

// ErrRunNotFound is an export of a run with no audit event and no record.
var ErrRunNotFound = errors.New("no such AI run")

// VerifyRun builds the export of one run: its stored records in order, each
// checked against the audit events, and the audit events' hash chain
// recomputed. A mismatch is reported, not returned as an error.
func VerifyRun(runID string, events []CallAudit, records []TranscriptRecord) (Export, error) {
	if len(events) == 0 && len(records) == 0 {
		return Export{}, ErrRunNotFound
	}
	export := Export{RunID: runID, Exchanges: []Exchange{}, Mismatches: []string{}, Missing: []string{}}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Round < events[j].Round })
	sort.SliceStable(records, func(i, j int) bool { return records[i].Round < records[j].Round })
	byRound := map[int]TranscriptRecord{}
	for _, record := range records {
		if _, twice := byRound[record.Round]; twice {
			export.Mismatches = append(export.Mismatches, fmt.Sprintf("round %d: more than one stored record", record.Round))
		}
		byRound[record.Round] = record
	}
	mismatch := func(round int, format string, args ...any) {
		export.Mismatches = append(export.Mismatches, fmt.Sprintf("round %d: ", round)+fmt.Sprintf(format, args...))
	}
	previous := ""
	audited := map[int]bool{}
	for index, event := range events {
		if export.Owner == "" {
			export.Owner, export.SessionID = event.Owner, event.SessionID
		}
		if event.Round != index+1 {
			mismatch(event.Round, "audit events are not a contiguous sequence from 1 (position %d)", index+1)
		}
		if event.PreviousChainSHA256 != previous {
			mismatch(event.Round, "audit event does not link to the previous call's chain digest")
		}
		if want := ChainDigest(event.PreviousChainSHA256, event.Round, event.PromptSHA256, event.ResponseSHA256); event.ChainSHA256 != want {
			mismatch(event.Round, "audit event's chain digest does not recompute")
		}
		previous = event.ChainSHA256
		audited[event.Round] = true
		exchange := Exchange{
			Round: event.Round, Capability: event.Capability, AuditEventID: event.EventID, Outcome: event.Outcome,
			Transcript: event.Transcript, SentPromptSHA256: event.PromptSHA256, ReceivedResponseSHA256: event.ResponseSHA256,
			ChainSHA256: event.ChainSHA256,
		}
		record, present := byRound[event.Round]
		if !present {
			reason := "no stored record"
			switch event.Transcript {
			case TranscriptStored:
				reason = "the audit event says a transcript was stored; it has expired or been removed"
			case TranscriptOff:
				reason = "transcripts were off"
			case TranscriptOmittedRecordCap, TranscriptOmittedRunCap:
				reason = "over the size cap (" + event.Transcript + ")"
			case TranscriptFailed:
				reason = "the transcript store refused the write"
			}
			export.Missing = append(export.Missing, fmt.Sprintf("round %d: %s", event.Round, reason))
			export.Exchanges = append(export.Exchanges, exchange)
			continue
		}
		exchange.Present = true
		exchange.Prompt, exchange.Response = string(record.Prompt), string(record.Response)
		exchange.StoredPromptSHA256, exchange.StoredResponseSHA256 = record.StoredPromptSHA256, record.StoredResponseSHA256
		if record.SentPromptSHA256 != event.PromptSHA256 {
			mismatch(event.Round, "record's sent prompt digest is not the audited one")
		}
		if record.ReceivedResponseSHA256 != event.ResponseSHA256 {
			mismatch(event.Round, "record's received response digest is not the audited one")
		}
		exchange.PromptFidelity = verifySide(event.Round, "prompt", record.Prompt, record.StoredPromptSHA256, event.PromptSHA256, event.StoredPromptSHA256, mismatch)
		exchange.ResponseFidelity = verifySide(event.Round, "response", record.Response, record.StoredResponseSHA256, event.ResponseSHA256, event.StoredResponseSHA256, mismatch)
		export.Exchanges = append(export.Exchanges, exchange)
	}
	for _, record := range records {
		if !audited[record.Round] {
			mismatch(record.Round, "stored record has no audit event")
			if export.Owner == "" {
				export.Owner, export.SessionID = record.Owner, record.SessionID
			}
		}
	}
	export.Verified = len(export.Mismatches) == 0
	export.Complete = len(export.Missing) == 0
	return export, nil
}

// verifySide checks one side's stored bytes. With no redaction digest on the
// audit event the bytes must hash to the audited sent digest. With one they
// must hash to it instead: a redacted copy that the audit log vouches for.
func verifySide(
	round int, side string, stored []byte, recordedStoredSHA256, auditedSentSHA256, auditedStoredSHA256 string,
	mismatch func(int, string, ...any),
) string {
	actual := Digest(stored)
	if actual != recordedStoredSHA256 {
		mismatch(round, "stored %s bytes do not hash to the record's stored digest", side)
	}
	if auditedStoredSHA256 == "" {
		if actual != auditedSentSHA256 {
			mismatch(round, "stored %s bytes do not hash to the audited digest, and the audit event records no redaction", side)
		}
		return FidelityExact
	}
	if actual != auditedStoredSHA256 {
		mismatch(round, "redacted %s bytes do not hash to the audited stored digest", side)
	}
	return FidelityRedacted
}
