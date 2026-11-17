package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
)

type modelAuditIdentityClock struct{ at time.Time }

func (clock modelAuditIdentityClock) Now() time.Time                 { return clock.at }
func (modelAuditIdentityClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

func TestModelAuditDistinctRoundsAtOneInstantRemainDistinctInMountedExport(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	transcripts := memory.NewTranscripts()
	governor := modelgovernor.New(modelgovernor.Config{
		AuditRequired: true, Transcripts: true, TranscriptRetention: time.Hour,
	}, modelgovernor.Dependencies{
		Audit: modelCallAudit{repository: repository}, Transcripts: transcripts,
		Clock: modelAuditIdentityClock{at: time.Now().UTC()},
	})
	ctx := modelgovernor.WithScope(context.Background(), modelgovernor.Scope{Owner: func() string { return "ada" }})
	for _, prompt := range []string{"first", "second"} {
		_, err := governor.Do(ctx, modelgovernor.Call{
			Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP, Prompt: []byte(prompt),
		}, nil, func(context.Context) (modelgovernor.Result, error) {
			return modelgovernor.Result{Response: []byte("reply " + prompt)}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	handler := mountModelGovernance(http.NotFoundHandler(), repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, governor, ModelGovernance{Transcripts: transcripts})
	runID := modelgovernor.RunFrom(ctx).ID()
	response := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/transcripts/"+runID, "")
	var export modelgovernor.Export
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &export) != nil {
		t.Fatalf("mounted export returned %d %s", response.Code, response.Body.String())
	}
	events := auditEvents(t, repository, modelgovernor.ActionModelCall)
	if len(events) != 2 || !export.Verified || !export.Complete || len(export.Exchanges) != 2 {
		t.Fatalf("two distinct same-instant rounds produced %d audit events and mounted export %+v", len(events), export)
	}
}

type lostModelAuditAcknowledgement struct {
	controlplane.Repository
	attempts []controlplane.AuthorizationAuditEvent
}

func (repository *lostModelAuditAcknowledgement) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	stored, err := repository.Repository.AppendAuthorizationAudit(ctx, event)
	if event.Action == modelgovernor.ActionModelCall && event.Context["round"] == 2 {
		repository.attempts = append(repository.attempts, event)
		if err == nil && len(repository.attempts) == 1 {
			return controlplane.AuthorizationAuditEvent{}, errors.New("injected lost commit acknowledgement")
		}
	}
	return stored, err
}

func TestModelAuditRetriesKeepIdentityWhileDistinctSameTimeRoundsRemainDistinct(t *testing.T) {
	repository := &lostModelAuditAcknowledgement{Repository: controlplane.NewMemoryRepository()}
	transcripts := memory.NewTranscripts()
	governor := modelgovernor.New(modelgovernor.Config{
		AuditRequired: true, Transcripts: true, TranscriptRetention: time.Hour,
	}, modelgovernor.Dependencies{
		Audit: modelCallAudit{repository: repository}, Transcripts: transcripts,
		Clock: modelAuditIdentityClock{at: time.Now().UTC()},
	})
	ctx := modelgovernor.WithScope(context.Background(), modelgovernor.Scope{Owner: func() string { return "ada" }})
	for index, prompt := range []string{"first", "second", "third"} {
		result, err := governor.Do(ctx, modelgovernor.Call{
			Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP, Prompt: []byte(prompt),
		}, nil, func(context.Context) (modelgovernor.Result, error) {
			return modelgovernor.Result{Response: []byte("reply " + prompt)}, nil
		})
		if index == 1 {
			if !errors.Is(err, modelgovernor.ErrAuditUnavailable) || len(result.Response) != 0 {
				t.Fatalf("unacknowledged exchange returned %+v, %v", result, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if len(repository.attempts) != 2 || !reflect.DeepEqual(repository.attempts[0], repository.attempts[1]) {
		t.Fatalf("lost acknowledgement was retried with different event evidence: %+v", repository.attempts)
	}
	events := auditEvents(t, repository, modelgovernor.ActionModelCall)
	identities := map[string]bool{}
	for _, event := range events {
		identities[event.ID] = true
	}
	handler := mountModelGovernance(http.NotFoundHandler(), repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, governor, ModelGovernance{Transcripts: transcripts})
	runID := modelgovernor.RunFrom(ctx).ID()
	response := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/transcripts/"+runID, "")
	var export modelgovernor.Export
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &export) != nil {
		t.Fatalf("mounted export returned %d %s", response.Code, response.Body.String())
	}
	if len(events) != 3 || len(identities) != 3 || !export.Verified || !export.Complete || len(export.Exchanges) != 3 {
		t.Fatalf("same-time recovered run produced %d events, %d identities, export %+v", len(events), len(identities), export)
	}
	for index, prompt := range []string{"first", "second", "third"} {
		exchange := export.Exchanges[index]
		if exchange.Round != index+1 || exchange.Prompt != prompt || exchange.Response != "reply "+prompt ||
			exchange.SentPromptSHA256 != modelgovernor.Digest([]byte(prompt)) ||
			exchange.ReceivedResponseSHA256 != modelgovernor.Digest([]byte("reply "+prompt)) {
			t.Fatalf("exchange %d is misbound: %+v", index+1, exchange)
		}
	}
}
