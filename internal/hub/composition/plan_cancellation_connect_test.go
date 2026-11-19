package composition

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPlanCancellationConnectMatchesRESTAuthorityAndEffects(t *testing.T) {
	for _, tc := range []struct {
		name, actor, represented string
		governed, authorOperate  bool
		command                  controlplane.PlanCancellationCommand
		status                   int
		code                     connect.Code
	}{
		{name: "ordinary operator", actor: "alice", command: controlplane.PlanCancellationCommand{Reason: "stop"}, status: 200},
		{name: "reader refused", actor: "reader", command: controlplane.PlanCancellationCommand{Reason: "stop"}, status: 403, code: connect.CodePermissionDenied},
		{name: "outsider refused", actor: "outsider", command: controlplane.PlanCancellationCommand{Reason: "stop"}, status: 403, code: connect.CodePermissionDenied},
		{name: "author without Operate", actor: "author", governed: true, command: controlplane.PlanCancellationCommand{Reason: "stop"}, status: 200},
		{name: "non-author with Operate", actor: "alice", governed: true, command: controlplane.PlanCancellationCommand{Reason: "stop"}, status: 403, code: connect.CodePermissionDenied},
		{name: "author cannot suppress", actor: "author", governed: true, command: controlplane.PlanCancellationCommand{Reason: "stop", CreateSuppression: true}, status: 403, code: connect.CodePermissionDenied},
		{name: "author with suppression authority", actor: "author", governed: true, authorOperate: true, command: controlplane.PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "app"}, status: 200},
		{name: "short duration", actor: "alice", command: controlplane.PlanCancellationCommand{Reason: "stop", CreateSuppression: true, SuppressionDurationSeconds: 59}, status: 422, code: connect.CodeInvalidArgument},
		{name: "unplanned installation", actor: "alice", command: controlplane.PlanCancellationCommand{Reason: "stop", CreateSuppression: true, InstallationID: "other"}, status: 422, code: connect.CodeInvalidArgument},
		{name: "missing reason", actor: "alice", status: 409, code: connect.CodeFailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rpc := range []bool{false, true} {
				transport := "REST"
				if rpc {
					transport = "Connect"
				}
				t.Run(transport, func(t *testing.T) {
					// Reuse the same native Plan, governance teams and carrier transport as approval parity.
					f := newApprovalFixture(t, tc.governed, 1, false)
					if tc.authorOperate {
						team, err := f.repository.GetTeam(t.Context(), "operators")
						if err != nil {
							t.Fatal(err)
						}
						team.MemberUsers = append(team.MemberUsers, "author")
						if _, err := controlplane.NewGovernanceService(f.repository).PutTeam(t.Context(), team); err != nil {
							t.Fatal(err)
						}
					}
					base := f.server.Client()
					client := *base
					client.Transport = approvalActorTransport{actor: tc.actor, represented: tc.represented, next: base.Transport}
					var result *plansv1.CancelPlanResponse
					if rpc {
						api := plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(&client, f.server.URL)))
						got, err := api.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: tc.command.Reason, CreateSuppression: tc.command.CreateSuppression, SuppressionDurationSeconds: int64(tc.command.SuppressionDurationSeconds), InstallationId: tc.command.InstallationID})
						if tc.status != 200 {
							if connect.CodeOf(err) != tc.code {
								t.Fatalf("error=%v want=%s", err, tc.code)
							}
						} else if err != nil {
							t.Fatal(err)
						} else {
							result = got
						}
					} else {
						b, err := json.Marshal(tc.command)
						if err != nil {
							t.Fatal(err)
						}
						request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/v1/plans/plan/cancel", bytes.NewReader(b))
						if err != nil {
							t.Fatal(err)
						}
						request.Header.Set("Content-Type", "application/json")
						response, err := client.Do(request)
						if err != nil {
							t.Fatal(err)
						}
						defer response.Body.Close()
						if response.StatusCode != tc.status {
							t.Fatalf("status=%d want=%d", response.StatusCode, tc.status)
						}
						if response.StatusCode == 200 {
							var got controlplane.PlanRecord
							if err := json.NewDecoder(response.Body).Decode(&got); err != nil || got.Lease == nil || got.Lease.Token != "" {
								t.Fatalf("REST lease/redaction=%#v %v", got, err)
							}
						}
					}
					stored, err := f.repository.GetPlan(t.Context(), "plan")
					if err != nil {
						t.Fatal(err)
					}
					suppressions, err := f.repository.ListSuppressions(t.Context(), "dev")
					if err != nil {
						t.Fatal(err)
					}
					if tc.status != 200 {
						if stored.Status != controlplane.PlanAwaitingApproval || stored.Cancellation != nil || len(suppressions) != 0 {
							t.Fatalf("refused command changed state: %#v %#v", stored, suppressions)
						}
						return
					}
					if stored.Status != controlplane.PlanCancelled || stored.Cancellation == nil || stored.Cancellation.Subject != tc.actor || stored.Cancellation.Reason != "stop" {
						t.Fatalf("native result=%#v", stored)
					}
					if rpc {
						if result.Plan.Id != stored.ID || result.Plan.Environment != stored.Plan.Environment || result.Plan.Status != string(stored.Status) || result.Plan.Revision != stored.Revision || result.Plan.StepCount != int64(len(stored.Plan.Steps)) || result.ChangeRequestId != stored.ChangeRequestID || result.Cancellation.Subject != stored.Cancellation.Subject || result.Cancellation.Reason != stored.Cancellation.Reason || !result.Cancellation.CreatedAt.AsTime().Equal(stored.Cancellation.CreatedAt) || !result.Plan.UpdatedAt.AsTime().Equal(stored.UpdatedAt) {
							t.Fatalf("wire result changed: %v native=%#v", result, stored)
						}
					}
					if tc.governed {
						request, err := f.repository.GetChangeRequest(t.Context(), "cr")
						if err != nil || request.Status != controlplane.ChangeCancelled || request.Reviews[len(request.Reviews)-1].Subject != tc.actor {
							t.Fatalf("request=%#v error=%v", request, err)
						}
					}
					if tc.command.CreateSuppression {
						if len(suppressions) != 1 || suppressions[0].InstallationID != "app" || suppressions[0].EndsAt == nil || suppressions[0].EndsAt.Sub(suppressions[0].StartsAt) != 30*time.Minute {
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

func TestPlanCancellationConnectErrorDoesNotClaimNoPersistedEffects(t *testing.T) {
	f := newApprovalFixture(t, true, 1, false)
	if _, err := f.repository.UpdateChangeRequest(t.Context(), "cr", func(r *controlplane.ChangeRequest) error { r.Status = controlplane.ChangeCancelled; return nil }); err != nil {
		t.Fatal(err)
	}
	base := f.server.Client()
	client := *base
	client.Transport = approvalActorTransport{actor: "author", next: base.Transport}
	api := plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(&client, f.server.URL)))
	_, err := api.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
	var failure *connect.Error
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !errors.As(err, &failure) || len(failure.Details()) != 0 {
		t.Fatalf("partial outcome=%v", err)
	}
	stored, err := f.repository.GetPlan(t.Context(), "plan")
	if err != nil || stored.Status != controlplane.PlanCancelled {
		t.Fatalf("existing sequential effect lost: %#v %v", stored, err)
	}
}

func TestPlanCancellationConnectProductionAuthAndUnknownMethods(t *testing.T) {
	repository := &approvalReadCounter{Repository: controlplane.NewMemoryRepository()}
	handler, err := NewHub(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}, Options{PlanOrchestrator: &approvalOrchestrator{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "root"} {
		request := httptest.NewRequest(http.MethodPost, "/conduit.plans.v1.PlanCancellationService/CancelPlan", strings.NewReader("{"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Platform-Subject", actor)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 || repository.reads.Load() != 0 {
			t.Fatalf("production auth: %d %s reads=%d", response.Code, response.Body.String(), repository.reads.Load())
		}
	}
	for _, tc := range []struct{ method, path string }{{"GET", "/conduit.plans.v1.PlanCancellationService/CancelPlan"}, {"POST", "/conduit.plans.v1.PlanCancellationService/CancelEverything"}, {"POST", "/conduit.plans.v1.PlanReadService/CancelPlan"}} {
		response := driveAs(t, handler, tc.method, tc.path, "root", "")
		if (response.Code != 404 && response.Code != 405) || repository.reads.Load() != 0 {
			t.Fatalf("unknown method: %d reads=%d", response.Code, repository.reads.Load())
		}
	}
	f := newApprovalFixture(t, false, 1, false)
	base := f.server.Client()
	client := *base
	client.Transport = approvalActorTransport{actor: "alice", next: base.Transport}
	api := plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(&client, f.server.URL)))
	_, err = api.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "missing", Reason: "stop"})
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing Plan=%v", err)
	}
	stored, err := f.repository.GetPlan(t.Context(), "plan")
	if err != nil || stored.Status != controlplane.PlanAwaitingApproval || f.driver.calls() != 0 {
		t.Fatalf("unrelated Plan changed: %#v %v", stored, err)
	}
}

