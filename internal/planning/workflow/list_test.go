package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type indexValue struct{ value any }

func (v indexValue) HasValue() bool { return true }
func (v indexValue) Get(out any) error {
	b, err := json.Marshal(v.value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type indexQuery struct{ id, run, kind string }
type visibilityStub struct {
	client.Client
	pages    map[string]*workflowservice.ListWorkflowExecutionsResponse
	owners   map[string]string
	sessions map[string]domain.Session
	requests []*workflowservice.ListWorkflowExecutionsRequest
	queries  []indexQuery
	err      error
	queryErr string
}

func (s *visibilityStub) ListWorkflow(_ context.Context, req *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.pages[string(req.NextPageToken)], nil
}
func (s *visibilityStub) QueryWorkflow(_ context.Context, id, run, kind string, _ ...any) (converter.EncodedValue, error) {
	s.queries = append(s.queries, indexQuery{id, run, kind})
	if s.queryErr == kind {
		return nil, errors.New("query unavailable")
	}
	if kind == QueryOwner {
		return indexValue{s.owners[run]}, nil
	}
	return indexValue{s.sessions[run]}, nil
}
func execution(id, run string, at int64) *workflowpb.WorkflowExecutionInfo {
	return &workflowpb.WorkflowExecutionInfo{Execution: &commonpb.WorkflowExecution{WorkflowId: WorkflowID(id), RunId: run}, Type: &commonpb.WorkflowType{Name: WorkflowName}, StartTime: timestamppb.New(time.Unix(at, 0))}
}
func inventoryStub() *visibilityStub {
	return &visibilityStub{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"":     {Executions: []*workflowpb.WorkflowExecutionInfo{execution("ps_a", "a-old", 1), execution("ps_b", "b", 2)}, NextPageToken: []byte("next")},
		"next": {Executions: []*workflowpb.WorkflowExecutionInfo{execution("ps_a", "a-current", 3), execution("ps_legacy", "legacy", 1)}},
	}, owners: map[string]string{"a-old": "bob", "a-current": "alice", "b": "bob", "legacy": ""}, sessions: map[string]domain.Session{
		"a-current": {ID: "ps_a", Status: domain.StatusAccepted, Profile: domain.ProfileRef{Name: domain.ProfileOnboard}, Intake: domain.Intake{RepoURL: "https://github.com/org/app", Ref: "main", ProblemStatement: "private prompt"}, Messages: []domain.Message{{Text: "private message"}}, Plan: &domain.Plan{Text: "private plan"}},
		"b":         {ID: "ps_b", Status: domain.StatusNeedsInput},
	}}
}
func TestOwnedVisibilityPinsNewestExecutionAndNeverReadsOtherOwnerPayloads(t *testing.T) {
	stub := inventoryStub()
	service := NewClientService(stub, "", "alice", nil)
	got, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "ps_a" || got[0].WorkflowRunID != "a-current" || got[0].Status != domain.StatusAccepted {
		t.Fatalf("index %#v", got)
	}
	expected := []indexQuery{{WorkflowID("ps_a"), "a-current", QueryOwner}, {WorkflowID("ps_a"), "a-current", QuerySession}, {WorkflowID("ps_b"), "b", QueryOwner}, {WorkflowID("ps_legacy"), "legacy", QueryOwner}}
	if !reflect.DeepEqual(stub.queries, expected) {
		t.Fatalf("queries %#v", stub.queries)
	}
	for _, req := range stub.requests {
		if req.Query != `WorkflowType = "planning.Session"` || req.PageSize != 200 {
			t.Fatalf("visibility scope %#v", req)
		}
	}
	if len(stub.requests) != 2 || string(stub.requests[1].NextPageToken) != "next" {
		t.Fatalf("pagination %#v", stub.requests)
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"private prompt", "private message", "private plan"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("inventory exposed transcript")
		}
	}
	// A new Hub-side service instance enumerates the same durable backend.
	replica, err := NewClientService(stub, "", "alice", nil).List(context.Background())
	if err != nil || !reflect.DeepEqual(got, replica) {
		t.Fatalf("replica %#v %v", replica, err)
	}
	bob, err := NewClientService(stub, "", "bob", nil).List(context.Background())
	if err != nil || len(bob) != 1 || bob[0].ID != "ps_b" {
		t.Fatalf("other caller %#v %v", bob, err)
	}
}
func TestVisibilityFailsClosedOnUnavailableOrInvalidAuthoritativeState(t *testing.T) {
	for _, name := range []string{"visibility", "nil response", "owner query", "session query", "identity mismatch", "invalid execution", "repeated token", "oversized"} {
		t.Run(name, func(t *testing.T) {
			stub := inventoryStub()
			switch name {
			case "visibility":
				stub.err = errors.New("offline")
			case "nil response":
				stub.pages[""] = nil
			case "owner query":
				stub.queryErr = QueryOwner
			case "session query":
				stub.queryErr = QuerySession
			case "identity mismatch":
				stub.sessions["a-current"] = domain.Session{ID: "ps_other"}
			case "invalid execution":
				stub.pages[""].Executions[0].Execution.RunId = ""
			case "repeated token":
				stub.pages["next"].NextPageToken = []byte("next")
			case "oversized":
				for i := 0; i <= domain.MaxIndexedExecutions; i++ {
					stub.pages[""].Executions = append(stub.pages[""].Executions, execution("ps_more", "run", 1))
				}
			}
			if got, err := NewClientService(stub, "", "alice", nil).List(context.Background()); err == nil || got != nil {
				t.Fatalf("partial success %#v %v", got, err)
			}
		})
	}
	if _, err := NewClientService(inventoryStub(), "", "", nil).List(context.Background()); err == nil {
		t.Fatal("blank owner accepted")
	}
	if _, err := NewClientService(nil, "", "alice", nil).List(context.Background()); err == nil {
		t.Fatal("missing backend accepted")
	}
}
