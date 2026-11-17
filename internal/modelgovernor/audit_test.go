package modelgovernor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// redact stands in for the repository-content redactor.
func redact(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("hunter2"), []byte("[REDACTED]"))
}

func transcriptConfig() modelgovernor.Config {
	return modelgovernor.Config{Transcripts: true, TranscriptRetention: time.Hour}
}

func withRedaction(deps *modelgovernor.Dependencies) { deps.Redact = redact }

func exchange(t *testing.T, fx fixture, ctx context.Context, prompt, response string) {
	t.Helper()
	call := assistantCall
	call.Prompt = []byte(prompt)
	if _, err := fx.governor.Do(ctx, call, nil, reply(response)); err != nil {
		t.Fatal(err)
	}
}

func TestEveryModelCallIsAuditedAndChainedWithinItsRun(t *testing.T) {
	fx := newFixture(modelgovernor.Config{})
	ctx := as("ada")
	modelgovernor.Annotate(ctx, "assistant-thread/t1")
	exchange(t, fx, ctx, "first prompt", "first reply")
	exchange(t, fx, ctx, "second prompt", "second reply")
	// A failed call is a call: it is audited with its outcome.
	_, _ = fx.governor.Do(ctx, assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
		return modelgovernor.Result{}, fmt.Errorf("%w: status 401", modelhttp.ErrUnauthorized)
	})
	other := as("grace")
	exchange(t, fx, other, "another run", "reply")

	events := fx.audit.Events()
	if len(events) != 4 {
		t.Fatalf("%d audit events for 4 model calls", len(events))
	}
	run := modelgovernor.RunFrom(ctx).ID()
	if !modelgovernor.ValidRunID(run) || run == modelgovernor.RunFrom(other).ID() {
		t.Fatalf("run ids %q and %q", run, modelgovernor.RunFrom(other).ID())
	}
	first, second, third, fourth := events[0], events[1], events[2], events[3]
	if first.RunID != run || first.Round != 1 || first.Owner != "ada" || first.Capability != "assistant" || first.Transport != "http" ||
		first.SessionID != "assistant-thread/t1" || first.ConfiguredModel != "m" || first.Outcome != "ok" || first.Transcript != "off" ||
		first.PromptSHA256 != modelgovernor.Digest([]byte("first prompt")) || first.ResponseSHA256 != modelgovernor.Digest([]byte("first reply")) {
		t.Fatalf("first event = %+v", first)
	}
	if first.PreviousChainSHA256 != "" || first.ChainSHA256 != modelgovernor.ChainDigest("", 1, first.PromptSHA256, first.ResponseSHA256) {
		t.Fatalf("the first call's chain = %q after %q", first.ChainSHA256, first.PreviousChainSHA256)
	}
	if second.Round != 2 || second.PreviousChainSHA256 != first.ChainSHA256 ||
		second.ChainSHA256 != modelgovernor.ChainDigest(first.ChainSHA256, 2, second.PromptSHA256, second.ResponseSHA256) {
		t.Fatalf("the second call does not link to the first: %+v", second)
	}
	if third.Round != 3 || third.PreviousChainSHA256 != second.ChainSHA256 || third.Outcome != "auth_error" ||
		third.ResponseSHA256 != modelgovernor.Digest(nil) {
		t.Fatalf("the failed call's event = %+v", third)
	}
	if fourth.Round != 1 || fourth.PreviousChainSHA256 != "" || fourth.Owner != "grace" || fourth.SessionID != "" {
		t.Fatalf("another run's first call = %+v: a chain is per run", fourth)
	}
	for _, event := range events {
		if text := fmt.Sprintf("%+v", event); strings.Contains(text, "prompt\"") || strings.Contains(text, "first reply") || strings.Contains(text, "another run") {
			t.Fatalf("an audit event carries prompt or response text: %s", text)
		}
	}
}

