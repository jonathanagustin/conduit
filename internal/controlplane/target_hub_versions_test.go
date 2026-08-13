package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTargetHubVersionsHTTPRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "edge-pipeline", DisplayName: "Edge", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{{Type: BundleSelectRelease, IDs: []string{"app.example:example-api:0.1.0"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "dev-use2", Entities: []ReportedEntity{{
			InstallationID: "example-api", Product: "app.example:example-api", Version: "0.1.0",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	exported := httptest.NewRecorder()
	handler.ServeHTTP(exported, httptest.NewRequest(http.MethodGet,
		"/v1/environments/dev-use2/target-hub-versions", nil))
	if exported.Code != http.StatusOK {
		t.Fatalf("export = %d %s", exported.Code, exported.Body.String())
	}
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, httptest.NewRequest(http.MethodPut,
		"/v1/export-pipelines/edge-pipeline/target-hub-versions/edge", bytes.NewReader(exported.Body.Bytes())))
	if recorded.Code != http.StatusOK {
		t.Fatalf("record = %d %s", recorded.Code, recorded.Body.String())
	}
	pipeline, err := repository.GetExportPipeline(ctx, "edge-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if got := pipeline.TargetHubVersions["edge"].Entities; len(got) != 1 || got[0].Version != "0.1.0" {
		t.Fatalf("recorded versions = %#v", got)
	}
}

// The payload a target Hub produces must be pasteable verbatim into the source,
// so both the bare installation list and the wrapping record are accepted.
func TestDecodeTargetHubVersionsAcceptsEitherShape(t *testing.T) {
	t.Parallel()
	entities := []TargetHubEntityVersion{{
		Environment: "dev-use2", InstallationID: "example-api",
		Product: "app.example:example-api", Version: "0.1.0",
	}}
	list, err := json.Marshal(entities)
	if err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(TargetHubVersions{TargetHub: "edge", Entities: entities})
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{"bare list": list, "full record": record} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decoded, err := DecodeTargetHubVersions(payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded) != 1 || decoded[0].Version != "0.1.0" {
				t.Fatalf("decoded = %#v", decoded)
			}
		})
	}
}

// Bad target knowledge is worse than none: it would silently shrink a Bundle the
// target then cannot apply. Every field is validated.
func TestDecodeTargetHubVersionsRejectsUnusablePayloads(t *testing.T) {
	t.Parallel()
	valid := TargetHubEntityVersion{
		Environment: "dev-use2", InstallationID: "example-api",
		Product: "app.example:example-api", Version: "0.1.0",
	}
	for name, testCase := range map[string]struct {
		payload   string
		wantError string
	}{
		"empty payload":    {payload: "   ", wantError: "payload is empty"},
		"malformed json":   {payload: "{oh no", wantError: "decode record"},
		"no installations": {payload: "[]", wantError: "at least one installation"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeTargetHubVersions([]byte(testCase.payload)); err == nil ||
				!strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.wantError)
			}
		})
	}

	for name, mutate := range map[string]func(TargetHubEntityVersion) TargetHubEntityVersion{
		"invalid environment": func(v TargetHubEntityVersion) TargetHubEntityVersion { v.Environment = "Not A Label"; return v },
		"blank installation":  func(v TargetHubEntityVersion) TargetHubEntityVersion { v.InstallationID = " "; return v },
		"invalid product":     func(v TargetHubEntityVersion) TargetHubEntityVersion { v.Product = "not-a-coordinate"; return v },
		"inexact version":     func(v TargetHubEntityVersion) TargetHubEntityVersion { v.Version = "latest"; return v },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal([]TargetHubEntityVersion{mutate(valid)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeTargetHubVersions(payload); err == nil {
				t.Fatal("an unusable installation was accepted")
			}
		})
	}

	// A duplicate installation is contradictory: it would claim one installation
	// runs two versions.
	payload, err := json.Marshal([]TargetHubEntityVersion{valid, valid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTargetHubVersions(payload); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v, want a duplicate rejection", err)
	}
}

