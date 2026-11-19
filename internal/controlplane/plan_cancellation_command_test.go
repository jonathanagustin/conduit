package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNativePlanCancellationPreservesRESTPolicyAndSuppressionValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                         string
		governed, grant, wrongAuthor bool
		command                      PlanCancellationCommand
		status                       int
	}{
		{name: "author without Environment authority", governed: true, command: PlanCancellationCommand{Reason: "stop"}, status: 200},
		{name: "non-author with Operate", governed: true, grant: true, wrongAuthor: true, command: PlanCancellationCommand{Reason: "stop"}, status: 403},
		{name: "author cannot plant suppression", governed: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true}, status: 403},
		{name: "ungranted native Plan", command: PlanCancellationCommand{Reason: "stop"}, status: 403},
		{name: "default suppression", governed: true, grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "example-api"}, status: 200},
		{name: "minimum suppression", grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "example-api", SuppressionDurationSeconds: 60}, status: 200},
		{name: "maximum suppression", grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "example-api", SuppressionDurationSeconds: 30 * 24 * 60 * 60}, status: 200},
		{name: "duration ignored without suppression", grant: true, command: PlanCancellationCommand{Reason: "stop", SuppressionDurationSeconds: -1}, status: 200},
		{name: "short suppression", grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, SuppressionDurationSeconds: 59}, status: 422},
		{name: "long suppression", grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, SuppressionDurationSeconds: 30*24*60*60 + 1}, status: 422},
		{name: "unplanned installation", grant: true, command: PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "not-planned"}, status: 422},
		{name: "missing reason", grant: true, command: PlanCancellationCommand{}, status: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"native", "REST"} {
				t.Run(transport, func(t *testing.T) {
					ctx := t.Context()
					repository := NewMemoryRepository()
					governance := NewGovernanceService(repository)
					lifecycle := NewLifecycle(repository)
					caller := Principal{Username: "author"}
					if tc.wrongAuthor {
						caller.Username = "other"
					}
					var record PlanRecord
					if tc.governed {
						if _, err := governance.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}); err != nil {
							t.Fatal(err)
						}
						if _, err := governance.PutPolicy(ctx, ApprovalPolicy{ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`}); err != nil {
							t.Fatal(err)
						}
						var err error
						record, _, err = governance.ProposePlan(ctx, foundation(), Principal{Username: "author"}, "", "")
						if err != nil {
							t.Fatal(err)
						}
					} else {
						record = planWithoutAChangeRequest(t, repository)
					}
					if tc.grant {
						grantOperateOnEnvironment(t, repository, caller.Username, record.Plan.Environment)
					}
					// REST resolves team membership before invoking the native command.
					// Supply the same authenticated carrier to the direct call.
					caller, err := governance.ResolvePrincipal(ctx, IdentityClaims{Username: caller.Username})
					if err != nil {
						t.Fatal(err)
					}
					options := HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}
					status := http.StatusOK
					if transport == "native" {
						_, err := CancelPlanAsOperator(ctx, repository, lifecycle, governance, caller, options, record.ID, tc.command)
						var validation PlanCancellationValidationError
						switch {
						case err == nil:
						case errors.Is(err, ErrForbidden):
							status = 403
						case errors.As(err, &validation):
							status = 422
						case errors.Is(err, ErrInvalidTransition):
							status = 409
						default:
							t.Fatalf("unexpected native error: %v", err)
						}
					} else {
						b, err := json.Marshal(tc.command)
						if err != nil {
							t.Fatal(err)
						}
						request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+record.ID+"/cancel", bytes.NewReader(b))
						request.Header.Set("X-Platform-Subject", caller.Username)
						request.Header.Set("Content-Type", "application/json")
						response := httptest.NewRecorder()
						NewHandler(repository, options).ServeHTTP(response, request)
						status = response.Code
						if status == 422 {
							var body map[string]string
							if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 1 || body["error"] == "" {
								t.Fatalf("validation envelope changed: %s %v", response.Body.String(), err)
							}
						}
					}
					if status != tc.status {
						t.Fatalf("status=%d want=%d", status, tc.status)
					}
					stored, err := repository.GetPlan(ctx, record.ID)
					if err != nil {
						t.Fatal(err)
					}
					suppressions, err := repository.ListSuppressions(ctx, record.Plan.Environment)
					if err != nil {
						t.Fatal(err)
					}
					if status != 200 {
						if stored.Status != record.Status || stored.Cancellation != nil || len(suppressions) != 0 {
							t.Fatalf("refused command mutated state: %#v %#v", stored, suppressions)
						}
						return
					}
					if stored.Status != PlanCancelled || stored.Cancellation == nil || stored.Cancellation.Subject != caller.Username || stored.Cancellation.Reason != "stop" {
						t.Fatalf("cancellation=%#v", stored)
					}
					if tc.governed {
						request, err := repository.GetChangeRequest(ctx, stored.ChangeRequestID)
						if err != nil || request.Status != ChangeCancelled {
							t.Fatalf("request=%#v err=%v", request, err)
						}
					}
					if tc.command.CreateSuppression {
						seconds := tc.command.SuppressionDurationSeconds
						if seconds == 0 {
							seconds = 30 * 60
						}
						if len(suppressions) != 1 || suppressions[0].EndsAt == nil || suppressions[0].EndsAt.Sub(suppressions[0].StartsAt) != time.Duration(seconds)*time.Second || suppressions[0].InstallationID != "example-api" {
							t.Fatalf("suppression=%#v", suppressions)
						}
					} else if len(suppressions) != 0 {
						t.Fatalf("unexpected suppression=%#v", suppressions)
					}
				})
			}
		})
	}
}
