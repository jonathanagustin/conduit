package domain

import (
	"strings"
	"testing"
)

func byID(components []Component) map[string]Component {
	out := map[string]Component{}
	for _, component := range components {
		out[component.ID] = component
	}
	return out
}

var allTools = map[string]bool{"docker": true, "git": true, "go": true, "zarf": true}

func TestAIComponentsUseOnlyContractStatuses(t *testing.T) {
	// api/admin-status.json admits exactly these component statuses.
	allowed := map[string]bool{Available: true, Configured: true, Unconfigured: true, Unavailable: true}
	for _, facts := range []AIFacts{
		{},
		{ProviderKind: ProviderHTTP, Tools: allTools, ImageBuilderAdmitted: true, WorkspaceRootSet: true},
		{ProviderKind: ProviderCLI},
		{ProviderKind: ProviderInvalid, ConfigError: "both set"},
	} {
		components := AIComponents(facts)
		if len(components) != 6 {
			t.Fatalf("components = %d, want the provider and five capabilities", len(components))
		}
		for _, component := range components {
			if !allowed[component.Status] || component.ID == "" || component.Label == "" || component.Detail == "" {
				t.Fatalf("component outside the contract: %+v", component)
			}
			// Nothing here measures liveness, so nothing may claim it.
			if component.Status == Available {
				t.Fatalf("an unprobed capability claimed AVAILABLE: %+v", component)
			}
		}
	}
}

func TestNoModelProviderLeavesEveryModelCapabilityUnconfigured(t *testing.T) {
	got := byID(AIComponents(AIFacts{ProviderKind: ProviderNone, Tools: allTools}))
	if got["modelProvider"].Status != Unconfigured || !strings.Contains(got["modelProvider"].Detail, "kind none") {
		t.Fatalf("provider = %+v", got["modelProvider"])
	}
	for _, id := range []string{"aiAssistant", "aiPlanning", "aiAppGeneration", "aiPackagingProposals"} {
		if got[id].Status != Unconfigured || !strings.Contains(got[id].Detail, "No model provider is configured") {
			t.Fatalf("%s = %+v", id, got[id])
		}
	}
}

func TestPlanningNeedsGitAndSaysSo(t *testing.T) {
	// The shipped Hub image has no git, go or docker. A model alone does not
	// make planning or app generation usable there.
	got := byID(AIComponents(AIFacts{ProviderKind: ProviderHTTP, ProviderSummary: "endpoint https://models.example", ConfiguredModel: "m1", Tools: map[string]bool{}}))
	if got["modelProvider"].Status != Configured || !strings.Contains(got["modelProvider"].Detail, "kind http") || !strings.Contains(got["modelProvider"].Detail, "configured model m1") {
		t.Fatalf("provider = %+v", got["modelProvider"])
	}
	if got["aiPlanning"].Status != Unavailable || !strings.Contains(got["aiPlanning"].Detail, "binary not found: git") {
		t.Fatalf("planning without git = %+v", got["aiPlanning"])
	}
	if got["aiAppGeneration"].Status != Unavailable || !strings.Contains(got["aiAppGeneration"].Detail, "binary not found: docker") {
		t.Fatalf("app generation without go and docker = %+v", got["aiAppGeneration"])
	}
	// The assistant and packaging proposals need only the model.
	for _, id := range []string{"aiAssistant", "aiPackagingProposals"} {
		if got[id].Status != Configured || !strings.Contains(got[id].Detail, "no model call") {
			t.Fatalf("%s = %+v", id, got[id])
		}
	}
	withGit := byID(AIComponents(AIFacts{ProviderKind: ProviderHTTP, Tools: map[string]bool{"git": true}}))
	if withGit["aiPlanning"].Status != Configured || !strings.Contains(withGit["aiPlanning"].Detail, "git found") {
		t.Fatalf("planning with git = %+v", withGit["aiPlanning"])
	}
	if withGit["aiAppGeneration"].Status != Unavailable {
		t.Fatalf("app generation with only git = %+v", withGit["aiAppGeneration"])
	}
}

