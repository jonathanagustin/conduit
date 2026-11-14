package conduitcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
)

func membershipCLIFixture(t *testing.T) (*controlplane.FileRepository, controlplane.DesiredState, string, string) {
	t.Helper()
	directory := t.TempDir()
	repository, err := controlplane.NewFileRepository(filepath.Join(directory, "core"))
	if err != nil {
		t.Fatal(err)
	}
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), controlplane.Team{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "platform/root", Team: "writers", Operations: []string{controlplane.OperationProposeChange, controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "membership", DisplayName: "Membership", Enabled: true, Expression: `type == "CONDUIT_NATIVE_UPDATE_RELEASE_CHANNEL_MEMBERSHIP" ? require_team("writers", "membership review") : approve("existing workload")`}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/dev-use2", Team: "writers", Operations: []string{controlplane.OperationProposeChange, controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	for i := range state.Entities {
		state.Entities[i].ReleaseChannel = ""
		state.Entities[i].Version = "0.1.0"
	}
	state.ReleaseChannels = []controlplane.ReleaseChannelDefinition{{Name: "STABLE", Description: "Preserve this definition"}, {Name: "CANARY"}}
	for i := range state.Releases {
		state.Releases[i].Channels = []string{"STABLE"}
	}
	if failures := controlplane.Validate(state); len(failures) > 0 {
		t.Fatal(failures)
	}
	author, err := service.ResolvePrincipal(t.Context(), controlplane.IdentityClaims{Username: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ProposePlan(t.Context(), state, author, "Baseline", ""); err != nil {
		t.Fatal(err)
	}
	path := writeJSONFile(t, directory, "desired.json", state)
	return repository, state, path, directory
}

func membershipEditor(t *testing.T, directory, body string) string {
	t.Helper()
	path := filepath.Join(directory, "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMembershipCLI(t *testing.T, server, editor, subject string, args ...string) (string, error) {
	t.Helper()
	var output, diagnostics bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &diagnostics, Stdin: strings.NewReader(""), Getenv: func(name string) string {
		if name == "EDITOR" {
			return editor
		}
		return ""
	}})
	root.SetArgs(append([]string{"--server", server, "--development-subject", subject}, args...))
	err := root.Execute()
	return output.String(), err
}

func TestMembershipEditorCancellationAndEmptyListAreDistinct(t *testing.T) {
	for _, test := range []struct {
		name, script       string
		cancelled, invalid bool
	}{
		{"unchanged", "exit 0", true, false},
		{"empty-document", ": > \"$1\"", true, false},
		{"comment-only", "printf '# no proposal\\n' > \"$1\"", true, false},
		{"nonzero-before-validation", "printf '[invalid yaml' > \"$1\"; exit 7", true, false},
		{"malformed-saved", "printf '[invalid yaml' > \"$1\"", false, true},
		{"empty-list", "printf 'releaseChannels: []\\nrationale: CAB-42\\n' > \"$1\"", false, false},
		{"bare-empty-list", "printf '[]\\n' > \"$1\"", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, state, path, directory := membershipCLIFixture(t)
			handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
			if err != nil {
				t.Fatal(err)
			}
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					writes.Add(1)
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			before, err := repository.ListPlans(t.Context(), state.Environment.ID)
			if err != nil {
				t.Fatal(err)
			}
			coordinate := state.Releases[0].Product + ":" + state.Releases[0].Version
			output, err := runMembershipCLI(t, server.URL, membershipEditor(t, directory, test.script), "writer", "product-release", "release-channel", "edit", coordinate, "--state-file", path, "--next-generation", "reviewed-membership-edit")
			if test.invalid {
				if err == nil {
					t.Fatal("invalid saved document accepted", output)
				}
			} else if err != nil {
				t.Fatal(output, err)
			}
			after, err := repository.ListPlans(t.Context(), state.Environment.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.cancelled || test.invalid {
				if writes.Load() != 0 || !reflect.DeepEqual(before, after) {
					t.Fatal("cancelled editor mutated", writes.Load())
				}
				if test.cancelled && !strings.Contains(output, `"cancelled": true`) {
					t.Fatal(output)
				}
				return
			}
			if writes.Load() != 1 || len(after) != 2 || !strings.Contains(output, `"submitted": true`) {
				t.Fatal("empty list was cancelled", output)
			}
			var proposed controlplane.PlanRecord
			for _, p := range after {
				if p.ID != before[0].ID {
					proposed = p
				}
			}
			if proposed.DesiredState == nil || len(proposed.DesiredState.Releases[0].Channels) != 0 || !reflect.DeepEqual(proposed.DesiredState.ReleaseChannels, state.ReleaseChannels) {
				t.Fatal("membership/definition wrong", proposed)
			}
			request, err := repository.GetChangeRequest(t.Context(), proposed.ChangeRequestID)
			if err != nil || request.Status != controlplane.ChangePendingApprovals {
				t.Fatal(request.Status, err)
			}
			if test.name == "empty-list" && request.Description != "CAB-42" {
				t.Fatal("rationale lost", request.Description)
			}
			reopened, err := controlplane.NewFileRepository(filepath.Join(directory, "core"))
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := reopened.GetChangeRequest(t.Context(), request.ID)
			if err != nil || persisted.Description != request.Description {
				t.Fatal("restart lost rationale", err)
			}
		})
	}
}

