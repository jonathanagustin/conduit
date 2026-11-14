package controlplane

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func TestApolloEnvironmentSettingsValidationHTTPAdapter(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/v1/apollo/environment-settings/validate", bytes.NewBufferString(settingsFixture)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"normalized"`) {
		t.Fatalf("validation response = %d %s", response.Code, response.Body.String())
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost,
		"/v1/apollo/environment-settings/validate", bytes.NewBufferString("managed-helm-charts: [")))
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid response = %d %s", invalid.Code, invalid.Body.String())
	}
}

const settingsFixture = `release-channel: RELEASE
global-maintenance-windows:
  downtime: all-time
  no-downtime: all-time
managed-helm-charts:
  - entity-locator:
      name: wordpress
    k8s-namespace: namespace2
    product: com.bitnami.repos:wordpress
  - entity-locator:
      name: keycloak
    k8s-namespace: customer-namespace
    product: com.bitnami.repos:keycloak
    release-channel: DEV
`

// The published document is the shape an operator edits and CEL policies are
// written against, so the documented example must decode field for field.
func TestDecodeApolloEnvironmentSettingsReadsThePublishedShape(t *testing.T) {
	t.Parallel()
	settings, err := DecodeApolloEnvironmentSettings(settingsFixture)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ReleaseChannel != "RELEASE" {
		t.Fatalf("release channel = %q", settings.ReleaseChannel)
	}
	if settings.GlobalMaintenanceWindows == nil ||
		settings.GlobalMaintenanceWindows.Downtime != "all-time" ||
		settings.GlobalMaintenanceWindows.NoDowntime != "all-time" {
		t.Fatalf("global windows = %#v", settings.GlobalMaintenanceWindows)
	}
	if len(settings.ManagedHelmCharts) != 2 {
		t.Fatalf("charts = %d, want 2", len(settings.ManagedHelmCharts))
	}
	wordpress := settings.ManagedHelmCharts[0]
	if wordpress.EntityLocator.Name != "wordpress" || wordpress.KubernetesNamespace != "namespace2" ||
		wordpress.Product != "com.bitnami.repos:wordpress" || wordpress.ReleaseChannel != "" {
		t.Fatalf("wordpress = %#v", wordpress)
	}
	// The documented example exists to show an Entity-level channel overriding the
	// Environment default, so that distinction must survive decoding.
	if settings.ManagedHelmCharts[1].ReleaseChannel != "DEV" {
		t.Fatalf("keycloak channel = %q, want the Entity-level override", settings.ManagedHelmCharts[1].ReleaseChannel)
	}
}

// A field this adapter does not model must survive a round trip. Dropping it would
// destroy settings an operator legitimately wrote.
func TestApolloEnvironmentSettingsPreservesUnmodelledFields(t *testing.T) {
	t.Parallel()
	document := settingsFixture + "some-future-setting:\n  nested: value\n"
	settings, err := DecodeApolloEnvironmentSettings(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeApolloEnvironmentSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, "some-future-setting") || !strings.Contains(encoded, "nested") {
		t.Fatalf("round trip dropped an unmodelled field:\n%s", encoded)
	}
}

func TestDecodeApolloEnvironmentSettingsRejectsContradictions(t *testing.T) {
	t.Parallel()
	for name, document := range map[string]string{
		"empty document":       "   ",
		"malformed yaml":       "managed-helm-charts: [oh no",
		"unnamed installation": "managed-helm-charts:\n  - entity-locator:\n      name: ''\n",
		// Two entries locating the same installation leave nothing to decide which wins.
		"duplicate installation": "managed-helm-charts:\n" +
			"  - entity-locator:\n      name: api\n    k8s-namespace: ns\n" +
			"  - entity-locator:\n      name: api\n    k8s-namespace: ns\n",
		"duplicate locator in different namespaces": "managed-helm-charts:\n" +
			"  - entity-locator:\n      name: api\n    k8s-namespace: ns-a\n" +
			"  - entity-locator:\n      name: api\n    k8s-namespace: ns-b\n",
		"invalid product coordinate": "managed-helm-charts:\n" +
			"  - entity-locator:\n      name: api\n    product: not-a-coordinate\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeApolloEnvironmentSettings(document); err == nil {
				t.Fatal("an unusable settings document was accepted")
			}
		})
	}
}