// The target's own report is the export side of the exchange, so what one Hub
// produces is what the other can record.
func TestEnvironmentEntityVersionsRoundTripsIntoARecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "dev-use2", AgentID: "agent-1",
		Entities: []ReportedEntity{
			{InstallationID: "example-api", Product: "app.example:example-api", Version: "0.1.0", Health: HealthHealthy},
			// An installation with no version yet is not knowledge to carry across.
			{InstallationID: "pending", Product: "app.example:example-workspace", Health: HealthUnknown},
		},
	}); err != nil {
		t.Fatal(err)
	}
	versions, err := service.EnvironmentEntityVersions(ctx, "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].InstallationID != "example-api" {
		t.Fatalf("versions = %#v, want only the installed Entity", versions)
	}
	payload, err := json.Marshal(versions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTargetHubVersions(payload); err != nil {
		t.Fatalf("a Hub's own export was not decodable as target knowledge: %v", err)
	}
}

// Recording replaces rather than merges: an installation the target no longer
// reports is gone, and merging would leave the source believing otherwise forever.
func TestRecordTargetHubVersionsReplacesPriorKnowledge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "edge-pipeline", DisplayName: "Edge", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	first := []TargetHubEntityVersion{
		{Environment: "dev-use2", InstallationID: "example-api", Product: "app.example:example-api", Version: "0.1.0"},
		{Environment: "dev-use2", InstallationID: "retired", Product: "app.example:example-workspace", Version: "0.1.0"},
	}
	if _, err := service.RecordTargetHubVersions(ctx, "edge-pipeline", "edge", first); err != nil {
		t.Fatal(err)
	}
	second := []TargetHubEntityVersion{first[0]}
	pipeline, err := service.RecordTargetHubVersions(ctx, "edge-pipeline", "edge", second)
	if err != nil {
		t.Fatal(err)
	}
	recorded := pipeline.TargetHubVersions["edge"]
	if len(recorded.Entities) != 1 || recorded.Entities[0].InstallationID != "example-api" {
		t.Fatalf("recorded = %#v, want the retired installation dropped", recorded.Entities)
	}
	if recorded.RecordedAt.IsZero() {
		t.Fatal("record carries no timestamp")
	}

	// Knowledge is scoped per target Hub, so recording one does not affect another.
	pipeline, err = service.RecordTargetHubVersions(ctx, "edge-pipeline", "other", first)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline.TargetHubVersions["edge"].Entities) != 1 || len(pipeline.TargetHubVersions["other"].Entities) != 2 {
		t.Fatalf("per-target knowledge leaked: %#v", pipeline.TargetHubVersions)
	}

	if _, err := service.RecordTargetHubVersions(ctx, "edge-pipeline", " ", first); err == nil {
		t.Fatal("a blank target Hub was accepted")
	}
	invalid := []TargetHubEntityVersion{{
		Environment: "Not A Label", InstallationID: "example-api",
		Product: "app.example:example-api", Version: "0.1.0",
	}}
	if _, err := service.RecordTargetHubVersions(ctx, "edge-pipeline", "edge", invalid); err == nil {
		t.Fatal("direct recording bypassed target-version validation")
	}
}

// A Release the target already runs need not be re-sent, but nothing else may be
// inferred from that: a running version says nothing about which Teams or
// Environment settings the target holds.
func TestTargetHubBaselineOnlySuppressesKnownReleases(t *testing.T) {
	t.Parallel()
	releases := map[string]Release{
		"app.example:example-api:0.1.0":       {Product: "app.example:example-api", Version: "0.1.0"},
		"app.example:example-workspace:0.1.0": {Product: "app.example:example-workspace", Version: "0.1.0"},
	}
	baseline := map[string]BundleResource{}
	recorded := TargetHubVersions{TargetHub: "edge", Entities: []TargetHubEntityVersion{{
		Environment: "dev-use2", InstallationID: "example-api",
		Product: "app.example:example-api", Version: "0.1.0",
	}}}
	result := targetHubReleaseBaseline(baseline, releases, recorded)
	if _, suppressed := result[BundleSelectRelease+":app.example:example-api:0.1.0"]; !suppressed {
		t.Fatalf("a Release the target runs was not baselined: %#v", result)
	}
	if _, wrong := result[BundleSelectRelease+":app.example:example-workspace:0.1.0"]; wrong {
		t.Fatalf("a Release the target does not run was baselined: %#v", result)
	}
	for key := range result {
		if !strings.HasPrefix(key, BundleSelectRelease+":") {
			t.Fatalf("baseline inferred a non-Release resource: %s", key)
		}
	}

	// With no recorded knowledge the baseline is untouched.
	if got := targetHubReleaseBaseline(map[string]BundleResource{}, releases, TargetHubVersions{}); len(got) != 0 {
		t.Fatalf("empty knowledge produced a baseline: %#v", got)
	}
}
