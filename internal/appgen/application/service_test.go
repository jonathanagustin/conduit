package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

func TestRecordedGenerationPassesGatesAndAudits(t *testing.T) {
	sandbox := &fakeSandbox{build: BuildResult{Passed: true, Reason: "image built without network", ImageID: "sha256:image"}, health: HealthResult{Passed: true, Reason: "GET /health returned 200"}}
	publisher := &fakePublisher{ref: "127.0.0.1:5000/hello-http:0.1.0", image: "sha256:" + strings.Repeat("ab", 32), chart: "sha256:" + strings.Repeat("cd", 32)}
	handoff := &fakeHandoff{}
	audit := &fakeAudit{}
	store := &Memory{}
	service := Service{
		Generator: recorded.Adapter{}, Sandbox: sandbox, Publish: publisher, Handoff: handoff, Audit: audit, Store: store,
		Now: func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) },
	}
	result, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Label != domain.LabelNoModel {
		t.Fatalf("label = %s", result.Label)
	}
	if result.ImageDigest != publisher.image || result.ChartDigest != publisher.chart || result.RunID != "hello-http-1" {
		t.Fatalf("release = %#v", result)
	}
	if !strings.Contains(result.PackagingSource, result.ImageDigest) || !strings.Contains(handoff.source, "name: hello-http") {
		t.Fatal("packaging source was not handed to the existing flow")
	}
	for _, gate := range result.Gates {
		if gate.Outcome != domain.OutcomePass || gate.Reason == "" {
			t.Fatalf("gate = %#v", gate)
		}
	}
	if len(result.Gates) != 6 {
		t.Fatalf("gates = %#v", result.Gates)
	}
	if audit.event.Adapter != "recorded" || len(audit.event.PromptSHA256) != 64 || len(audit.event.ResponseSHA256) != 64 {
		t.Fatalf("audit provenance = %#v", audit.event)
	}
	if audit.event.Files["main.go"] == "" || audit.event.Files["Dockerfile"] == "" || !audit.event.BuildPassed || !audit.event.HealthPassed {
		t.Fatalf("audit files = %#v", audit.event)
	}
	if !strings.Contains(fileContent(result, "Dockerfile"), "FROM scratch") || strings.Contains(fileContent(result, "Dockerfile"), "package main") {
		t.Fatal("template was not rendered separately from the source")
	}
	stored, ok, err := store.Get(context.Background(), result.ID)
	if err != nil || !ok || stored.Label != domain.LabelNoModel {
		t.Fatalf("stored = %#v %v %v", stored, ok, err)
	}
}

func TestSecretScanStopsBeforeTheSandbox(t *testing.T) {
	spec := recordedSpec(t)
	spec.Files[0].Content += "\npassword=hunter2\n"
	sandbox := &fakeSandbox{build: BuildResult{Passed: true, Reason: "should not run"}}
	service := Service{Generator: &stubGen{spec: spec, prov: domain.Provenance{Adapter: "recorded", PromptSHA256: strings.Repeat("a", 64), ResponseSHA256: strings.Repeat("b", 64)}}, Sandbox: sandbox, Audit: &fakeAudit{}}
	result, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.builds != 0 {
		t.Fatal("sandbox ran after a secret finding")
	}
	found := false
	for _, gate := range result.Gates {
		if gate.Name == GateSecrets && gate.Outcome == domain.OutcomeFail && strings.Contains(gate.Reason, "credential") {
			found = true
		}
		if gate.Name == GateBuild && gate.Outcome != domain.OutcomeSkip {
			t.Fatalf("build gate = %#v", gate)
		}
	}
	if !found {
		t.Fatalf("gates = %#v", result.Gates)
	}
}