// An omitted field means unchanged. Importing a partial document must not clear
// settings the operator never mentioned.
func TestApplyApolloEnvironmentSettingsOnlyAppliesStatedFields(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Entities[0].Namespace = "original"
	state.Entities[0].HelmReleaseName = "kept-release-name"

	document := "managed-helm-charts:\n" +
		"  - entity-locator:\n      name: example-workspace\n    k8s-namespace: moved\n"
	settings, err := DecodeApolloEnvironmentSettings(document)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyApolloEnvironmentSettings(state, settings)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Entities[0].Namespace != "moved" {
		t.Fatalf("namespace = %q, want the stated value", applied.Entities[0].Namespace)
	}
	if applied.Entities[0].HelmReleaseName != "kept-release-name" {
		t.Fatalf("helm release name = %q, want it untouched", applied.Entities[0].HelmReleaseName)
	}
	// The source state is not mutated in place.
	if state.Entities[0].Namespace != "original" {
		t.Fatalf("apply mutated the input state: %q", state.Entities[0].Namespace)
	}

	// The Environment default fills in only where an installation stated nothing.
	defaulted, err := DecodeApolloEnvironmentSettings(
		"release-channel: RELEASE\nmanaged-helm-charts:\n" +
			"  - entity-locator:\n      name: example-workspace\n" +
			"  - entity-locator:\n      name: example-api\n    release-channel: DEV\n")
	if err != nil {
		t.Fatal(err)
	}
	applied, err = ApplyApolloEnvironmentSettings(state, defaulted)
	if err != nil {
		t.Fatal(err)
	}
	channels := map[string]string{}
	for _, entity := range applied.Entities {
		channels[entity.Name] = entity.ReleaseChannel
	}
	if channels["example-workspace"] != "RELEASE" || channels["example-api"] != "DEV" {
		t.Fatalf("channels = %#v", channels)
	}

	// Settings naming an installation the Environment does not declare are a
	// contradiction, not a silent no-op.
	unknown, err := DecodeApolloEnvironmentSettings(
		"managed-helm-charts:\n  - entity-locator:\n      name: ghost\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyApolloEnvironmentSettings(state, unknown); err == nil {
		t.Fatal("settings for an undeclared installation were applied")
	}
}

// A policy that permits only "dependency-stack-overrides" must match the same path
// here that its author wrote upstream, so diff paths use the document's vocabulary
// and "/" separators.
func TestApolloSettingsDiffPathsUseTheDocumentVocabulary(t *testing.T) {
	t.Parallel()
	before, err := DecodeApolloEnvironmentSettings(settingsFixture)
	if err != nil {
		t.Fatal(err)
	}

	stackChange := cloneJSON(before)
	stackChange.ManagedHelmCharts[0].DependencyStackOverrides = map[string]string{"com.bitnami.repos:redis": "green"}
	paths := ApolloSettingsDiffPaths(before, stackChange)
	want := SettingsFieldManagedHelmCharts + "/wordpress/" + SettingsFieldDependencyStacks
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("paths = %#v, want exactly %q", paths, want)
	}
	// The published allowlist pattern from the documentation must match it.
	if !governance.PathMatchesAnyPattern(paths[0], []string{SettingsFieldManagedHelmCharts + "/**"}) {
		t.Fatalf("path %q is not matched by the documented glob", paths[0])
	}
	// A narrower policy that only permits a different field must not match.
	if governance.PathMatchesAnyPattern(paths[0], []string{SettingsFieldManagedHelmCharts + "/*/" + SettingsFieldStack}) {
		t.Fatalf("path %q matched an unrelated field pattern", paths[0])
	}

	channelChange := cloneJSON(before)
	channelChange.ReleaseChannel = "STABLE"
	if paths := ApolloSettingsDiffPaths(before, channelChange); len(paths) != 1 || paths[0] != SettingsFieldReleaseChannel {
		t.Fatalf("paths = %#v, want the top-level release channel", paths)
	}

	// Adding and removing an installation both report the installation root.
	added := cloneJSON(before)
	added.ManagedHelmCharts = append(added.ManagedHelmCharts, ApolloManagedHelmChart{
		EntityLocator: ApolloEntityLocator{Name: "redis"},
	})
	if paths := ApolloSettingsDiffPaths(before, added); len(paths) != 1 ||
		paths[0] != SettingsFieldManagedHelmCharts+"/redis" {
		t.Fatalf("added paths = %#v", paths)
	}
	removed := cloneJSON(before)
	removed.ManagedHelmCharts = removed.ManagedHelmCharts[:1]
	if paths := ApolloSettingsDiffPaths(before, removed); len(paths) != 1 ||
		paths[0] != SettingsFieldManagedHelmCharts+"/keycloak" {
		t.Fatalf("removed paths = %#v", paths)
	}

	// An unchanged document changes nothing.
	if paths := ApolloSettingsDiffPaths(before, cloneJSON(before)); len(paths) != 0 {
		t.Fatalf("identical documents reported %#v", paths)
	}

	// Preserved future fields are still security-relevant: an allowlist must see
	// them change even though this adapter does not model their values yet.
	unknownBefore, err := DecodeApolloEnvironmentSettings(settingsFixture + "future-setting:\n  enabled: false\n")
	if err != nil {
		t.Fatal(err)
	}
	unknownAfter, err := DecodeApolloEnvironmentSettings(settingsFixture + "future-setting:\n  enabled: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if paths := ApolloSettingsDiffPaths(unknownBefore, unknownAfter); len(paths) != 1 || paths[0] != "future-setting" {
		t.Fatalf("unmodelled change paths = %#v, want future-setting", paths)
	}
}

