package conduitcli

import (
	"bytes"
	"encoding/json"
	discussionfile "github.com/jonathanagustin/conduit/internal/changeactivity/adapters/filestore"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChangeActivityCLIUsesHubAuthorityAndSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	repository, err := controlplane.NewFileRepository(filepath.Join(directory, "core"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := discussionfile.New(filepath.Join(directory, "discussion"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "activity-fixture", DisplayName: "Activity fixture", Enabled: true, Expression: `approve("fixture")`}); err != nil {
		t.Fatal(err)
	}
	bytesState, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(bytesState, &state); err != nil {
		t.Fatal(err)
	}
	author, err := governance.ResolvePrincipal(t.Context(), controlplane.IdentityClaims{Username: "author"})
	if err != nil {
		t.Fatal(err)
	}
	_, request, err := governance.ProposePlan(t.Context(), state, author, "CLI activity", "")
	if err != nil || request.Status != controlplane.ChangeApproved {
		t.Fatal(request.Status, request.Error, err)
	}
	definition, _, err := controlplane.NewLabelDefinitionService(repository).Prepare(t.Context(), controlplane.LabelDefinition{ID: "priority", Description: "Priority", SuggestedResourceTypes: []string{"change-request"}, AcceptedValues: []string{"present"}}, "author", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	definition.ChangeRequestID = "crq_definition_fixture"
	if _, err := repository.PutLabelDefinition(t.Context(), definition); err != nil {
		t.Fatal(err)
	}
	var commentReads atomic.Int32
	makeServer := func() *httptest.Server {
		handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bootstrap"}}, composition.Options{ChangeDiscussion: store})
		if err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
				commentReads.Add(1)
			}
			handler.ServeHTTP(w, r)
		}))
	}
	server := makeServer()
	t.Cleanup(func() { server.Close() })
	run := func(subject string, args ...string) (string, error) {
		var output, diagnostics bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &diagnostics, Getenv: func(string) string { return "" }})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", subject}, args...))
		err := root.Execute()
		return output.String(), err
	}
	before, err := repository.GetChangeRequest(t.Context(), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforePlan, err := repository.GetPlan(t.Context(), request.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := run("author", "changerequest", "comment", request.ID, "-c", "Discussion only.\nNo approval is given.")
	if err != nil {
		t.Fatal(err)
	}
	var first activity.Comment
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}
	if first.RequestID != request.ID || first.Author != "author" || first.Actor != "author" || first.Text != "Discussion only.\nNo approval is given." || first.CreatedAt.IsZero() || first.ValueTier != "CONDUIT_NATIVE" {
		t.Fatal("comment metadata lost", out)
	}
	after, err := repository.GetChangeRequest(t.Context(), request.ID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("discussion changed review state", err)
	}
	afterPlan, err := repository.GetPlan(t.Context(), request.PlanID)
	if err != nil || !reflect.DeepEqual(afterPlan, beforePlan) {
		t.Fatal("discussion approved or changed Plan", err)
	}
	if out, err := run("intruder", "changerequest", "comment", request.ID, "--comment", "untrusted"); err == nil || out != "" {
		t.Fatal("unauthorized comment succeeded", out, err)
	}
	for _, args := range [][]string{{"changerequest", "comment", request.ID}, {"changerequest", "comment", request.ID, "extra", "-c", "text"}, {"changerequest", "label", "add", request.ID}} {
		if out, err := run("author", args...); err == nil || out != "" {
			t.Fatal("missing or excess arguments accepted", args, out, err)
		}
	}
	_, err = run("author", "changerequest", "label", "add", request.ID, "--label-name", "priority")
	if err != nil {
		t.Fatal(err)
	}
	out, err = run("author", "changerequest", "label", "add", request.ID, "--label-name", "Do not merge")
	if err != nil {
		t.Fatal(err)
	}
	var held activity.LabelMutation
	if err := json.Unmarshal([]byte(out), &held); err != nil {
		t.Fatal(err)
	}
	if held.ResultingStatus != controlplane.ChangePendingApprovals || held.PlanStatus != string(controlplane.PlanAwaitingApproval) || len(held.Labels) != 2 {
		t.Fatal("label body or hold lost", out)
	}
	heldRequest, _ := repository.GetChangeRequest(t.Context(), request.ID)
	heldPlan, _ := repository.GetPlan(t.Context(), request.PlanID)
	heldAudit, _ := repository.ListAuthorizationAudit(t.Context(), request.ID)
	if _, err := run("author", "changerequest", "comment", request.ID, "-c", "Discussion while approval is held"); err != nil {
		t.Fatal(err)
	}
	stillHeld, _ := repository.GetChangeRequest(t.Context(), request.ID)
	stillHeldPlan, _ := repository.GetPlan(t.Context(), request.PlanID)
	if !reflect.DeepEqual(stillHeld, heldRequest) || !reflect.DeepEqual(stillHeldPlan, heldPlan) {
		t.Fatal("discussion cleared a hold or changed approval")
	}

	out, err = run("author", "changerequest", "label", "remove", request.ID, "--label-name", "Do not merge", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	var preview activity.LabelMutation
	if err := json.Unmarshal([]byte(out), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Mode != "PREVIEW" || preview.ResultingStatus != controlplane.ChangeApproved || !preview.ApprovalTransition {
		t.Fatal(out)
	}
	actualRequest, _ := repository.GetChangeRequest(t.Context(), request.ID)
	actualPlan, _ := repository.GetPlan(t.Context(), request.PlanID)
	actualAudit, _ := repository.ListAuthorizationAudit(t.Context(), request.ID)
	if !reflect.DeepEqual(actualRequest, heldRequest) || !reflect.DeepEqual(actualPlan, heldPlan) || !reflect.DeepEqual(actualAudit, heldAudit) {
		t.Fatal("dry-run mutated request, Plan or audit")
	}
	if out, err := run("author", "changerequest", "label", "add", request.ID, "--label-name", "unknown"); err == nil || out != "" {
		t.Fatal("unknown label accepted", out, err)
	}
	out, err = run("author", "changerequest", "label", "remove", request.ID, "--label-name", "Do not merge")
	if err != nil {
		t.Fatal(err)
	}
	var released activity.LabelMutation
	if err := json.Unmarshal([]byte(out), &released); err != nil {
		t.Fatal(err)
	}
	if released.Mode != "APPLIED" || released.ResultingStatus != controlplane.ChangeApproved || !released.ApprovalTransition || len(released.Labels) != 1 || released.Labels[0] != "priority" {
		t.Fatal("hold removal lost unrelated label or approval", out)
	}
	for i := range activity.MaxPageSize*2 + 1 {
		if _, err := run("author", "changerequest", "comment", request.ID, "--comment", strings.Repeat("x", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	server.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = controlplane.NewFileRepository(filepath.Join(directory, "core"))
	if err != nil {
		t.Fatal(err)
	}
	store, err = discussionfile.New(filepath.Join(directory, "discussion"))
	if err != nil {
		t.Fatal(err)
	}
	server = makeServer()
	beforeReads := commentReads.Load()
	out, err = run("author", "changerequest", "comments", request.ID)
	if err != nil {
		t.Fatal(err)
	}
	var comments []activity.Comment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		t.Fatal(err)
	}
	foundFirst := false
	for _, comment := range comments {
		if comment.ID == first.ID && comment == first {
			foundFirst = true
		}
	}
	if len(comments) != activity.MaxPageSize*2+3 || !foundFirst || commentReads.Load()-beforeReads < 3 {
		t.Fatal("restart or paginated reading lost comments", len(comments), commentReads.Load()-beforeReads)
	}
	if out, err := run("intruder", "changerequest", "comments", request.ID); err == nil || out != "" {
		t.Fatal("discussion reader gained access", out, err)
	}
	// Closed requests remain discussable by their author, without a Plan or a review.
	other := controlplane.ChangeRequest{ID: "crq_closed_discussion", Author: controlplane.Principal{Username: "other"}, Environment: "other", Status: controlplane.ChangeRejected, Revision: 1, CreatedAt: time.Now().UTC()}
	if _, err := repository.CreateChangeRequest(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := run("other", "changerequest", "comment", other.ID, "-c", "Follow-up after rejection"); err != nil {
		t.Fatal(err)
	}
	if out, err := run("author", "changerequest", "comments", other.ID); err == nil || out != "" {
		t.Fatal("cross-request discussion disclosure", out, err)
	}
}