func TestAFailedAuditWriteFailsTheCallAndDiscardsTheReply(t *testing.T) {
	fx := newFixture(transcriptConfig())
	ctx := as("ada")
	exchange(t, fx, ctx, "audited", "reply")
	fx.audit.fail = errors.New("audit store is down")
	result, err := fx.governor.Do(ctx, assistantCall, nil, reply("a reply nobody may read"))
	if !errors.Is(err, modelgovernor.ErrAuditUnavailable) || len(result.Response) != 0 {
		t.Fatalf("result = %q, err = %v: want the audit failure and no reply", result.Response, err)
	}
	rows := usageRows(t, fx, "ada")
	outcomes := map[string]int64{}
	for _, row := range rows {
		outcomes[row.Outcome] += row.Calls
	}
	if outcomes["audit_failed"] != 1 || outcomes["ok"] != 1 {
		t.Fatalf("usage outcomes = %v", outcomes)
	}
	// The chain does not claim the failed write was committed. The next call
	// first retries that exact event, retaining the transcript's coordinate.
	if rounds := modelgovernor.RunFrom(ctx).Rounds(); rounds != 1 {
		t.Fatalf("%d rounds acknowledged during the outage, want 1", rounds)
	}
	fx.audit.fail = nil
	exchange(t, fx, ctx, "after the outage", "reply")
	events := fx.audit.Events()
	if len(events) != 3 || events[1].Round != 2 || events[1].PreviousChainSHA256 != events[0].ChainSHA256 ||
		events[2].Round != 3 || events[2].PreviousChainSHA256 != events[1].ChainSHA256 {
		t.Fatalf("events after an audit outage = %+v", events)
	}
	export := exportOf(t, fx, modelgovernor.RunFrom(ctx).ID(), nil)
	if !export.Verified || !export.Complete || len(export.Exchanges) != 3 ||
		export.Exchanges[1].Response != "a reply nobody may read" || export.Exchanges[2].Prompt != "after the outage" {
		t.Fatalf("recovered transcript export = %+v", export)
	}
}

func TestAHubGovernorWithNoAuditorRefusesTheCall(t *testing.T) {
	fx := newFixture(modelgovernor.Config{AuditRequired: true}, func(deps *modelgovernor.Dependencies) { deps.Audit = nil })
	called := false
	_, err := fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		called = true
		return modelgovernor.Result{}, nil
	})
	if !errors.Is(err, modelgovernor.ErrAuditUnavailable) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestAlreadyInvokedConcurrentCallsReserveDistinctRoundsAcrossAnAuditFailure(t *testing.T) {
	fx := newFixture(transcriptConfig())
	ctx, cancel := context.WithCancel(as("ada"))
	t.Cleanup(cancel)
	start := func(prompt string) (chan struct{}, <-chan error) {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			call := assistantCall
			call.Prompt = []byte(prompt)
			_, err := fx.governor.Do(ctx, call, nil, func(ctx context.Context) (modelgovernor.Result, error) {
				close(entered)
				select {
				case <-release:
					return modelgovernor.Result{Response: []byte("reply " + prompt)}, nil
				case <-ctx.Done():
					return modelgovernor.Result{}, ctx.Err()
				}
			})
			done <- err
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("a concurrent call did not invoke")
		}
		return release, done
	}
	firstRelease, firstDone := start("first")
	secondRelease, secondDone := start("second")
	fx.audit.fail = errors.New("injected audit outage")
	close(firstRelease)
	if err := <-firstDone; !errors.Is(err, modelgovernor.ErrAuditUnavailable) {
		t.Fatalf("first call returned %v", err)
	}
	fx.audit.fail = nil
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatalf("already-invoked second call could not recover the first audit: %v", err)
	}
	export := exportOf(t, fx, modelgovernor.RunFrom(ctx).ID(), nil)
	if !export.Verified || !export.Complete || len(export.Exchanges) != 2 ||
		export.Exchanges[0].Prompt != "first" || export.Exchanges[1].Prompt != "second" {
		t.Fatalf("concurrent recovery reused transcript coordinates: %+v", export)
	}
}