// Desired state projects into the document shape, so what Platform holds can be
// expressed in the vocabulary policies and operators use.
func TestDesiredStateProjectsIntoTheSettingsDocument(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Entities[0].HelmReleaseName = "helm-chart-workspace"
	state.Entities[0].Stack = "blue"
	state.Entities[0].DependencyStackOverrides = map[string]string{"app.example:example-api": "green"}
	state.Entities[0].IgnoredDependencies = []string{"app.example:optional"}

	settings := ApolloEnvironmentSettingsFromDesiredState(state)
	if len(settings.ManagedHelmCharts) != len(state.Entities) {
		t.Fatalf("charts = %d, want %d", len(settings.ManagedHelmCharts), len(state.Entities))
	}
	chart := settings.ManagedHelmCharts[0]
	if chart.EntityLocator.Name != "example-workspace" || chart.HelmReleaseName != "helm-chart-workspace" ||
		chart.Stack != "blue" || chart.DependencyStackOverrides["app.example:example-api"] != "green" ||
		len(chart.IgnoredDependencies) != 1 {
		t.Fatalf("chart = %#v", chart)
	}
	encoded, err := EncodeApolloEnvironmentSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		SettingsFieldManagedHelmCharts, SettingsFieldEntityLocator, SettingsFieldHelmReleaseName,
		SettingsFieldStack, SettingsFieldDependencyStacks, SettingsFieldIgnoredDependencies,
	} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("encoded document omits %q:\n%s", field, encoded)
		}
	}
}

// A Helm release name becomes part of the Kubernetes object names Helm creates,
// which is why it is bounded at 53 rather than the 63 a DNS label allows -- the
// remainder is headroom for the suffixes Helm appends. The same bound is
// enforced twice, on the DesiredState Entity and on the imported settings
// document, and neither edge was pinned: mutation testing moved this one and
// nothing failed.
func TestHelmReleaseNameBoundIsExactOnBothValidators(t *testing.T) {
	t.Parallel()
	name := func(length int) string {
		// Leading letter keeps it a valid DNS label at every length.
		return "a" + strings.Repeat("b", length-1)
	}

	for _, test := range []struct {
		length    int
		wantError bool
	}{
		{length: 53},
		{length: 54, wantError: true},
	} {
		t.Run(fmt.Sprintf("settings/%d", test.length), func(t *testing.T) {
			t.Parallel()
			errs := validateApolloEnvironmentSettings(ApolloEnvironmentSettings{
				ManagedHelmCharts: []ApolloManagedHelmChart{{
					EntityLocator:   ApolloEntityLocator{Name: "example-api"},
					HelmReleaseName: name(test.length),
				}},
			})
			if got := len(errs) > 0; got != test.wantError {
				t.Fatalf("length %d rejected=%v, want %v (%v)", test.length, got, test.wantError, errs)
			}
		})

		t.Run(fmt.Sprintf("desired-state/%d", test.length), func(t *testing.T) {
			t.Parallel()
			state := foundation()
			state.Entities[0].HelmReleaseName = name(test.length)
			rejected := false
			for _, err := range Validate(state) {
				if strings.Contains(err.Path, "helmReleaseName") {
					rejected = true
				}
			}
			if rejected != test.wantError {
				t.Fatalf("length %d rejected=%v, want %v", test.length, rejected, test.wantError)
			}
		})
	}
}

