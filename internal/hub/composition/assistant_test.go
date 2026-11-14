package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type runningPackaging struct{}

func (runningPackaging) Read(_ context.Context, runID string) (packagingdomain.RunStatus, error) {
	if runID != "sample-app-1" {
		return packagingdomain.RunStatus{}, packaginghttp.ErrNotFound
	}
	return packagingdomain.RunStatus{Configured: true, RunID: runID, Phase: "RUNNING"}, nil
}

func (runningPackaging) List(context.Context) ([]packagingdomain.RunStatus, error) {
	return nil, nil
}

func (runningPackaging) Events(context.Context, string) ([]packagingdomain.StepEvent, error) {
	return nil, nil
}

func TestAssistantDeniesWithoutARoleAndAllowsAPendingRequest(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{PackagingStatus: runningPackaging{}})
	if err != nil {
		t.Fatal(err)
	}
	denied := postAssistant(handler, "local-developer", "", "/v1/assistant/tools", `{
		"mode":"release_plan",
		"tool":"propose_governed_action",
		"arguments":{"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}
	}`)
	if denied.Code != http.StatusOK || !strings.Contains(denied.Body.String(), "caller does not have a role") || strings.Contains(denied.Body.String(), `"allowed":true`) {
		t.Fatalf("deny = %d %s", denied.Code, denied.Body.String())
	}
	stored, err := repository.ListChangeRequests(context.Background(), "")
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored = %#v %v", stored, err)
	}
	allowed := postAssistant(handler, "local-developer", "Operator", "/v1/assistant/tools", `{
		"mode":"ontology",
		"tool":"propose_governed_action",
		"arguments":{"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}
	}`)
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"allowed":true`) || !strings.Contains(allowed.Body.String(), "PENDING_APPROVALS") {
		t.Fatalf("allow = %d %s", allowed.Code, allowed.Body.String())
	}
	stored, err = repository.ListChangeRequests(context.Background(), "")
	if err != nil || len(stored) != 1 || stored[0].Status != controlplane.ChangePendingApprovals {
		t.Fatalf("opened = %#v %v", stored, err)
	}
	turned := postAssistant(handler, "local-developer", "", "/v1/assistant/turns", `{"mode":"explore","message":"list environments"}`)
	if turned.Code != http.StatusUnprocessableEntity || !strings.Contains(turned.Body.String(), "No model call") {
		t.Fatalf("turn = %d %s", turned.Code, turned.Body.String())
	}
}

func TestAssistantQueryUsesTheCatalog(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProposeHumanChange(context.Background(), "PLAN", "sample-app-1:memory", "dev", []controlplane.EntityChange{{
		ID: "memory-limit", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: "sample-app",
		Diff: map[string]controlplane.ChangeDiffEntry{"limits.memory": {Before: "256Mi", After: "512Mi"}},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/dev", Name: "dev"},
			"installation": {RID: "installation/dev/sample-app", Name: "sample-app"},
			"product":      {RID: "product/app.example:sample-app", Name: "sample-app"},
		},
	}}, controlplane.Principal{Username: "operator"}, "Proposed memory limit 512Mi", "headroom"); err != nil {
		t.Fatal(err)
	}
	queried := postAssistant(handler, "local-developer", "", "/v1/assistant/tools", `{
		"mode":"explore","tool":"query_ontology_objects",
		"arguments":{"objectType":"ChangeRequest","filter":{"field":"status","value":"PENDING_APPROVALS"}}
	}`)
	if queried.Code != http.StatusOK || !strings.Contains(queried.Body.String(), `"writeOps":0`) || !strings.Contains(queried.Body.String(), "ChangeRequest") {
		t.Fatalf("query = %d %s", queried.Code, queried.Body.String())
	}
	var body struct {
		ChangeRequestID string `json:"changeRequestId"`
		Entities        []struct {
			ID string `json:"id"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(queried.Body.Bytes(), &body); err != nil || len(body.Entities) != 1 {
		t.Fatalf("entities = %#v %v", body, err)
	}
	diff := postAssistant(handler, "local-developer", "", "/v1/assistant/tools", `{
		"mode":"config_diff","tool":"review_change_diff",
		"arguments":{"changeRequestId":"`+body.Entities[0].ID+`"}
	}`)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "512Mi") || !strings.Contains(diff.Body.String(), "256Mi") {
		t.Fatalf("diff = %d %s", diff.Code, diff.Body.String())
	}
}

func postAssistant(handler http.Handler, subject, groups, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", subject)
	if groups != "" {
		request.Header.Set("X-Platform-Groups", groups)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