func TestTranscriptsAreOffByDefaultAndWriteNothing(t *testing.T) {
	fx := newFixture(modelgovernor.Config{})
	ctx := as("ada")
	exchange(t, fx, ctx, "a prompt with hunter2 in it", "reply")
	records, err := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(ctx).ID(), fx.clock.Now())
	if err != nil || len(records) != 0 {
		t.Fatalf("with transcripts off the store holds %d records", len(records))
	}
	runs, _ := fx.transcripts.Runs(context.Background(), modelgovernor.RunFilter{}, fx.clock.Now())
	if len(runs) != 0 || fx.audit.Events()[0].Transcript != "off" {
		t.Fatalf("runs = %+v, audit says %q", runs, fx.audit.Events()[0].Transcript)
	}
}

func TestATranscriptStoresRedactedBytesWithBothDigests(t *testing.T) {
	fx := newFixture(transcriptConfig(), withRedaction)
	ctx := as("ada")
	modelgovernor.Annotate(ctx, "planning-session/s1")
	exchange(t, fx, ctx, "password is hunter2", "a clean reply")
	records, _ := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(ctx).ID(), fx.clock.Now())
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	record, event := records[0], fx.audit.Events()[0]
	if string(record.Prompt) != "password is [REDACTED]" || strings.Contains(string(record.Prompt), "hunter2") {
		t.Fatalf("the stored prompt is %q", record.Prompt)
	}
	sent := modelgovernor.Digest([]byte("password is hunter2"))
	if record.SentPromptSHA256 != sent || event.PromptSHA256 != sent {
		t.Fatalf("the sent digest is of the bytes that were sent: record %s, audit %s, want %s", record.SentPromptSHA256, event.PromptSHA256, sent)
	}
	if record.StoredPromptSHA256 != modelgovernor.Digest(record.Prompt) || record.StoredPromptSHA256 == sent ||
		event.StoredPromptSHA256 != record.StoredPromptSHA256 {
		t.Fatalf("stored digest: record %s, audit %s", record.StoredPromptSHA256, event.StoredPromptSHA256)
	}
	// The response was not changed by redaction: one digest serves for both.
	if record.StoredResponseSHA256 != record.ReceivedResponseSHA256 || event.StoredResponseSHA256 != "" || event.Transcript != "stored" {
		t.Fatalf("an unredacted side: record %+v, audit stored digest %q", record, event.StoredResponseSHA256)
	}
	if record.Owner != "ada" || record.SessionID != "planning-session/s1" || record.Round != 1 ||
		!record.ExpiresAt.Equal(record.CreatedAt.Add(time.Hour)) {
		t.Fatalf("record = %+v", record)
	}
}

func TestTranscriptSizeCapsOmitTheRecordAndSayso(t *testing.T) {
	config := transcriptConfig()
	config.TranscriptRecordBytes, config.TranscriptRunBytes = 20, 30
	fx := newFixture(config)
	ctx := as("ada")
	exchange(t, fx, ctx, "0123456789", "0123456789")  // 20: kept
	exchange(t, fx, ctx, "0123456789", "0123456789-") // 21: over the record cap
	exchange(t, fx, ctx, "0123456789", "0")           // 11: would take the run to 31
	exchange(t, fx, ctx, "01234", "01234")            // 10: fits, run is 30
	events := fx.audit.Events()
	got := []string{events[0].Transcript, events[1].Transcript, events[2].Transcript, events[3].Transcript}
	if fmt.Sprint(got) != "[stored omitted_record_cap omitted_run_cap stored]" {
		t.Fatalf("transcript outcomes = %v", got)
	}
	records, _ := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(ctx).ID(), fx.clock.Now())
	if len(records) != 2 || records[0].Round != 1 || records[1].Round != 4 {
		t.Fatalf("stored rounds = %+v", records)
	}
}

