//go:build ignore

// Command seed fills a running Hub with enough real state for the console to
// have something to render, and does it through the Hub's own HTTP API.
//
// Why this is Go rather than a JSON document under testdata: the desired state
// is built from controlplane's exported types, so a field this simulation feeds
// the console cannot be renamed in the Go source without breaking this build.
// A checked-in JSON blob would go on parsing quietly while the console rendered
// an empty screen, which is the failure mode web/src/test/e2e/harness.ts warns
// about in its own comment -- those tests stub /v1 and so establish that the
// console renders correctly GIVEN a response, never that the Hub produces it.
//
// It is deliberately small. One environment, two Releases with a dependency
// between them, and the entities that install them is enough to produce a Plan,
// a Change Request, fleet installations and an overview -- every screen the
// console has a landing page for. Adding fixtures here to make a screen busier
// buys nothing the stubbed suite does not already buy more cheaply.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The digest is a placeholder, but it has to be a syntactically valid one: the
// Hub validates artifact references before it will accept a desired state, so a
// simulation that fed it nonsense would be refused rather than seeded.
const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func artifact(name string) controlplane.Artifact {
	return controlplane.Artifact{
		Repository: "registry.example/" + name, Tag: "0.1.0", Digest: digest,
	}
}

// assessed marks a Release as scanned and passing. Without it the environment's
// ArtifactSecurityPolicy refuses to plan an install at all, so the simulation
// would seed an environment with no Plan and the console would correctly show
// nothing -- a green run that proved the console can render emptiness.
func assessed() *controlplane.VulnerabilityAssessment {
	return &controlplane.VulnerabilityAssessment{
		Provider: "simulation-scanner", RunID: "scan-simulation", Status: "COMPLETE",
		Outcome: "PASS", ArtifactDigests: []string{digest}, AssessedAt: time.Now().UTC(),
	}
}

func desiredState() controlplane.DesiredState {
	return controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: controlplane.Environment{
			ID: "dev-use2", Generation: "0123456789abcdef",
			ArtifactSecurityPolicy: &controlplane.ArtifactSecurityPolicy{RequiredOutcome: "PASS"},
		},
		Releases: []controlplane.Release{
			{
				Product: "app.example:example-api", Version: "0.1.0",
				Chart: artifact("example-api"), Images: []controlplane.Artifact{artifact("example-api")},
				VulnerabilityAssessment: assessed(),
			},
			{
				Product: "app.example:example-workspace", Version: "0.1.0",
				Chart: artifact("example-workspace"), Images: []controlplane.Artifact{artifact("example-workspace")},
				Dependencies: []controlplane.Dependency{
					{Product: "app.example:example-api", MinimumVersion: "0.1.0", MaximumVersion: "0.1.999"},
				},
				VulnerabilityAssessment: assessed(),
			},
		},
		Entities: []controlplane.Entity{
			{Name: "example-workspace", Product: "app.example:example-workspace", Version: "0.1.0", Namespace: "example"},
			{Name: "example-api", Product: "app.example:example-api", Version: "0.1.0", Namespace: "example"},
		},
	}
}

// The Hub is on the loopback interface, so an ambient egress proxy must not be
// consulted for it. A zero Transport would honour the environment's proxy
// settings and the request would never arrive.
var client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{}}

func send(base, subject, method, path string, payload any) []byte {
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	request, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", subject)
	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "seed: the Hub refused %s %s: %d %s\n", method, path, response.StatusCode, answer)
		os.Exit(1)
	}
	return answer
}

func main() {
	base := os.Getenv("CONDUIT_HUB_URL")
	if base == "" {
		fmt.Fprintln(os.Stderr, "seed: CONDUIT_HUB_URL is required")
		os.Exit(2)
	}
	subject := os.Getenv("CONDUIT_SUBJECT")
	if subject == "" {
		subject = "operator"
	}

	// Order matters. A Change Request is evaluated once, when the Plan is
	// proposed, so a policy installed afterwards would never be consulted and
	// the request would sit in ERROR reading "no enabled approval policies" --
	// which is what the first version of this simulation produced. The console
	// then showed a Plan AWAITING_APPROVAL beside an Overview counting zero
	// awaiting approval, and both were telling the truth.
	// The reviewer is whoever the browser will act as, and it is deliberately
	// not the subject seeding this: the Hub refuses the author of a Change
	// Request approving it, so a simulation that proposed and approved as one
	// person could never exercise an approval at all.
	reviewer := os.Getenv("CONDUIT_REVIEWER")
	if reviewer == "" {
		reviewer = "operator"
	}
	send(base, subject, http.MethodPut, "/v1/teams/reviewers", controlplane.Team{
		Name: "reviewers", DisplayName: "Reviewers",
		AdminUsers: []string{reviewer}, MemberUsers: []string{reviewer},
	})
	// The requirement is what makes the governance path real rather than
	// decorative: it puts the Change Request in PENDING_APPROVALS with a second
	// party genuinely outstanding, so the console has something to approve and
	// the author -- who proposed it -- correctly cannot.
	send(base, subject, http.MethodPut, "/v1/approval-policies/simulation", controlplane.ApprovalPolicy{
		ID: "simulation", DisplayName: "Simulated deployment review", Enabled: true,
		Expression: `require_team("reviewers", "a second party reviews deployments")`,
	})

	answer := send(base, subject, http.MethodPost, "/v1/plans", desiredState())
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answer, &created); err != nil || created.ID == "" {
		fmt.Fprintf(os.Stderr, "seed: the Hub accepted the desired state but returned no Plan: %s\n", answer)
		os.Exit(1)
	}
	// Printed rather than asserted: the harness reads it, so a test can look for
	// the identifier the Hub minted instead of hard-coding a digest that changes
	// whenever the seed does.
	fmt.Println(created.ID)
}
