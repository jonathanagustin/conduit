package composition

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// reviewedHub is a Hub whose Plan proposals wait for the reviewers team, over
// a notification store, with nothing passed for Announcements: NewHub is what
// assembles them, and this is what proves it does.
func reviewedHub(t *testing.T) (http.Handler, *notificationStore) {
	t.Helper()
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, controlplane.Team{
		Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, controlplane.ApprovalPolicy{
		ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`,
	}); err != nil {
		t.Fatal(err)
	}
	store := newNotificationStore()
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store
}

func reviewedState(generation string) controlplane.DesiredState {
	state := recallingState(generation)
	state.Recalls = nil
	return state
}

// #840, end to end: a Plan proposed through the assembled Hub that waits for an
// approver puts an entry in the inbox of the person who subscribed to that
// Environment, without internal/controlplane learning what a notification is.
func TestAProposalWaitingForApprovalReachesTheInboxOfItsSubscriber(t *testing.T) {
	t.Parallel()
	handler, store := reviewedHub(t)
	approver := domain.Owner{Kind: domain.PrincipalUser, ID: "reviewer"}
	if err := store.SavePreference(context.Background(), domain.Preference{
		ID: "npref_reviewer", Owner: approver, ResourceRID: apolloapplication.EnvironmentRID("dev-use2"),
		Channel: domain.ChannelInApp, Types: []domain.EventType{domain.EventChangeRequestPendingApproval},
	}); err != nil {
		t.Fatal(err)
	}
	propose(t, handler, reviewedState("0123456789abcdef"))

	if len(store.events) != 1 {
		t.Fatalf("a proposal waiting for approval recorded %d events: %#v", len(store.events), store.events)
	}
	event := store.events[0]
	if event.Type != domain.EventChangeRequestPendingApproval {
		t.Errorf("raised as %q", event.Type)
	}
	if event.ResourceRID != apolloapplication.EnvironmentRID("dev-use2") {
		t.Errorf("the event names %q, which is not the identifier a preference joins on", event.ResourceRID)
	}
	if event.Actor != "local-developer" || !strings.Contains(event.Summary, "waiting for approval") {
		t.Errorf("the event does not say who proposed or what is wanted: %#v", event)
	}
	if len(store.entries) != 1 || store.entries[0].Recipient.Owner != approver ||
		store.entries[0].Recipient.Channel != domain.ChannelInApp {
		t.Fatalf("the subscribed approver was not told: %#v", store.entries)
	}

	// The same desired state proposed again is the same request at the same
	// revision, and the approver is not told twice.
	propose(t, handler, reviewedState("0123456789abcdef"))
	if len(store.events) != 1 || len(store.entries) != 1 {
		t.Fatalf("a restated proposal was announced again: %#v", store.events)
	}

	response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+event.ResourceRID, "local-developer", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "CHANGE_REQUEST_PENDING_APPROVAL") {
		t.Errorf("the event is not in what the surface serves: %d %s", response.Code, response.Body.String())
	}
}

// A grant written through the assembled Hub is a ROLE_CHANGE on the resource
// it names.
func TestAGrantWrittenThroughTheHubBecomesARoleChangeEvent(t *testing.T) {
	t.Parallel()
	handler, store := reviewedHub(t)
	response := request(t, handler, http.MethodPost, "/v1/resource-grants", "local-developer",
		`{"resourceRid":"environment/dev-use2","team":"reviewers","roles":["environment:viewer"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("write grant = %d %s", response.Code, response.Body.String())
	}
	if len(store.events) != 1 {
		t.Fatalf("a grant write recorded %d events: %#v", len(store.events), store.events)
	}
	event := store.events[0]
	if event.Type != domain.EventRoleChange || event.ResourceRID != "environment/dev-use2" ||
		event.Actor != "local-developer" || !strings.Contains(event.Summary, "environment:viewer") {
		t.Errorf("the grant change is not described: %#v", event)
	}
}

// No store, no ports: the zero value is the whole of "nobody to tell", and a
// caller that supplied a port of its own keeps it.
func TestAnnouncementsAreAssembledOnlyOverAStoreAndNeverOverwriteACallers(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	none, err := NewAnnouncements(nil, logger)
	if err != nil || none != (controlplane.Announcements{}) {
		t.Fatalf("a deployment with no store got %#v, %v", none, err)
	}
	if _, err := NewAnnouncements(newNotificationStore(), nil); err == nil {
		t.Error("announcements were assembled with no logger to report a dropped error to")
	}
	assembled, err := NewAnnouncements(newNotificationStore(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if assembled.ChangeRequestOpenings == nil || assembled.GrantChanges == nil ||
		assembled.PlanFailures == nil || assembled.PromotionFailures == nil {
		t.Fatalf("a deployment with a store is missing a port: %#v", assembled)
	}
	theirs := loggedAnnouncements{logger: logger}
	kept, err := withAnnouncements(controlplane.HandlerOptions{
		Announcements: controlplane.Announcements{PlanFailures: theirs},
	}, newNotificationStore(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Announcements.PlanFailures != theirs {
		t.Error("a port the caller supplied was replaced")
	}
	if kept.Announcements.ChangeRequestOpenings == nil || kept.Announcements.PromotionFailures == nil ||
		kept.Announcements.GrantChanges == nil {
		t.Error("the ports the caller did not supply were not filled in")
	}
}

// The error is dropped where the announcement is made, so the wrapper is the
// only place a broken store is heard. A request with no Environment is routine
// and must not be logged as a failure.
func TestADroppedAnnouncementErrorIsLogged(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelInfo}))
	announcements, err := NewAnnouncements(failingNotificationStore{newNotificationStore()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := announcements.PlanFailures.PlanFailed(ctx, planFailure()); err == nil {
		t.Fatal("fixture: the failing store did not fail")
	}
	if !strings.Contains(log.String(), "level=ERROR") || !strings.Contains(log.String(), "announce failed plan") {
		t.Errorf("a failed announcement was not logged as an error: %q", log.String())
	}
	log.Reset()
	if err := announcements.ChangeRequestOpenings.AwaitingApproval(ctx, catalogueOpening()); err == nil {
		t.Fatal("fixture: a request with no Environment was announced")
	}
	if strings.Contains(log.String(), "level=ERROR") || !strings.Contains(log.String(), "change request not announced") {
		t.Errorf("a routine unannounceable request was logged wrongly: %q", log.String())
	}
}
