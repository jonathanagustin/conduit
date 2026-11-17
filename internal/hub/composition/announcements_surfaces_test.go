package composition

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/adapters/memorybridge"
	acpdomain "github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

// Observe through the independent repository reader before forwarding to the
// real emitter. The notification must describe an already persisted request.
type committedOpeningProbe struct {
	t          *testing.T
	repository controlplane.Repository
	next       governanceports.ChangeRequestOpenings
	openings   []governanceports.ChangeRequestOpening
}

func (probe *committedOpeningProbe) AwaitingApproval(ctx context.Context, opening governanceports.ChangeRequestOpening) error {
	stored, err := probe.repository.GetChangeRequest(ctx, opening.ID)
	if err != nil || stored.Status != controlplane.ChangePendingApprovals || stored.Revision != opening.Revision || stored.Environment != opening.Environment {
		probe.t.Errorf("announcement preceded persisted pending request: opening=%#v stored=%#v error=%v", opening, stored, err)
	}
	probe.openings = append(probe.openings, opening)
	if probe.next != nil {
		return probe.next.AwaitingApproval(ctx, opening)
	}
	return errors.New("notification destination unavailable after the request committed")
}

func TestAlternateHubSurfacesAnnouncePersistedHumanProposals(t *testing.T) {
	for _, surface := range []string{"assistant", "intelligence", "workspace", "blackout", "waves", "drift", "offline-bundle"} {
		t.Run(surface, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			now := time.Now().UTC()
			if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{
				ID: "existing-plan", Status: controlplane.PlanSucceeded, CreatedAt: now, UpdatedAt: now,
				Plan: controlplane.Plan{Environment: "dev", Steps: []controlplane.PlanStep{{
					Entity: "sample-app", InstallationID: "sample-app", Product: "app.example:sample-app", Version: "2", ReleaseDigest: "sha256:desired",
				}}},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{
				Environment: "dev", AgentID: "reporting-agent", ObservedAt: now, Entities: []controlplane.ReportedEntity{{
					InstallationID: "sample-app", Product: "app.example:sample-app", Version: "1", ReleaseDigest: "sha256:observed", ObservedAt: now,
				}},
			}); err != nil {
				t.Fatal(err)
			}
			store := newNotificationStore()
			ports, err := NewAnnouncements(store, allowRecipients{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			probe := &committedOpeningProbe{t: t, repository: repository, next: ports.ChangeRequestOpenings}
			key, err := controlplane.GenerateBundleSigningKey()
			if err != nil {
				t.Fatal(err)
			}
			bundleID := ""
			if surface == "offline-bundle" {
				state := reviewedState("announcement-bundle")
				state.Environment.ID = "dev"
				if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), state); err != nil {
					t.Fatal(err)
				}
				bundles, err := controlplane.NewBundleService(repository, key, nil)
				if err != nil {
					t.Fatal(err)
				}
				pipeline, err := bundles.PutPipeline(t.Context(), controlplane.ExportPipeline{
					ID: "notification-bundle", DisplayName: "Notification bundle", BuildStrategy: controlplane.BundleStrategySnapshot,
					Selections: []controlplane.BundleSelectionRule{{Type: controlplane.BundleSelectEnvironment, IDs: []string{"dev"}, ReleaseStrategy: controlplane.BundleReleasesAll}},
				})
				if err != nil {
					t.Fatal(err)
				}
				built, err := bundles.Build(t.Context(), pipeline.ID)
				if err != nil || len(built) != 1 {
					t.Fatalf("build signed bundle fixture: %#v, %v", built, err)
				}
				bundleID = built[0].Manifest.ID
			}
			handler, err := NewHub(repository, controlplane.HandlerOptions{
				AllowInsecureOperatorIdentityHeaders: true,
				BootstrapAdministrators:              []string{"local-developer"},
				Announcements:                        controlplane.Announcements{ChangeRequestOpenings: probe},
				BundlePrivateKey:                     key,
			}, Options{NotificationStore: store, PackagingStatus: runningPackaging{}})
			if err != nil {
				t.Fatal(err)
			}
			post := func(path, body string, status int) *httptest.ResponseRecorder {
				t.Helper()
				response := postAssistant(handler, "local-developer", "Operator", path, body)
				if response.Code != status {
					t.Fatalf("POST %s = %d: %s", path, response.Code, response.Body.String())
				}
				return response
			}
			switch surface {
			case "assistant":
				post("/v1/assistant/tools", `{"mode":"ontology","tool":"propose_governed_action","arguments":{"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}}`, http.StatusOK)
			case "intelligence":
				post("/v1/intelligence/courses/select", `{"Environment":"dev","InstallationID":"sample-app","ProductID":"app.example:sample-app","Value":"512Mi"}`, http.StatusOK)
			case "workspace":
				created := post("/v1/dev-workspaces", `{"profileId":"go-service","environmentId":"dev","title":"Reviewable workspace"}`, http.StatusCreated)
				var workspace struct {
					ID string `json:"workspaceId"`
				}
				if err := json.Unmarshal(created.Body.Bytes(), &workspace); err != nil || workspace.ID == "" {
					t.Fatalf("workspace receipt: %s, %v", created.Body.String(), err)
				}
				post("/v1/dev-workspaces/"+workspace.ID+"/submit", `{"title":"Review workspace","description":"Ready for review"}`, http.StatusAccepted)
			case "blackout":
				body, _ := json.Marshal(map[string]any{"action": "declare", "environment": "dev", "reason": "planned work", "startsAt": now, "endsAt": now.Add(time.Hour), "confirm": true})
				post("/v1/blackouts", string(body), http.StatusCreated)
				post("/v1/blackouts", `{"action":"clear","environment":"dev"}`, http.StatusAccepted)
			case "waves":
				post("/v1/rollouts", `{"action":"create","id":"rollout","product":"app.example:sample-app","version":"2","waves":[{"name":"first","environments":["dev"],"gate":"health"}]}`, http.StatusOK)
				post("/v1/rollouts", `{"action":"start","id":"rollout"}`, http.StatusAccepted)
			case "drift":
				post("/v1/fleet/propose-fix", `{"environment":"dev","installationId":"sample-app"}`, http.StatusCreated)
			case "offline-bundle":
				created := post("/v1/offline-bundles", `{"action":"handoff","bundleId":"`+bundleID+`","site":"dev","confirm":true}`, http.StatusCreated)
				var body struct {
					Handoff struct {
						ID string `json:"id"`
					} `json:"handoff"`
				}
				if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil || body.Handoff.ID == "" {
					t.Fatalf("handoff receipt: %s, %v", created.Body.String(), err)
				}
				post("/v1/offline-bundles", `{"action":"report","id":"`+body.Handoff.ID+`","outcome":"installed"}`, http.StatusAccepted)
			}
			if len(probe.openings) != 1 || len(store.events) != 1 {
				t.Fatalf("a persisted human proposal must emit once: calls=%#v events=%#v", probe.openings, store.events)
			}
			if event := store.events[0]; event.Type != domain.EventChangeRequestPendingApproval || event.Environment != "dev" || event.Actor != "local-developer" {
				t.Fatalf("wrong proposal event: %#v", event)
			}
		})
	}
}