func TestMembershipRemovalMergesCoordinatesAndPreservesReviewAndArtifacts(t *testing.T) {
	repository, state, path, directory := membershipCLIFixture(t)
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	first, second := state.Releases[0].Product+":"+state.Releases[0].Version, state.Releases[1].Product+":"+state.Releases[1].Version
	file := filepath.Join(directory, "coordinates.yaml")
	if err := os.WriteFile(file, []byte("- "+first+"\n- "+second+"\n- "+second+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"release-channel", "remove", "STABLE", "--state-file", path, "--next-generation", "reviewed-membership-remove", "--maven-coordinate", first, "--maven-coordinate", first, "-f", file, "--rationale", "Ticket CAB-99\nReviewed removals"}
	before, _ := repository.ListPlans(t.Context(), state.Environment.ID)
	for _, subject := range []string{"intruder", "writer"} {
		preview, err := runMembershipCLI(t, server.URL, "", subject, append(append([]string{}, args...), "--dry-run")...)
		if err != nil || !strings.Contains(preview, `"submitted": false`) {
			t.Fatal(preview, err)
		}
	}
	after, _ := repository.ListPlans(t.Context(), state.Environment.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("dry run wrote")
	}
	if output, err := runMembershipCLI(t, server.URL, "", "intruder", args...); err == nil || output != "" {
		t.Fatal("unauthorized proposal", output, err)
	}
	bad := append(append([]string{}, args...), "--maven-coordinate", "missing:release:1.2.3")
	if output, err := runMembershipCLI(t, server.URL, "", "writer", bad...); err == nil || output != "" {
		t.Fatal("unknown coordinate", output, err)
	}
	output, err := runMembershipCLI(t, server.URL, "", "writer", args...)
	if err != nil {
		t.Fatal(output, err)
	}
	var response struct {
		Coordinates []string                `json:"mavenCoordinates"`
		Plan        controlplane.PlanRecord `json:"plan"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Coordinates) != 2 || response.Coordinates[0] != first || response.Coordinates[1] != second {
		t.Fatal("coordinates not merged/deduplicated", output)
	}
	request, err := repository.GetChangeRequest(t.Context(), response.Plan.ChangeRequestID)
	if err != nil || request.Description != "Ticket CAB-99\nReviewed removals" || len(request.Requirements) != 2 {
		t.Fatal("review metadata lost", request, err)
	}
	for _, release := range response.Plan.DesiredState.Releases {
		if len(release.Channels) != 0 {
			t.Fatal("membership retained")
		}
	}
	immutable := *response.Plan.DesiredState
	immutable.Environment.Generation = state.Environment.Generation
	for i := range immutable.Releases {
		immutable.Releases[i].Channels = state.Releases[i].Channels
	}
	if !reflect.DeepEqual(immutable, state) {
		t.Fatal("artifact or definition mutated")
	}
	if output, err := runMembershipCLI(t, server.URL, "", "writer", args...); err == nil || output != "" {
		t.Fatal("stale source accepted", output, err)
	}
}

func TestMembershipEditorRetainsFlagRationaleAndDryRunIsOffline(t *testing.T) {
	repository, state, path, directory := membershipCLIFixture(t)
	before, err := repository.ListPlans(t.Context(), state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	editor := membershipEditor(t, directory, "if grep -q '^rationale:' \"$1\"; then exit 9; fi\nprintf 'releaseChannels: [STABLE, CANARY, CANARY]\\n' > \"$1\"")
	rationale := "Ticket CAB-123\nKeep multiline context"
	output, err := runMembershipCLI(t, "http://127.0.0.1:1", editor, "writer", "product-release", "release-channel", "edit", state.Releases[0].Product+":"+state.Releases[0].Version, "--state-file", path, "--next-generation", "reviewed-edit-preview", "--rationale", rationale, "--dry-run")
	if err != nil {
		t.Fatal(output, err)
	}
	var preview struct {
		Submitted    bool                      `json:"submitted"`
		DesiredState controlplane.DesiredState `json:"desiredState"`
		Review       struct {
			Rationale string `json:"rationale"`
			Digest    string `json:"baseDesiredStateDigest"`
		} `json:"review"`
	}
	if err := json.Unmarshal([]byte(output), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Submitted || preview.Review.Rationale != rationale || preview.Review.Digest != before[0].DesiredStateDigest || !reflect.DeepEqual(preview.DesiredState.Releases[0].Channels, []string{"CANARY", "STABLE"}) {
		t.Fatal("preview lost edited membership or rationale", output)
	}
	after, err := repository.ListPlans(t.Context(), state.Environment.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("offline preview mutated", err)
	}
}

func TestMembershipInputDocumentsAreStrictAndBounded(t *testing.T) {
	for _, contents := range []string{"releaseChannels: []\nunknown: value\n", "releaseChannels: []\n---\nreleaseChannels: [STABLE]\n", "releaseChannels: null\n", "rationale: missing-list\n"} {
		if _, _, empty, err := parseMembershipEdit([]byte(contents)); err == nil || empty {
			t.Fatalf("invalid saved document accepted: %q", contents)
		}
	}
	var coordinates []string
	for _, contents := range []string{"one: coordinate\n", "- app:one:1.2.3\n---\n- app:two:1.2.3\n"} {
		if decodeMembershipYAML([]byte(contents), &coordinates) == nil {
			t.Fatal("invalid coordinate list accepted", contents)
		}
	}
	path := filepath.Join(t.TempDir(), "large.yaml")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maximumMembershipDocument+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMembershipFile(path); err == nil {
		t.Fatal("unbounded input accepted")
	}
}