// addUnknownDiffPaths exists so a document cannot change a setting this version
// does not model while diff_only_modifies_paths reports no change -- an
// approval policy scoped to a path list would otherwise approve an edit it
// never saw. The round-trip test proves those fields survive encoding; nothing
// proved they show up in a diff, and mutation testing moved the comparison
// without failing anything.
func TestUnmodelledSettingsChangesAreVisibleToTheDiff(t *testing.T) {
	t.Parallel()
	decode := func(t *testing.T, extra string) ApolloEnvironmentSettings {
		t.Helper()
		settings, err := DecodeApolloEnvironmentSettings(settingsFixture + extra)
		if err != nil {
			t.Fatal(err)
		}
		return settings
	}

	base := decode(t, "some-future-setting:\n  nested: value\n")

	// Unchanged: the diff must stay silent, or every reconcile of an untouched
	// document would look like a change and defeat a path-scoped policy.
	if paths := ApolloSettingsDiffPaths(base, decode(t, "some-future-setting:\n  nested: value\n")); len(paths) != 0 {
		t.Errorf("an unchanged unmodelled setting reported %#v", paths)
	}

	for _, test := range []struct {
		name  string
		after string
	}{
		{name: "value changed", after: "some-future-setting:\n  nested: other\n"},
		{name: "key removed", after: ""},
		{name: "sibling added", after: "some-future-setting:\n  nested: value\nanother-future-setting: true\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			paths := ApolloSettingsDiffPaths(base, decode(t, test.after))
			if len(paths) == 0 {
				t.Fatalf("%s produced no diff path; a path-scoped policy would approve it unseen", test.name)
			}
			// The reported path must be the document's own key, so an allowlist
			// written against the document vocabulary can match it.
			matched := false
			for _, path := range paths {
				if strings.HasPrefix(path, "some-future-setting") || strings.HasPrefix(path, "another-future-setting") {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("paths %#v do not name the unmodelled key", paths)
			}
		})
	}
}

