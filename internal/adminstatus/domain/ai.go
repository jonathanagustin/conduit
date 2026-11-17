package domain

import (
	"sort"
	"strings"
)

// Model provider kinds. Invalid means the configuration is present and the
// Hub would refuse it, for example both transports set.
const (
	ProviderNone    = "none"
	ProviderCLI     = "cli"
	ProviderHTTP    = "http"
	ProviderInvalid = "invalid"
)

// Configured is the status of a component whose prerequisites are present.
// It is not a health claim: nothing here calls a model or runs a tool.
const (
	Configured   = "CONFIGURED"
	Unconfigured = "UNCONFIGURED"
)

// AIFacts is what one Hub process can observe about its own AI setup. It
// holds no secret: no key, no key path, and no endpoint path or query.
type AIFacts struct {
	// ProviderKind is one of the Provider constants.
	ProviderKind string
	// ProviderSummary is safe display text for the provider: a command's
	// base name, or an endpoint's scheme and host.
	ProviderSummary string
	// ConfiguredModel is the operator-configured model name, or empty.
	ConfiguredModel string
	// ConfigError says why the configuration is invalid, in fixed words.
	ConfigError string
	// Tools reports, per binary name, whether it was found by this process
	// (on PATH; zarf also through ZARF_BIN). A name that is absent from the map counts as not found.
	Tools map[string]bool
	// ImageBuilderAdmitted is true when CONDUIT_IMAGE_BUILDER=docker.
	ImageBuilderAdmitted bool
	// WorkspaceRootSet is true when DEVWORKSPACE_GIT_ROOT is set.
	WorkspaceRootSet bool
}

type aiCapability struct {
	id, label string
	// needsModel is false for a capability that still runs without a model.
	needsModel bool
	tools      []string
	// withoutModel describes what happens with no model configured.
	withoutModel string
}

var aiCapabilities = []aiCapability{
	{id: "aiAssistant", label: "AI assistant", needsModel: true, withoutModel: "No model provider is configured; the assistant makes no model call"},
	{id: "aiPlanning", label: "AI planning", needsModel: true, tools: []string{"git"}, withoutModel: "No model provider is configured; planning turns are refused"},
	{id: "aiAppGeneration", label: "AI app generation", needsModel: true, tools: []string{"docker", "go"}, withoutModel: "No model provider is configured; the recorded application is used and no model call is made"},
	{id: "aiPackagingProposals", label: "AI packaging proposals", needsModel: true, withoutModel: "No model provider is configured; the recorded proposals are used and no model call is made"},
}

// AIComponents reports the model provider and, for each AI capability,
// whether this deployment can run it and why not. A capability is CONFIGURED
// when its prerequisites are present, UNCONFIGURED when no model provider is
// set, and UNAVAILABLE when a prerequisite is missing. None of these is a
// liveness result.
func AIComponents(facts AIFacts) []Component {
	if facts.ProviderKind == ProviderCLI {
		facts.ProviderKind = ProviderInvalid
		facts.ConfigError = "local model commands are unsupported; configure an HTTP model endpoint"
	}
	out := []Component{providerComponent(facts)}
	for _, capability := range aiCapabilities {
		out = append(out, capabilityComponent(facts, capability))
	}
	return append(out, packagingComputeComponent(facts))
}

func providerComponent(facts AIFacts) Component {
	component := Component{ID: "modelProvider", Label: "Model provider"}
	switch facts.ProviderKind {
	case ProviderHTTP:
		component.Status = Configured
		model := "model name not configured (recorded as unverified)"
		if facts.ConfiguredModel != "" {
			model = "configured model " + facts.ConfiguredModel
		}
		component.Detail = "Provider kind " + facts.ProviderKind + ": " + facts.ProviderSummary + "; " + model + "; not probed, credentials not shown"
	case ProviderInvalid:
		component.Status = Unavailable
		component.Detail = "Provider kind invalid: " + facts.ConfigError
	default:
		component.Status = Unconfigured
		component.Detail = "Provider kind none: CONDUIT_MODEL_BASE_URL is not set"
	}
	return component
}

func capabilityComponent(facts AIFacts, capability aiCapability) Component {
	component := Component{ID: capability.id, Label: capability.label}
	switch facts.ProviderKind {
	case ProviderInvalid:
		component.Status = Unavailable
		component.Detail = "The model provider configuration is invalid: " + facts.ConfigError
		return component
	case ProviderHTTP:
	default:
		component.Status = Unconfigured
		component.Detail = capability.withoutModel
		if missing := missingTools(facts, capability.tools); len(missing) > 0 {
			component.Detail += "; also binary not found: " + strings.Join(missing, ", ")
		}
		return component
	}
	if missing := missingTools(facts, capability.tools); len(missing) > 0 {
		component.Status = Unavailable
		component.Detail = "Not usable in this deployment: binary not found: " + strings.Join(missing, ", ")
		return component
	}
	component.Status = Configured
	component.Detail = "Model provider kind " + facts.ProviderKind + " is configured"
	if len(capability.tools) > 0 {
		component.Detail += " and " + strings.Join(capability.tools, ", ") + " found"
	}
	component.Detail += "; presence only, no model call or tool run was made"
	return component
}

// packagingComputeComponent is the governed build after Accept. It runs with
// or without a model: the model only chooses who writes the package files.
func packagingComputeComponent(facts AIFacts) Component {
	component := Component{ID: "aiPackagingCompute", Label: "Packaging compute after Accept"}
	var reasons []string
	if !facts.ImageBuilderAdmitted {
		reasons = append(reasons, "CONDUIT_IMAGE_BUILDER is not docker")
	}
	if !facts.WorkspaceRootSet {
		reasons = append(reasons, "DEVWORKSPACE_GIT_ROOT is not set")
	}
	if missing := missingTools(facts, []string{"docker", "git", "zarf"}); len(missing) > 0 {
		reasons = append(reasons, "binary not found: "+strings.Join(missing, ", "))
	}
	if facts.ProviderKind == ProviderInvalid {
		reasons = append(reasons, "the model provider configuration is invalid: "+facts.ConfigError)
	}
	if len(reasons) > 0 {
		component.Status = Unavailable
		if !facts.ImageBuilderAdmitted && facts.ProviderKind != ProviderInvalid {
			component.Status = Unconfigured
		}
		component.Detail = "Not usable in this deployment: " + strings.Join(reasons, "; ")
		return component
	}
	component.Status = Configured
	writer := "the deterministic writer"
	if facts.ProviderKind == ProviderHTTP {
		writer = "the model provider (kind " + facts.ProviderKind + ")"
	}
	component.Detail = "Image builder admitted and docker, git, zarf found; package files are written by " + writer + "; presence only, nothing was run"
	return component
}

func missingTools(facts AIFacts, tools []string) []string {
	var missing []string
	for _, tool := range tools {
		if !facts.Tools[tool] {
			missing = append(missing, tool)
		}
	}
	sort.Strings(missing)
	return missing
}

// AITools lists every binary the AI capabilities look for.
func AITools() []string {
	return []string{"docker", "git", "go", "zarf"}
}
