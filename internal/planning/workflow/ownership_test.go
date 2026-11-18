package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	temporalworkflow "go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type ownershipValue struct{ data any }

func (v ownershipValue) HasValue() bool { return true }
func (v ownershipValue) Get(out any) error {
	b, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type ownershipClient struct {
	client.Client
	owner        any
	missingValue bool
	queries      []string
}

func (c *ownershipClient) QueryWorkflow(_ context.Context, _ string, _ string, kind string, _ ...any) (converter.EncodedValue, error) {
	c.queries = append(c.queries, kind)
	if kind == QueryOwner {
		if c.missingValue {
			return nil, nil
		}
		return ownershipValue{c.owner}, nil
	}
	return ownershipValue{domain.Session{ID: "ps_private", Status: domain.StatusReady, Plan: &domain.Plan{Text: "synthetic private plan"}}}, nil
}
func TestPlanningReadsRequireVerifiedNonemptyOwner(t *testing.T) {
	for _, tc := range []struct {
		name, caller         string
		owner                any
		missingValue, wantOK bool
	}{
		{name: "missing owner", caller: "alice", owner: ""},
		{name: "missing caller", caller: "", owner: "alice"},
		{name: "whitespace owner", caller: "alice", owner: "  "},
		{name: "different owner", caller: "alice", owner: "bob"},
		{name: "unavailable owner value", caller: "alice", missingValue: true},
		{name: "malformed owner", caller: "alice", owner: 42},
		{name: "matching owner", caller: "alice", owner: "alice", wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &ownershipClient{owner: tc.owner, missingValue: tc.missingValue}
			session, err := NewClientService(backend, "", tc.caller, nil).Get(context.Background(), "ps_private")
			if tc.wantOK {
				if err != nil || session == nil || session.ID != "ps_private" || session.Plan == nil {
					t.Fatalf("owned read: %#v %v", session, err)
				}
				if len(backend.queries) != 2 || backend.queries[0] != QueryOwner || backend.queries[1] != QuerySession {
					t.Fatalf("owned queries: %#v", backend.queries)
				}
				return
			}
			if err == nil || session != nil {
				t.Fatalf("unverified caller received session payload: %#v %v", session, err)
			}
			for _, kind := range backend.queries {
				if kind == QuerySession {
					t.Fatal("session payload queried before ownership was established")
				}
			}
			if tc.caller == "" && len(backend.queries) != 0 {
				t.Fatal("missing caller reached the workflow backend")
			}
		})
	}
}

func TestWorkflowRejectsMissingIdentityBeforeTurnActivity(t *testing.T) {
	for _, tc := range []struct{ name, owner, caller string }{
		{"missing owner", "", "alice"}, {"missing caller", "alice", ""}, {"both missing", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterWorkflowWithOptions(SessionWorkflow, temporalworkflow.RegisterOptions{Name: WorkflowName})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context, TurnActivityInput) (*application.TurnResult, error) {
				calls++
				return &application.TurnResult{}, nil
			}, activity.RegisterOptions{Name: ActTurn})
			accepted := false
			var rejection error
			env.RegisterDelayedCallback(func() {
				env.UpdateWorkflow(UpdateTurn, "unverified-turn", callbackUpdateCallbacks{onAccept: func() { accepted = true }, onReject: func(err error) { rejection = err }}, TurnRequest{Actor: tc.caller, Message: "inspect"})
			}, time.Millisecond)
			// End the otherwise waiting workflow after the update's validation result.
			env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, 2*time.Millisecond)
			env.ExecuteWorkflow(WorkflowName, Input{SessionID: "ps_identity", Actor: tc.owner, RepoURL: "https://github.com/org/app", Ref: "main"})
			var failure *temporal.ApplicationError
			if accepted || rejection == nil || !errors.As(rejection, &failure) || failure.Type() != ErrTypeForbidden {
				t.Fatalf("update was not refused: accepted=%v rejection=%v activityCalls=%d", accepted, rejection, calls)
			}
			if calls != 0 {
				t.Fatalf("unverified update invoked %d model activities", calls)
			}
		})
	}
}
