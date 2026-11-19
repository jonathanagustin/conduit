package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

// No embedded Repository: this fixture cannot write, approve or lease a Plan.
type planDetailReadSource struct {
	record                      PlanRecord
	request                     ChangeRequest
	planFailure, requestFailure error
	calls                       []string
	contexts                    []context.Context
}

func (s *planDetailReadSource) GetPlan(ctx context.Context, id string) (PlanRecord, error) {
	s.calls = append(s.calls, "plan:"+id)
	s.contexts = append(s.contexts, ctx)
	return s.record, s.planFailure
}
func (s *planDetailReadSource) GetChangeRequest(ctx context.Context, id string) (ChangeRequest, error) {
	s.calls = append(s.calls, "request:"+id)
	s.contexts = append(s.contexts, ctx)
	return s.request, s.requestFailure
}

func TestPlanDetailReadPreservesCarrierAuthorizationBranches(t *testing.T) {
	repository, plans := planIndexFixture(t)
	if _, err := repository.PutResourceGrant(t.Context(), ResourceGrant{Revision: 1, ID: "read-dev", Team: "readers", ResourceRID: environmentRID(plans[0].Plan.Environment), Roles: []string{"environment:viewer"}}); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	options := HandlerOptions{BootstrapAdministrators: []string{"administrator"}}
	for _, tc := range []struct {
		name               string
		principal          Principal
		ordinary, governed bool
	}{
		{"outsider", Principal{Username: "outsider"}, false, false},
		{"environment-reader", Principal{Username: "reader", Teams: []string{"readers"}}, true, false},
		{"request-author", Principal{Username: "author"}, false, true},
		{"request-reviewer", Principal{Username: "reviewer", Teams: []string{"reviewers"}}, false, true},
		{"carrier-administrator", Principal{Username: "administrator", OnBehalfOf: &Principal{Username: "outsider"}}, true, true},
		{"represented-administrator", Principal{Username: "service", ServiceAccount: true, OnBehalfOf: &Principal{Username: "administrator"}}, false, false},
		{"delegated-reader", Principal{Username: "service", ServiceAccount: true, OnBehalfOf: &Principal{Username: "reader", Teams: []string{"readers"}}}, true, false},
	} {
		for _, governed := range []bool{false, true} {
			name := tc.name + "/ordinary"
			want := tc.ordinary
			if governed {
				name = tc.name + "/governed"
				want = tc.governed
			}
			t.Run(name, func(t *testing.T) {
				record := plans[0]
				record.Lease = &PlanLease{Token: "private-token"}
				if governed {
					record.ChangeRequestID = "cr"
				}
				source := &planDetailReadSource{record: record, request: ChangeRequest{ID: "cr", Environment: "prod", Author: Principal{Username: "author"}, Requirements: []ReviewerRequirement{{Kind: RequirementTeam, Team: "reviewers"}}}}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				got, err := ReadAuthorizedPlan(ctx, source, governance, tc.principal, options, record.ID)
				if !want {
					if !errors.Is(err, ErrForbidden) || !reflect.DeepEqual(got, PlanRecord{}) {
						t.Fatalf("refused read=%+v error=%v", got, err)
					}
				} else {
					expected := record
					lease := *record.Lease
					lease.Token = ""
					expected.Lease = &lease
					if err != nil || !reflect.DeepEqual(got, expected) {
						t.Fatalf("detail changed: %+v error=%v", got, err)
					}
					if source.record.Lease.Token != "private-token" {
						t.Fatal("redaction mutated stored lease")
					}
				}
				calls := []string{"plan:" + record.ID}
				if governed {
					calls = append(calls, "request:cr")
				}
				if !reflect.DeepEqual(source.calls, calls) {
					t.Fatalf("reads=%v", source.calls)
				}
				for _, got := range source.contexts {
					if got != ctx {
						t.Fatal("original context lost")
					}
				}
			})
		}
	}
}

func TestPlanDetailReadPreservesFailuresWithoutReturningPartialRecords(t *testing.T) {
	failure := errors.New("private store failure")
	for _, stage := range []string{"plan", "request"} {
		t.Run(stage, func(t *testing.T) {
			source := &planDetailReadSource{record: PlanRecord{ID: "plan", ChangeRequestID: "cr", Lease: &PlanLease{Token: "private-token"}}}
			if stage == "plan" {
				source.planFailure = failure
			} else {
				source.requestFailure = failure
			}
			got, err := ReadAuthorizedPlan(t.Context(), source, NewGovernanceService(NewMemoryRepository()), Principal{Username: "administrator"}, HandlerOptions{BootstrapAdministrators: []string{"administrator"}}, "plan")
			if err != failure || !reflect.DeepEqual(got, PlanRecord{}) {
				t.Fatalf("failure changed: %+v %v", got, err)
			}
			want := 1
			if stage == "request" {
				want = 2
			}
			if len(source.calls) != want {
				t.Fatalf("unexpected reads: %v", source.calls)
			}
		})
	}
}

func TestPlanDetailRESTUsesNativeRedactedResult(t *testing.T) {
	repository, plans := planIndexFixture(t)
	record, err := repository.UpdatePlan(t.Context(), plans[0].ID, func(record *PlanRecord) error { record.Lease = &PlanLease{Token: "private-token"}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	options := HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"administrator"}}
	native, err := ReadAuthorizedPlan(t.Context(), repository, NewGovernanceService(repository), Principal{Username: "administrator"}, options, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := governanceRequest(t, NewHandler(repository, options), http.MethodGet, "/v1/plans/"+record.ID, "administrator", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("REST %d %s", response.Code, response.Body.String())
	}
	var decoded PlanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(native)
	got, _ := json.Marshal(decoded)
	if string(got) != string(want) {
		t.Fatalf("REST/native mismatch: %s != %s", got, want)
	}
	if decoded.Lease == nil || decoded.Lease.Token != "" {
		t.Fatal("REST exposed lease")
	}
	stored, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || stored.Lease.Token != "private-token" {
		t.Fatal("read changed persisted lease")
	}
}