// "Only applies stated fields" is a data-loss property, not a convenience: an
// unstated field must leave the Environment's existing value alone. Drop or
// invert one of those guards and applying a settings document that never
// mentions a field silently clears it. The existing test proves this for the
// Helm release name; the guards for stack, ignored dependencies and dependency
// stack overrides were unpinned, and mutation testing moved all three.
func TestApplyApolloEnvironmentSettingsDoesNotClearUnstatedFields(t *testing.T) {
	t.Parallel()
	populated := func() DesiredState {
		state := foundation()
		entity := &state.Entities[0]
		entity.Namespace = "original"
		entity.HelmReleaseName = "kept-release-name"
		entity.ReleaseChannel = "RELEASE"
		entity.Version = ""
		entity.Stack = "green"
		entity.IgnoredDependencies = []string{"com.example:legacy"}
		entity.DependencyStackOverrides = map[string]string{"com.example:database": "blue"}
		return state
	}

	// A document that names the installation and states nothing else.
	settings, err := DecodeApolloEnvironmentSettings(
		"managed-helm-charts:\n  - entity-locator:\n      name: example-workspace\n")
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyApolloEnvironmentSettings(populated(), settings)
	if err != nil {
		t.Fatal(err)
	}
	entity := applied.Entities[0]
	if entity.Stack != "green" {
		t.Errorf("stack = %q, want it untouched", entity.Stack)
	}
	if len(entity.IgnoredDependencies) != 1 || entity.IgnoredDependencies[0] != "com.example:legacy" {
		t.Errorf("ignored dependencies = %#v, want them untouched", entity.IgnoredDependencies)
	}
	if len(entity.DependencyStackOverrides) != 1 || entity.DependencyStackOverrides["com.example:database"] != "blue" {
		t.Errorf("dependency stack overrides = %#v, want them untouched", entity.DependencyStackOverrides)
	}
	if entity.Namespace != "original" || entity.HelmReleaseName != "kept-release-name" {
		t.Errorf("namespace/release name = %q/%q, want them untouched", entity.Namespace, entity.HelmReleaseName)
	}

	// And each of those fields is applied when the document does state it, so
	// the guards cannot be satisfied by never writing at all.
	stated, err := DecodeApolloEnvironmentSettings(
		"managed-helm-charts:\n  - entity-locator:\n      name: example-workspace\n" +
			"    stack: blue\n" +
			"    ignored-dependencies: [com.example:other]\n" +
			"    dependency-stack-overrides:\n      com.example:database: red\n")
	if err != nil {
		t.Fatal(err)
	}
	applied, err = ApplyApolloEnvironmentSettings(populated(), stated)
	if err != nil {
		t.Fatal(err)
	}
	entity = applied.Entities[0]
	if entity.Stack != "blue" {
		t.Errorf("stated stack = %q, want blue", entity.Stack)
	}
	if len(entity.IgnoredDependencies) != 1 || entity.IgnoredDependencies[0] != "com.example:other" {
		t.Errorf("stated ignored dependencies = %#v", entity.IgnoredDependencies)
	}
	if entity.DependencyStackOverrides["com.example:database"] != "red" {
		t.Errorf("stated dependency stack overrides = %#v", entity.DependencyStackOverrides)
	}
}

// The optional maintenance settings are compared through nil-tolerant helpers
// whose guards do two jobs: they decide whether a change reaches the diff a CEL
// policy reads, and they are what stops a nil dereference when only one side is
// stated. Neither job was tested -- the decode test reads global windows but
// never diffs them, and mutation testing moved both guards.
func TestOptionalMaintenanceSettingsDiffWithoutDereferencingNil(t *testing.T) {
	t.Parallel()
	windows := func(downtime, noDowntime string) *ApolloMaintenanceWindows {
		return &ApolloMaintenanceWindows{Downtime: downtime, NoDowntime: noDowntime}
	}
	flag := func(value bool) *bool { return &value }
	contains := func(paths []string, want string) bool {
		for _, path := range paths {
			if path == want {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name       string
		before     ApolloEnvironmentSettings
		after      ApolloEnvironmentSettings
		wantPath   string
		wantChange bool
	}{
		// Nil on one side is the case that dereferences if the guard is wrong.
		{
			name:     "windows stated for the first time",
			after:    ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "")},
			wantPath: SettingsFieldGlobalMaintenance, wantChange: true,
		},
		{
			name:     "windows withdrawn",
			before:   ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "")},
			wantPath: SettingsFieldGlobalMaintenance, wantChange: true,
		},
		{
			name:     "windows unchanged",
			before:   ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "")},
			after:    ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "")},
			wantPath: SettingsFieldGlobalMaintenance,
		},
		{
			name:     "windows edited",
			before:   ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "")},
			after:    ApolloEnvironmentSettings{GlobalMaintenanceWindows: windows("all-time", "all-time")},
			wantPath: SettingsFieldGlobalMaintenance, wantChange: true,
		},
		{name: "both absent", wantPath: SettingsFieldGlobalMaintenance},
		// The override flag distinguishes unset from an explicit false, so nil
		// against false is a real change rather than two ways of saying no.
		{
			name:     "override stated as false",
			after:    ApolloEnvironmentSettings{OverrideProductMaintenanceWindows: flag(false)},
			wantPath: SettingsFieldOverrideProductWind, wantChange: true,
		},
		{
			name:     "override flipped",
			before:   ApolloEnvironmentSettings{OverrideProductMaintenanceWindows: flag(false)},
			after:    ApolloEnvironmentSettings{OverrideProductMaintenanceWindows: flag(true)},
			wantPath: SettingsFieldOverrideProductWind, wantChange: true,
		},
		{
			name:     "override unchanged",
			before:   ApolloEnvironmentSettings{OverrideProductMaintenanceWindows: flag(true)},
			after:    ApolloEnvironmentSettings{OverrideProductMaintenanceWindows: flag(true)},
			wantPath: SettingsFieldOverrideProductWind,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			paths := ApolloSettingsDiffPaths(test.before, test.after)
			if got := contains(paths, test.wantPath); got != test.wantChange {
				t.Fatalf("%s reported=%v, want %v (paths=%#v)", test.wantPath, got, test.wantChange, paths)
			}
		})
	}

	// Rendering is the other half of the same nil handling. An Environment with
	// no windows must render none rather than an empty pair, or every such
	// Environment encodes a window it never stated -- and one that states only
	// half the pair must keep that half, or the stated window is dropped on the
	// way into the document.
	absent := ApolloEnvironmentSettingsFromDesiredState(foundation())
	if absent.GlobalMaintenanceWindows != nil {
		t.Fatalf("absent windows rendered as %#v, want nil", absent.GlobalMaintenanceWindows)
	}

	downtimeOnly := foundation()
	downtimeOnly.Environment.Maintenance.Downtime = weeklyWindow("MONDAY/10:00-MONDAY/12:00")
	rendered := ApolloEnvironmentSettingsFromDesiredState(downtimeOnly)
	if rendered.GlobalMaintenanceWindows == nil {
		t.Fatal("an Environment stating only a downtime window rendered no windows at all")
	}
	if rendered.GlobalMaintenanceWindows.Downtime == "" {
		t.Fatalf("the stated downtime window was dropped: %#v", rendered.GlobalMaintenanceWindows)
	}
	if rendered.GlobalMaintenanceWindows.NoDowntime != "" {
		t.Fatalf("an unstated no-downtime window was invented: %#v", rendered.GlobalMaintenanceWindows)
	}
}