func TestBuildAndHealthFailuresStayOnTheirGates(t *testing.T) {
	spec := recordedSpec(t)
	gen := &stubGen{spec: spec, prov: domain.Provenance{Adapter: "codex", PromptSHA256: strings.Repeat("c", 64), ResponseSHA256: strings.Repeat("d", 64)}}
	publisher := &fakePublisher{}
	buildFail := Service{Generator: gen, Sandbox: &fakeSandbox{build: BuildResult{Reason: "compile failed"}}, Publish: publisher, Audit: &fakeAudit{}}
	result, err := buildFail.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil || result.Label != domain.LabelLiveModel || publisher.calls != 0 {
		t.Fatalf("build = %#v %v calls %d", result, err, publisher.calls)
	}
	if gateOutcome(result, GateBuild) != domain.OutcomeFail || gateOutcome(result, GateHealth) != domain.OutcomeSkip {
		t.Fatalf("gates = %#v", result.Gates)
	}
	healthFail := Service{Generator: gen, Sandbox: &fakeSandbox{build: BuildResult{Passed: true, Reason: "built", ImageID: "local"}, health: HealthResult{Reason: "health timed out"}}, Publish: publisher, Audit: &fakeAudit{}}
	result, err = healthFail.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil || publisher.calls != 0 || gateOutcome(result, GateHealth) != domain.OutcomeFail {
		t.Fatalf("health = %#v %v", result, err)
	}
}

func TestMissingFieldsAndConfirmDoNotCallTheGenerator(t *testing.T) {
	gen := &stubGen{}
	service := Service{Generator: gen}
	missing, err := service.Generate(context.Background(), Input{})
	if err != nil || len(missing.Needs) != 2 || missing.Label != domain.LabelNoModel {
		t.Fatalf("needs = %#v %v", missing, err)
	}
	held, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page"})
	if err != nil || !held.ConfirmRequired || gen.calls != 0 {
		t.Fatalf("confirm = %#v calls %d %v", held, gen.calls, err)
	}
}

func TestAllowlistFailureIsAGate(t *testing.T) {
	spec := recordedSpec(t)
	spec.Files[0].Path = "Dockerfile"
	spec.Files[0].Language = "go"
	service := Service{Generator: &stubGen{spec: spec, prov: domain.Provenance{Adapter: "recorded"}}, Sandbox: &fakeSandbox{build: BuildResult{Passed: true}}, Audit: &fakeAudit{}}
	result, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if gateOutcome(result, GateAllowlist) != domain.OutcomeFail || gateOutcome(result, GateSecrets) != domain.OutcomeSkip {
		t.Fatalf("gates = %#v", result.Gates)
	}
}