func TestTheSweeperDeletesOnlyExpiredTranscriptsOnItsTimer(t *testing.T) {
	fx := newFixture(transcriptConfig())
	old := as("ada")
	exchange(t, fx, old, "old", "old")
	fx.clock.Advance(40 * time.Minute)
	fresh := as("ada")
	exchange(t, fx, fresh, "fresh", "fresh")

	ctx, cancel := context.WithCancel(context.Background())
	swept := make(chan int64, 8)
	done := make(chan struct{})
	go func() {
		modelgovernor.Sweeper{Store: fx.transcripts, Clock: fx.clock, Interval: 30 * time.Minute, Swept: func(n int64) { swept <- n }}.Run(ctx)
		close(done)
	}()
	if n := <-swept; n != 0 {
		t.Fatalf("the first pass deleted %d records; nothing has expired", n)
	}
	// 40m + 30m: the first record's hour is up, the second's is not.
	eventually(t, "the sweeper to wait on its timer", func() bool { return len(fx.clock.Delays()) >= 3 })
	fx.clock.Advance(30 * time.Minute)
	if n := <-swept; n != 1 {
		t.Fatalf("the second pass deleted %d records, want 1", n)
	}
	// Read as of the start, so the read's own expiry filter is not what hides it.
	start := fx.clock.Now().Add(-70 * time.Minute)
	if records, _ := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(old).ID(), start); len(records) != 0 {
		t.Fatal("the expired transcript is still stored")
	}
	if records, _ := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(fresh).ID(), start); len(records) != 1 {
		t.Fatal("the sweep deleted a transcript that had not expired")
	}
	cancel()
	<-done
}