func TestWorkspaceACPAnnouncesItsHighRiskReviewThroughConfiguredPorts(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	probe := &committedOpeningProbe{t: t, repository: repository}
	options := controlplane.HandlerOptions{Announcements: controlplane.Announcements{ChangeRequestOpenings: probe}}
	getenv := func(string) string { return "" }
	spaces := assembleDevWorkspace(repository, options, getenv)
	workspace, err := spaces.Create(t.Context(), devdomain.Caller{Username: "alice"}, "profile", "dev", "Reviewable workspace")
	if err != nil {
		t.Fatal(err)
	}
	service := assembleWorkspaceACP(repository, spaces, Options{WorkspaceACPBridges: memorybridge.NewPool()}, getenv, options)
	caller := acpdomain.Caller{Username: "alice"}
	session, err := service.NewSession(t.Context(), caller, workspace.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = service.Prompt(ctx, caller, workspace.ID, session.Branch, session.SessionID, "notification-run", 1,
		[]map[string]any{{"type": "text", "text": "run command: git push origin main"}},
		func(frame map[string]any) error {
			if frame["type"] != "permission_request" {
				return nil
			}
			gate := frame["request"].(acpdomain.PermissionGate)
			if gate.RiskTier != acpdomain.RiskHighChangeRequest || gate.ChangeRequestID == "" {
				t.Errorf("high risk command has no mandatory review: %#v", gate)
			}
			_, err := service.DecidePermission(ctx, caller, workspace.ID, session.Branch, gate.RequestID, session.SessionID, "notification-run", "deny", "")
			return err
		})
	if err != nil || len(probe.openings) != 1 || probe.openings[0].TargetKind != "WORKSPACE_ACP" {
		t.Fatalf("ACP review notification: openings=%#v error=%v", probe.openings, err)
	}
}

func TestCallerAnnouncementPortSurvivesWithoutANotificationStore(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	probe := &committedOpeningProbe{t: t, repository: repository}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		Announcements:                        controlplane.Announcements{ChangeRequestOpenings: probe},
	}, Options{PackagingStatus: runningPackaging{}})
	if err != nil {
		t.Fatal(err)
	}
	response := postAssistant(handler, "local-developer", "Operator", "/v1/assistant/tools", `{"mode":"ontology","tool":"propose_governed_action","arguments":{"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}}`)
	if response.Code != http.StatusOK || len(probe.openings) != 1 {
		t.Fatalf("an optional emitter failure must not undo committed work: status=%d body=%s openings=%#v", response.Code, response.Body.String(), probe.openings)
	}
}