func recordedSpec(t *testing.T) domain.Spec {
	t.Helper()
	spec, _, err := (recorded.Adapter{}).Generate(context.Background(), domain.Request{Name: "hello-http", Description: "a small page"})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func gateOutcome(result Result, name string) string {
	for _, gate := range result.Gates {
		if gate.Name == name {
			return gate.Outcome
		}
	}
	return ""
}

func fileContent(result Result, path string) string {
	for _, file := range result.Files {
		if file.Path == path {
			return file.Content
		}
	}
	return ""
}

type stubGen struct {
	spec  domain.Spec
	prov  domain.Provenance
	err   error
	calls int
}

func (gen *stubGen) Generate(context.Context, domain.Request) (domain.Spec, domain.Provenance, error) {
	gen.calls++
	return gen.spec, gen.prov, gen.err
}

type fakeSandbox struct {
	build  BuildResult
	health HealthResult
	builds int
}

func (sandbox *fakeSandbox) Build(context.Context, BuildRequest) (BuildResult, error) {
	sandbox.builds++
	return sandbox.build, nil
}

func (sandbox *fakeSandbox) Check(context.Context, BuildResult, BuildRequest) (HealthResult, error) {
	return sandbox.health, nil
}

type fakePublisher struct {
	ref, image, chart string
	calls             int
	err               error
}

func (publisher *fakePublisher) Push(context.Context, string, string, []byte) (string, string, string, error) {
	publisher.calls++
	return publisher.ref, publisher.image, publisher.chart, publisher.err
}

type fakeHandoff struct {
	source string
}

func (handoff *fakeHandoff) Prepare(_ context.Context, _ string, source []byte) error {
	handoff.source = string(source)
	return nil
}

type fakeAudit struct {
	event Audit
	err   error
}

func (audit *fakeAudit) Record(_ context.Context, event Audit) error {
	audit.event = event
	return audit.err
}

type denyLog struct{ events []Denial }

func (log *denyLog) Deny(_ context.Context, event Denial) error {
	log.events = append(log.events, event)
	return nil
}

func TestAnotherOperatorAndAnUnownedRowAreHidden(t *testing.T) {
	store := &Memory{}
	denials := &denyLog{}
	service := Service{
		Generator: recorded.Adapter{},
		Sandbox:   &fakeSandbox{build: BuildResult{Passed: true, Reason: "built", ImageID: "local"}, health: HealthResult{Passed: true, Reason: "ok"}},
		Audit:     &fakeAudit{},
		Access:    denials,
		Store:     store,
		Now:       func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) },
	}
	created, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"})
	if err != nil || created.Owner != "ada" || created.ID == "" {
		t.Fatalf("created = %#v %v", created, err)
	}
	if err := store.Save(context.Background(), Result{ID: "gen_legacy", Name: "legacy", Description: "old secret", Owner: ""}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "ada", "bea"} {
		listed, err := service.List(context.Background(), actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listed {
			if item.ID == "gen_legacy" || item.Description == "old secret" {
				t.Fatalf("%s listed an unowned generation: %#v", actor, listed)
			}
		}
		if _, ok, err := service.Get(context.Background(), actor, "gen_legacy"); err != nil || ok {
			t.Fatalf("%s legacy get = %v %v", actor, ok, err)
		}
	}
	ada, err := service.List(context.Background(), "ada")
	if err != nil || len(ada) != 1 || ada[0].ID != created.ID {
		t.Fatalf("ada list = %#v %v", ada, err)
	}
	if _, ok, err := service.Get(context.Background(), "ada", created.ID); err != nil || !ok {
		t.Fatalf("ada get = %v %v", ok, err)
	}
	bea, err := service.List(context.Background(), "bea")
	if err != nil || len(bea) != 0 {
		t.Fatalf("bea list = %#v %v", bea, err)
	}
	if _, ok, err := service.Get(context.Background(), "bea", created.ID); err != nil || ok {
		t.Fatalf("bea get = %v %v", ok, err)
	}
	var sawBea bool
	for _, event := range denials.events {
		if event.Actor == "bea" && event.ID == created.ID && event.Action == ActionRead {
			sawBea = true
		}
	}
	if !sawBea {
		t.Fatalf("denials = %#v", denials.events)
	}
}

func TestAuditErrorIsReturned(t *testing.T) {
	service := Service{
		Generator: recorded.Adapter{},
		Sandbox:   &fakeSandbox{build: BuildResult{Passed: true, Reason: "built"}, health: HealthResult{Passed: true, Reason: "ok"}},
		Audit:     &fakeAudit{err: errors.New("audit store unavailable")},
	}
	if _, err := service.Generate(context.Background(), Input{Name: "hello-http", Description: "a small page", Confirm: true, Actor: "ada"}); err == nil {
		t.Fatal("audit failure was ignored")
	}
}

func TestGenerationRefusesAPlanDigestThatDoesNotMatchTheText(t *testing.T) {
	generator := &stubGen{}
	service := Service{Generator: generator}
	_, err := service.Generate(context.Background(), Input{
		Actor: "ada", Name: "hello-http", Description: "a small page", Confirm: true,
		PlanRevisionID: "ps_accepted", PlanDigest: strings.Repeat("ab", 32), AcceptedPlan: `{"summary":"catalog export"}`,
	})
	if !errors.Is(err, domain.ErrAcceptedPlanDigest) {
		t.Fatalf("error = %v", err)
	}
	if generator.calls != 0 {
		t.Fatalf("generator calls = %d", generator.calls)
	}
	digest := domain.AcceptedPlanDigest(`{"summary":"catalog export"}`)
	generator.err = errors.New("captured")
	_, err = service.Generate(context.Background(), Input{
		Actor: "ada", Name: "hello-http", Description: "a small page", Confirm: true,
		PlanRevisionID: "ps_accepted", PlanDigest: digest, AcceptedPlan: `{"summary":"catalog export"}`,
		Conversation: []domain.ConversationMessage{{Role: "operator", Text: "catalog export"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("matching digest did not reach generation: %d", generator.calls)
	}
}
