package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type commandRecallProbe struct{ recalls []Recall }

func (probe *commandRecallProbe) RecallsInForce(_ context.Context, _ string, recalls []Recall) error {
	probe.recalls = append(probe.recalls, recalls...)
	return nil
}

func TestCommandInputDoesNotAnnounceAnUncommittedRecallDeclaration(t *testing.T) {
	repository := NewMemoryRepository()
	state := foundation()
	if _, err := repository.PutEnvironmentReport(t.Context(), installedReport(t, state)); err != nil {
		t.Fatal(err)
	}
	state.Recalls = []Recall{{
		ID: "command-input-only", Product: state.Releases[0].Product,
		StartingVersion: "9.0.0", SingleVersion: true, Reason: "not an accepted Environment declaration",
		Strategy: RecallStrategy{Type: RecallRollForward},
	}}
	probe := &commandRecallProbe{}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"}, RecallOpenings: probe,
	})
	post := func(path string, value any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		request.Header.Set("X-Platform-Subject", "operator")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", path, response.Code, response.Body)
		}
		return response
	}
	response := post("/v1/commands", IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfig, InstallationID: "example-api", Rationale: "recover configuration",
	})
	var command PlanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &command); err != nil || command.Command == nil || command.DesiredState == nil || len(command.DesiredState.Recalls) != 0 {
		t.Fatalf("command changed its recall-bypassing persisted state: %#v %v", command, err)
	}
	if len(probe.recalls) != 0 {
		t.Fatalf("command input announced a recall it never persisted: %#v", probe.recalls)
	}
	// The same recall on an ordinary accepted desired state still announces.
	state.Environment.Generation += "-accepted"
	post("/v1/plans", state)
	if len(probe.recalls) != 1 || probe.recalls[0].ID != "command-input-only" {
		t.Fatalf("ordinary accepted recall stopped announcing: %#v", probe.recalls)
	}
}
