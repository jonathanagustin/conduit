package composition

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type resourceAuditRepository struct {
	*controlplane.MemoryRepository
	fail     bool
	mu       sync.Mutex
	attempts []controlplane.AuthorizationAuditEvent
}

func (repository *resourceAuditRepository) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	repository.mu.Lock()
	repository.attempts = append(repository.attempts, event)
	repository.mu.Unlock()
	if repository.fail {
		return controlplane.AuthorizationAuditEvent{}, errors.New("PRIVATE-AUDIT-DIAGNOSTIC")
	}
	return repository.MemoryRepository.AppendAuthorizationAudit(ctx, event)
}
func TestPlanApprovalRequiresResourceDecisionAuditAcrossTransports(t *testing.T) {
	for _, rpc := range []bool{false, true} {
		for _, outage := range []bool{false, true} {
			for _, actor := range []string{"root", "alice", "robot", "outsider"} {
				t.Run(map[bool]string{false: "REST", true: "Connect"}[rpc]+"/"+map[bool]string{false: "healthy", true: "outage"}[outage]+"/"+actor, func(t *testing.T) {
					memory := controlplane.NewMemoryRepository()
					service := controlplane.NewGovernanceService(memory)
					for _, team := range []controlplane.Team{{Name: "ops", DisplayName: "Ops", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}, {Name: "automation", DisplayName: "Automation", AdminUsers: []string{"owner"}, MemberUsers: []string{"robot"}}} {
						if _, err := service.PutTeam(t.Context(), team); err != nil {
							t.Fatal(err)
						}
					}
					for _, grant := range []controlplane.ResourceGrant{{ResourceRID: "environment/dev", Team: "ops", Operations: []string{controlplane.OperationOperate}}, {ResourceRID: "platform/root", Team: "automation", Operations: []string{controlplane.OperationActOnBehalf}}} {
						if _, err := service.PutGrant(t.Context(), grant); err != nil {
							t.Fatal(err)
						}
					}
					now := time.Date(2026, 11, 16, 1, 2, 3, 123456789, time.UTC)
					before := controlplane.PlanRecord{ID: "plan", DesiredStateDigest: "sha256:desired", Plan: controlplane.Plan{Environment: "dev", Generation: "one"}, Status: controlplane.PlanAwaitingApproval, RequiredApprovals: 1, Revision: 9007199254740993, CreatedAt: now, UpdatedAt: now}
					if _, err := memory.CreatePlan(t.Context(), before); err != nil {
						t.Fatal(err)
					}
					repository := &resourceAuditRepository{MemoryRepository: memory, fail: outage}
					driver := &approvalOrchestrator{}
					handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{PlanOrchestrator: driver})
					if err != nil {
						t.Fatal(err)
					}
					fixture := &approvalFixture{repository: memory, driver: driver, server: httptest.NewServer(handler)}
					defer fixture.server.Close()
					represented := ""
					if actor == "robot" {
						represented = "alice"
					}
					status, _, callErr := invokePlanApproval(t, fixture, rpc, actor, "reviewed audit fixture", represented)
					refused := outage || actor == "outsider"
					if refused {
						if rpc {
							if connect.CodeOf(callErr) != connect.CodePermissionDenied {
								t.Fatalf("RPC error=%v", callErr)
							}
						} else if status != 403 {
							t.Fatalf("REST status=%d", status)
						}
					} else if callErr != nil || (!rpc && status != 200) {
						t.Fatalf("healthy approval failed: %d %v", status, callErr)
					}
					stored, err := memory.GetPlan(t.Context(), "plan")
					if err != nil {
						t.Fatal(err)
					}
					if refused {
						if !reflect.DeepEqual(stored, before) || driver.calls() != 0 {
							t.Fatalf("approval escaped failed admission: %#v; sequencing=%d", stored, driver.calls())
						}
					} else if stored.Status != controlplane.PlanApproved || len(stored.Approvals) != 1 || stored.Revision != before.Revision+1 || driver.calls() != 1 {
						t.Fatalf("healthy approval changed: %#v; sequencing=%d", stored, driver.calls())
					}
					repository.mu.Lock()
					attempts := append([]controlplane.AuthorizationAuditEvent{}, repository.attempts...)
					repository.mu.Unlock()
					resourceEvents := []controlplane.AuthorizationAuditEvent{}
					for _, event := range attempts {
						if event.Action == controlplane.OperationOperate {
							resourceEvents = append(resourceEvents, event)
						}
					}
					if len(resourceEvents) != 1 {
						t.Fatalf("resource audit attempts=%d", len(resourceEvents))
					}
					event := resourceEvents[0]
					verdict := "ALLOW"
					if actor == "outsider" {
						verdict = "DENY"
					}
					if event.Subject != actor || event.ResourceRID != "environment/dev" || event.Decision != verdict || event.Context["onBehalfOf"] != represented {
						t.Fatalf("carrier or decision changed: %#v", event)
					}
				})
			}
		}
	}
}
