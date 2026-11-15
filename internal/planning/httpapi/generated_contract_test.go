package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

func TestGeneratedPlanningClientAgainstStrictServer(t *testing.T) {
	server := httptest.NewServer(newMux(t, application.NewService(scriptedPlanner{})))
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := client.CreatePlanningSessionWithResponse(ctx, api.CreatePlanningSessionJSONRequestBody{RepoUrl: "https://github.com/org/app", Ref: "main"})
	if err != nil || created.JSON201 == nil {
		t.Fatalf("create: %+v %v", created, err)
	}
	session := created.JSON201
	if session.Binding != nil || session.Plan != nil || session.Inspection != nil {
		t.Fatalf("unprepared fields must be null: %+v", session)
	}
	blocked, err := client.StartPlanningComputeWithResponse(ctx, session.Id)
	if err != nil || blocked.JSON409 == nil || blocked.JSON409.Code == nil || *blocked.JSON409.Code != api.COMPUTEBEFOREACCEPT {
		t.Fatalf("compute gate: %+v %v", blocked, err)
	}
	turn, err := client.PostPlanningTurnWithResponse(ctx, session.Id, api.PostPlanningTurnJSONRequestBody{})
	if err != nil || turn.JSON200 == nil || turn.JSON200.Plan == nil || len(turn.JSON200.NeedsInput) != 1 {
		t.Fatalf("turn: %+v %v", turn, err)
	}
	answered, err := client.AnswerPlanningNeedsInputWithResponse(ctx, session.Id, api.AnswerPlanningNeedsInputJSONRequestBody{Answers: []api.Answer{{Id: "sso", Value: "true"}}})
	if err != nil || answered.JSON200 == nil {
		t.Fatalf("answers: %+v %v", answered, err)
	}
	accepted, err := client.AcceptPlanningSessionWithResponse(ctx, session.Id)
	if err != nil || accepted.JSON200 == nil || accepted.JSON200.Binding == nil || accepted.JSON200.Binding.Plan.Sha256 == "" {
		t.Fatalf("accept: %+v %v", accepted, err)
	}
	binding, err := client.GetPlanningBindingWithResponse(ctx, session.Id)
	if err != nil || binding.JSON200 == nil || binding.JSON200.Plan.Sha256 != accepted.JSON200.Binding.Plan.Sha256 {
		t.Fatalf("binding: %+v %v", binding, err)
	}
	events, err := client.ListPlanningEventsWithResponse(ctx, session.Id)
	if err != nil || events.JSON200 == nil || len(events.JSON200.Items) == 0 {
		t.Fatalf("events: %+v %v", events, err)
	}
	compute, err := client.StartPlanningComputeWithResponse(ctx, session.Id)
	if err != nil || compute.JSON501 == nil || compute.JSON501.Code == nil || *compute.JSON501.Code != api.COMPUTENOTCONFIGURED {
		t.Fatalf("unconfigured compute: %+v %v", compute, err)
	}
	read, err := client.GetPlanningSessionWithResponse(ctx, session.Id)
	if err != nil || read.JSON200 == nil || string(read.JSON200.Status) != "accepted" {
		t.Fatalf("read: %+v %v", read, err)
	}
}

func TestGeneratedPlanningClientRejectsUndeclaredBodyFields(t *testing.T) {
	server := httptest.NewServer(newMux(t, application.NewService(nil)))
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CreatePlanningSessionWithBodyWithResponse(t.Context(), "application/json", strings.NewReader(`{"repoUrl":"https://github.com/org/app","ref":"main","approve":true}`))
	if err != nil || response.JSON400 == nil {
		t.Fatalf("unknown field accepted: %+v %v", response, err)
	}
}
