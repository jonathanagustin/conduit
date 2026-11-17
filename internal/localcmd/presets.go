package localcmd

import "strings"

// Preset is one `packaging-workflow agent-exec --provider` choice.
type Preset struct {
	Name string
	// Argv is the default command. Nil means the built-in offline agent.
	Argv []string
	// Verified is true only when this repository holds a recorded run of the
	// preset against the real engine. A test with a fake CLI does not count.
	Verified bool
	// Evidence says what the status rests on.
	Evidence string
}

// Status is VERIFIED or UNVERIFIED.
func (preset Preset) Status() string {
	if preset.Verified {
		return "VERIFIED"
	}
	return "UNVERIFIED"
}

// DeterministicPreset is the built-in offline agent, the default provider.
const DeterministicPreset = "deterministic"

// Presets is the one table of agent-exec provider presets and whether each
// has been exercised. docs/operations/local-model-cli.md mirrors it and a
// test keeps the two in step. Do not mark a preset verified without adding
// the recorded run that proves it.
func Presets() []Preset {
	return []Preset{
		{Name: DeterministicPreset, Verified: true,
			Evidence: "built-in offline agent; exercised by the cmd/packaging-workflow tests in CI"},
	}
}

// PresetByName finds a preset. The empty name is the deterministic default.
func PresetByName(name string) (Preset, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DeterministicPreset
	}
	for _, preset := range Presets() {
		if preset.Name == name {
			return preset, true
		}
	}
	return Preset{}, false
}

// PresetFromArgv reports the agent-exec preset a model command selects.
// providerEnv is CONDUIT_AGENT_PROVIDER, the default for --provider. It
// returns false when argv is not an agent-exec command or names no known
// preset.
func PresetFromArgv(argv []string, providerEnv string) (Preset, bool) {
	agentExec := false
	provider := strings.TrimSpace(providerEnv)
	for i, arg := range argv {
		switch {
		case arg == "agent-exec":
			agentExec = true
		case arg == "--provider" && i+1 < len(argv):
			provider = argv[i+1]
		case strings.HasPrefix(arg, "--provider="):
			provider = strings.TrimPrefix(arg, "--provider=")
		}
	}
	if !agentExec {
		return Preset{}, false
	}
	return PresetByName(provider)
}