func exportOf(t *testing.T, fx fixture, run string, tamper func([]modelgovernor.CallAudit, []modelgovernor.TranscriptRecord)) modelgovernor.Export {
	t.Helper()
	var events []modelgovernor.CallAudit
	for _, event := range fx.audit.Events() {
		if event.RunID == run {
			events = append(events, event)
		}
	}
	records, err := fx.transcripts.Run(context.Background(), run, fx.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if tamper != nil {
		tamper(events, records)
	}
	export, err := modelgovernor.VerifyRun(run, events, records)
	if err != nil {
		t.Fatal(err)
	}
	return export
}

func TestARedactedResponseIsVouchedForByTheAuditEventToo(t *testing.T) {
	fx := newFixture(transcriptConfig(), withRedaction)
	ctx := as("ada")
	exchange(t, fx, ctx, "a clean prompt", "password is hunter2")
	records, _ := fx.transcripts.Run(context.Background(), modelgovernor.RunFrom(ctx).ID(), fx.clock.Now())
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	record, event := records[0], fx.audit.Events()[0]
	if string(record.Response) != "password is [REDACTED]" || event.ResponseSHA256 != modelgovernor.Digest([]byte("password is hunter2")) ||
		event.StoredResponseSHA256 != modelgovernor.Digest(record.Response) || event.StoredPromptSHA256 != "" {
		t.Fatalf("record %q, audit received %s stored %q / prompt stored %q", record.Response, event.ResponseSHA256, event.StoredResponseSHA256, event.StoredPromptSHA256)
	}
}

func TestAnExportVerifiesAgainstTheAuditEventsAndDetectsTampering(t *testing.T) {
	fx := newFixture(transcriptConfig(), withRedaction)
	ctx := as("ada")
	exchange(t, fx, ctx, "round one, secret hunter2", "reply one")
	exchange(t, fx, ctx, "round two", "reply two")
	run := modelgovernor.RunFrom(ctx).ID()

	clean := exportOf(t, fx, run, nil)
	if !clean.Verified || !clean.Complete || len(clean.Mismatches) != 0 || len(clean.Exchanges) != 2 || clean.Owner != "ada" {
		t.Fatalf("a clean run exported as %+v", clean)
	}
	one, two := clean.Exchanges[0], clean.Exchanges[1]
	if one.PromptFidelity != "redacted" || one.ResponseFidelity != "exact" || two.PromptFidelity != "exact" ||
		one.Prompt != "round one, secret [REDACTED]" || two.Response != "reply two" || one.AuditEventID == "" ||
		one.SentPromptSHA256 != modelgovernor.Digest([]byte("round one, secret hunter2")) || one.StoredPromptSHA256 == one.SentPromptSHA256 {
		t.Fatalf("exchanges = %+v", clean.Exchanges)
	}

	cases := map[string]func([]modelgovernor.CallAudit, []modelgovernor.TranscriptRecord){
		"an exact record's text was edited": func(_ []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[1].Response = []byte("reply 2")
		},
		"an exact record's text and stored digest were both edited": func(_ []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[1].Response = []byte("reply 2")
			records[1].StoredResponseSHA256 = modelgovernor.Digest(records[1].Response)
		},
		"a redacted record's text and stored digest were both edited": func(_ []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[0].Prompt = []byte("round one, nothing to see")
			records[0].StoredPromptSHA256 = modelgovernor.Digest(records[0].Prompt)
		},
		"a record's sent digest was edited": func(_ []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[0].SentPromptSHA256 = modelgovernor.Digest([]byte("something else"))
		},
		"an audit event's digest was edited": func(events []modelgovernor.CallAudit, _ []modelgovernor.TranscriptRecord) {
			events[0].ResponseSHA256 = modelgovernor.Digest([]byte("forged"))
		},
		"an audit event was removed from the middle": func(events []modelgovernor.CallAudit, _ []modelgovernor.TranscriptRecord) {
			events[0] = events[1]
			events[1].Round = 99
		},
		// Record and event agree with each other; only the chain digest, which
		// was computed over the original prompt, gives the edit away.
		"an event and its record were rewritten together": func(events []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[1].Prompt = []byte("round 2")
			forged := modelgovernor.Digest(records[1].Prompt)
			records[1].SentPromptSHA256, records[1].StoredPromptSHA256, events[1].PromptSHA256 = forged, forged, forged
		},
		// The event's own chain digest recomputes; it no longer links back.
		"an event was re-chained from nothing": func(events []modelgovernor.CallAudit, _ []modelgovernor.TranscriptRecord) {
			events[1].PreviousChainSHA256 = ""
			events[1].ChainSHA256 = modelgovernor.ChainDigest("", events[1].Round, events[1].PromptSHA256, events[1].ResponseSHA256)
		},
		"a record was moved to another round": func(_ []modelgovernor.CallAudit, records []modelgovernor.TranscriptRecord) {
			records[1].Round = 7
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			export := exportOf(t, fx, run, tamper)
			if export.Verified || len(export.Mismatches) == 0 {
				t.Fatalf("a tampered run verified: %+v", export)
			}
		})
	}
}

func TestAnExportSaysWhatIsMissingRatherThanCallingItVerifiedAndComplete(t *testing.T) {
	fx := newFixture(transcriptConfig())
	ctx := as("ada")
	exchange(t, fx, ctx, "p1", "r1")
	exchange(t, fx, ctx, "p2", "r2")
	run := modelgovernor.RunFrom(ctx).ID()
	fx.clock.Advance(2 * time.Hour) // both expired
	export := exportOf(t, fx, run, nil)
	if !export.Verified || export.Complete || len(export.Missing) != 2 || export.Exchanges[0].Present ||
		export.Exchanges[0].Prompt != "" || !strings.Contains(export.Missing[0], "expired") {
		t.Fatalf("an expired run exported as %+v", export)
	}
	if _, err := modelgovernor.VerifyRun("airun_none", nil, nil); !errors.Is(err, modelgovernor.ErrRunNotFound) {
		t.Fatalf("an unknown run returned %v", err)
	}
}