// A maintenance window with no intervals means unrestricted, which Apollo
// spells "all-time" -- not "never". Getting that inversion wrong turns an
// Environment that may be changed at any time into one that may never be, or
// the reverse, and the document round-trips through both directions.
func TestMaintenanceWindowsRoundTripThroughTheAllTimeLiteral(t *testing.T) {
	t.Parallel()

	// Rendering: nil states nothing, empty intervals state unrestricted, and a
	// list states itself.
	if got := weeklyWindowToApollo(nil); got != "" {
		t.Errorf("a nil window rendered %q, want the field omitted", got)
	}
	if got := weeklyWindowToApollo(&WeeklyMaintenanceWindow{TimeZone: "UTC"}); got != apolloAllTime {
		t.Errorf("an unrestricted window rendered %q, want %q", got, apolloAllTime)
	}
	restricted := &WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"MONDAY/10:00-MONDAY/12:00"}}
	if got := weeklyWindowToApollo(restricted); got != "MONDAY/10:00-MONDAY/12:00" {
		t.Errorf("a restricted window rendered %q", got)
	}

	// Parsing back: the literal is unrestricted, an empty string is unstated,
	// and a list is preserved. The two must agree, or a settings document
	// changes meaning every time it is read and written.
	if window := apolloWindowToWeekly(""); window != nil {
		t.Errorf("an absent window parsed to %#v, want nil", window)
	}
	unrestricted := apolloWindowToWeekly(apolloAllTime)
	if unrestricted == nil || len(unrestricted.Intervals) != 0 {
		t.Errorf("%q parsed to %#v, want a window with no intervals", apolloAllTime, unrestricted)
	}
	if got := weeklyWindowToApollo(unrestricted); got != apolloAllTime {
		t.Errorf("unrestricted did not survive a round trip: %q", got)
	}
	parsed := apolloWindowToWeekly("MONDAY/10:00-MONDAY/12:00,FRIDAY/22:00-SATURDAY/02:00")
	if parsed == nil || len(parsed.Intervals) != 2 {
		t.Fatalf("a two-interval window parsed to %#v", parsed)
	}
	if got := weeklyWindowToApollo(parsed); got != "MONDAY/10:00-MONDAY/12:00,FRIDAY/22:00-SATURDAY/02:00" {
		t.Errorf("a restricted window did not survive a round trip: %q", got)
	}
}
