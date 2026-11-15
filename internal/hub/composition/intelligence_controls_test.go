package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestIntelligenceCourseUsesCurrentAuthorizedTargetAndScopedIdentity(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, controlplane.Team{Name: "course-writers", DisplayName: "Course writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"env-a", "env-b", "env-c"} {
		if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{ID: "plan-" + environment, Status: controlplane.PlanAwaitingApproval, CreatedAt: time.Now(), Plan: controlplane.Plan{Environment: environment, Steps: []controlplane.PlanStep{{Entity: "api", InstallationID: "api", Product: "product-a", Version: "1"}}}}); err != nil {
			t.Fatal(err)
		}
		if environment != "env-c" {
			if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "environment/" + environment, Team: "course-writers", Roles: []string{"environment:editor"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	submit := func(environment, installation, product, value string) (int, map[string]any) {
		payload, _ := json.Marshal(map[string]string{"Environment": environment, "InstallationID": installation, "ProductID": product, "Value": value})
		response := postAssistant(handler, "writer", "", "/v1/intelligence/courses/select", string(payload))
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(response.Body.String(), err)
		}
		return response.Code, body
	}
	for _, test := range []struct {
		environment, installation, product, value string
		status                                    int
	}{
		{"env-c", "api", "product-a", "512Mi", http.StatusForbidden},
		{"env-a", "missing", "product-a", "512Mi", http.StatusNotFound},
		{"env-a", "api", "wrong-product", "512Mi", http.StatusNotFound},
		{"env-a", "api", "product-a", "8Gi", http.StatusUnprocessableEntity},
	} {
		status, body := submit(test.environment, test.installation, test.product, test.value)
		if status != test.status {
			t.Fatal(test, status, body)
		}
	}
	stored, err := repository.ListChangeRequests(ctx, "")
	if err != nil || len(stored) != 0 {
		t.Fatal("denied course persisted", stored, err)
	}
	status, a := submit("env-a", "api", "product-a", "512Mi")
	if status != http.StatusOK {
		t.Fatal(status, a)
	}
	status, b := submit("env-b", "api", "product-a", "512Mi")
	if status != http.StatusOK {
		t.Fatal(status, b)
	}
	if a["changeRequestId"] == b["changeRequestId"] {
		t.Fatal("course identity leaked across environments", a, b)
	}
	status, retry := submit("env-a", "api", "product-a", "512Mi")
	if status != http.StatusOK || retry["changeRequestId"] != a["changeRequestId"] {
		t.Fatal("same scoped proposal lost identity", status, retry, a)
	}
	stored, err = repository.ListChangeRequests(ctx, "")
	if err != nil || len(stored) != 2 {
		t.Fatal(stored, err)
	}
	for _, request := range stored {
		if request.TargetKind != "CONDUIT_NATIVE_INTELLIGENCE_COURSE" || request.Status != controlplane.ChangePendingApprovals || request.RequirementsSatisfied || request.Author.Username != "writer" {
			t.Fatal("course gained approval or lost caller scope", request)
		}
	}
	response := postAssistant(handler, "writer", "", "/v1/intelligence/courses/select", `{"Value":"512Mi","Environment":"env-a","InstallationID":"api","ProductID":"product-a"} {"approve":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatal("trailing course document accepted", response.Code, response.Body.String())
	}
}