func TestPlanCancellationConnectCookieWritesRequireOriginAndCSRF(t *testing.T) {
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	for _, tc := range []struct {
		name, origin, csrf string
		status             int
	}{
		{"same-origin command", signInOrigin, "1", 404},
		{"missing CSRF", signInOrigin, "", 401},
		{"foreign Origin", "https://evil.example", "1", 401},
		{"missing Origin", "", "1", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, signInOrigin+"/conduit.plans.v1.PlanCancellationService/CancelPlan", strings.NewReader(`{"planId":"missing","reason":"stop"}`))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.csrf != "" {
				request.Header.Set("X-Conduit-CSRF", tc.csrf)
			}
			response := harness.do(request)
			if response.Code != tc.status {
				t.Fatalf("cookie command: %d %s want=%d", response.Code, response.Body.String(), tc.status)
			}
		})
	}
}

func TestPlanCancellationConnectDelegationPreservesCarrier(t *testing.T) {
	for _, tc := range []struct {
		actor, represented string
		governed, allowed  bool
	}{{"alice", "bob", false, true}, {"alice", "bob", true, true}, {"robot", "alice", false, false}, {"robot", "alice", true, false}} {
		t.Run(fmt.Sprintf("%s/%s/governed=%v", tc.actor, tc.represented, tc.governed), func(t *testing.T) {
			for _, rpc := range []bool{false, true} {
				f := newDelegatedApprovalFixture(t, tc.governed)
				base := f.server.Client()
				client := *base
				client.Transport = approvalActorTransport{actor: tc.actor, represented: tc.represented, next: base.Transport}
				if rpc {
					api := plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(&client, f.server.URL)))
					_, err := api.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
					if tc.allowed && err != nil || !tc.allowed && connect.CodeOf(err) != connect.CodePermissionDenied {
						t.Fatalf("delegated RPC=%v", err)
					}
				} else {
					request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/v1/plans/plan/cancel", strings.NewReader(`{"reason":"stop"}`))
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Content-Type", "application/json")
					response, err := client.Do(request)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					want := 403
					if tc.allowed {
						want = 200
					}
					if response.StatusCode != want {
						t.Fatalf("delegated REST=%d want=%d", response.StatusCode, want)
					}
				}
				stored, err := f.repository.GetPlan(t.Context(), "plan")
				if err != nil {
					t.Fatal(err)
				}
				if tc.allowed {
					if stored.Status != controlplane.PlanCancelled || stored.Cancellation == nil || stored.Cancellation.Subject != tc.actor {
						t.Fatalf("carrier lost: %#v", stored)
					}
				} else if stored.Status != controlplane.PlanAwaitingApproval || stored.Cancellation != nil {
					t.Fatalf("refused delegation changed Plan: %#v", stored)
				}
			}
		})
	}
}
