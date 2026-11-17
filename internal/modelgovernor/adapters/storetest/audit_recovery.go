package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

type recoveryClock struct{}

func (recoveryClock) Now() time.Time                       { return epoch }
func (recoveryClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

// recoveryAudit models both a refused write and a committed write whose
// acknowledgement was lost. Retrying a coordinate must carry identical data.
type recoveryAudit struct {
	events          []modelgovernor.CallAudit
	attempts        map[int]int
	firstAttempt    map[int]modelgovernor.CallAudit
	fail            bool
	commitBeforeErr bool
}

func (audit *recoveryAudit) RecordModelCall(_ context.Context, event modelgovernor.CallAudit) error {
	audit.attempts[event.Round]++
	if prior, exists := audit.firstAttempt[event.Round]; exists {
		if !reflect.DeepEqual(prior, event) {
			return fmt.Errorf("round %d was retried with different evidence", event.Round)
		}
	} else {
		audit.firstAttempt[event.Round] = event
	}
	failed := audit.fail && event.Round == 2
	if !failed || audit.commitBeforeErr {
		if event.Round > len(audit.events) {
			audit.events = append(audit.events, event)
		}
	}
	if failed {
		return errors.New("injected audit acknowledgement failure")
	}
	return nil
}

// RunAuditRecovery verifies the governor's transcript recovery against each
// real store, including the PostgreSQL primary key on run_id and round.
func RunAuditRecovery(t *testing.T, open func(*testing.T) modelgovernor.TranscriptStore) {
	for _, commitBeforeErr := range []bool{false, true} {
		name := map[bool]string{false: "write refused", true: "commit acknowledgement lost"}[commitBeforeErr]
		t.Run(name, func(t *testing.T) {
			store := open(t)
			audit := &recoveryAudit{attempts: map[int]int{}, firstAttempt: map[int]modelgovernor.CallAudit{}, commitBeforeErr: commitBeforeErr}
			governor := modelgovernor.New(modelgovernor.Config{
				AuditRequired: true, Transcripts: true, TranscriptRetention: time.Hour,
			}, modelgovernor.Dependencies{
				Transcripts: store, Audit: audit, Clock: recoveryClock{},
				Redact: func(data []byte) []byte { return bytes.ReplaceAll(data, []byte("secret"), []byte("[REDACTED]")) },
			})
			ctx := modelgovernor.WithScope(context.Background(), modelgovernor.Scope{Owner: func() string { return "ada" }})
			invoked := 0
			call := func(prompt string) (modelgovernor.Result, error) {
				return governor.Do(ctx, modelgovernor.Call{
					Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP, Prompt: []byte(prompt),
				}, nil, func(context.Context) (modelgovernor.Result, error) {
					invoked++
					return modelgovernor.Result{Response: []byte("reply " + prompt)}, nil
				})
			}
			if _, err := call("before"); err != nil {
				t.Fatal(err)
			}
			audit.fail = true
			if result, err := call("during secret"); !errors.Is(err, modelgovernor.ErrAuditUnavailable) || len(result.Response) != 0 {
				t.Fatalf("unacknowledged call returned %+v, %v", result, err)
			}
			if rounds := modelgovernor.RunFrom(ctx).Rounds(); rounds != 1 {
				t.Fatalf("%d acknowledged rounds during the outage, want 1", rounds)
			}
			if _, err := call("must not invoke while audit is unavailable"); !errors.Is(err, modelgovernor.ErrAuditUnavailable) {
				t.Fatalf("retry during the outage returned %v", err)
			}
			if invoked != 2 {
				t.Fatalf("%d model invocations during the outage, want only the two original exchanges", invoked)
			}
			runID := modelgovernor.RunFrom(ctx).ID()
			records, err := store.Run(ctx, runID, epoch)
			if err != nil || len(records) != 2 {
				t.Fatalf("pending transcripts = %+v, err = %v, want 2 unique coordinates", records, err)
			}
			if !commitBeforeErr {
				export, err := modelgovernor.VerifyRun(runID, audit.events, records)
				if err != nil || export.Verified || len(export.Mismatches) != 1 {
					t.Fatalf("unacknowledged transcript was not distinguished: %+v, %v", export, err)
				}
			}
			audit.fail = false
			if _, err := call("after"); err != nil {
				t.Fatal(err)
			}
			records, err = store.Run(ctx, runID, epoch)
			if err != nil || len(records) != 3 || len(audit.events) != 3 || audit.attempts[2] != 3 {
				t.Fatalf("recovery: %d transcripts, %d events, %d attempts of pending event, err = %v", len(records), len(audit.events), audit.attempts[2], err)
			}
			export, err := modelgovernor.VerifyRun(runID, audit.events, records)
			if err != nil || !export.Verified || !export.Complete || len(export.Exchanges) != 3 {
				t.Fatalf("recovered export = %+v, err = %v", export, err)
			}
			middle, last := export.Exchanges[1], export.Exchanges[2]
			if middle.Prompt != "during [REDACTED]" || middle.Response != "reply during [REDACTED]" ||
				middle.PromptFidelity != modelgovernor.FidelityRedacted || middle.ResponseFidelity != modelgovernor.FidelityRedacted ||
				last.Prompt != "after" || last.Response != "reply after" || last.PromptFidelity != modelgovernor.FidelityExact {
				t.Fatalf("recovery changed or misbound transcript text: middle = %+v, last = %+v", middle, last)
			}
		})
	}
}