func TestInvalidProviderMakesEveryCapabilityUnavailableWithTheReason(t *testing.T) {
	got := byID(AIComponents(AIFacts{ProviderKind: ProviderInvalid, ConfigError: "CONDUIT_MODEL_CMD and CONDUIT_MODEL_BASE_URL are both set", Tools: allTools, ImageBuilderAdmitted: true, WorkspaceRootSet: true}))
	for id, component := range got {
		if component.Status != Unavailable || !strings.Contains(component.Detail, "both set") {
			t.Fatalf("%s = %+v", id, component)
		}
	}
}

func TestLegacyCLIProviderCannotBeReportedConfigured(t *testing.T) {
	got := byID(AIComponents(AIFacts{ProviderKind: ProviderCLI, Tools: allTools, ImageBuilderAdmitted: true, WorkspaceRootSet: true}))
	for _, id := range []string{"modelProvider", "aiAssistant", "aiPlanning", "aiPackagingCompute"} {
		if got[id].Status != Unavailable || !strings.Contains(got[id].Detail, "unsupported") {
			t.Fatalf("%s=%+v", id, got[id])
		}
	}
}

func TestPackagingComputeReportsEachMissingPrerequisite(t *testing.T) {
	off := byID(AIComponents(AIFacts{Tools: allTools}))["aiPackagingCompute"]
	if off.Status != Unconfigured || !strings.Contains(off.Detail, "CONDUIT_IMAGE_BUILDER is not docker") || !strings.Contains(off.Detail, "DEVWORKSPACE_GIT_ROOT is not set") {
		t.Fatalf("not admitted = %+v", off)
	}
	noTools := byID(AIComponents(AIFacts{ImageBuilderAdmitted: true, WorkspaceRootSet: true}))["aiPackagingCompute"]
	if noTools.Status != Unavailable || !strings.Contains(noTools.Detail, "binary not found: docker, git, zarf") {
		t.Fatalf("admitted without tools = %+v", noTools)
	}
	ready := byID(AIComponents(AIFacts{ImageBuilderAdmitted: true, WorkspaceRootSet: true, Tools: allTools}))["aiPackagingCompute"]
	if ready.Status != Configured || !strings.Contains(ready.Detail, "deterministic writer") {
		t.Fatalf("ready without a model = %+v", ready)
	}
	withModel := byID(AIComponents(AIFacts{ProviderKind: ProviderHTTP, ImageBuilderAdmitted: true, WorkspaceRootSet: true, Tools: allTools}))["aiPackagingCompute"]
	if withModel.Status != Configured || !strings.Contains(withModel.Detail, "model provider (kind http)") {
		t.Fatalf("ready with a model = %+v", withModel)
	}
}

func TestAppGenerationReportsShapeSpecificPrerequisites(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		tools          map[string]bool
		want           string
		python         bool
	}{
		{"python without compiler", ProviderHTTP, map[string]bool{"docker": true}, Configured, true},
		{"all shapes", ProviderHTTP, map[string]bool{"docker": true, "go": true}, Configured, false},
		{"compiler without engine", ProviderHTTP, map[string]bool{"go": true}, Unavailable, false},
		{"no tools", ProviderHTTP, map[string]bool{}, Unavailable, false},
		{"recorded without compiler", ProviderNone, map[string]bool{"docker": true}, Unconfigured, false},
		{"invalid provider", ProviderInvalid, map[string]bool{"docker": true}, Unavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := byID(AIComponents(AIFacts{ProviderKind: tc.provider, Tools: tc.tools, ConfigError: "invalid provider"}))["aiAppGeneration"]
			if c.Status != tc.want {
				t.Fatalf("status=%s want=%s detail=%s", c.Status, tc.want, c.Detail)
			}
			if strings.Contains(c.Detail, "Python build prerequisites are present") != tc.python {
				t.Fatalf("Python prerequisite claim incorrect: %s", c.Detail)
			}
			if !tc.tools["go"] && !strings.Contains(c.Detail, "Go/HTML builds and the recorded Go demonstration also require go") {
				t.Fatalf("missing compiler limits not reported: %s", c.Detail)
			}
			if c.Status == Available {
				t.Fatal("presence check claimed liveness")
			}
		})
	}
}
