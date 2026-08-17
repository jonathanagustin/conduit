package domain

import "testing"

// The topology decides four separable things, and conflating any two of them is
// how an Environment ends up with two writers or a promotion passing on health
// nobody measured.
func TestTopologyPredicatesSeparateWhatEachSettingDecides(t *testing.T) {
	t.Parallel()
	for setting, want := range map[string]struct {
		issuesPlans, acceptsAgents, acceptsRelayed, tracksHealth, silenceExpected bool
	}{
		ConnectionAgentsConnected: {issuesPlans: true, acceptsAgents: true, tracksHealth: true},
		// Intermittent is operationally Connected -- it issues Plans, accepts
		// agents, tracks health, and promotes -- and differs only in that its
		// silence is expected. That is the single flag that separates it, and a
		// bare-constant addition that missed it would be the regression the
		// federation comment warns against.
		ConnectionIntermittentAgents: {issuesPlans: true, acceptsAgents: true, tracksHealth: true, silenceExpected: true},
		// Relayed state is visible and can gate a promotion, but the Plans are
		// issued by the Hub the Agents actually connect to. Issuing here as well
		// would make two writers for one Environment.
		ConnectionRelayedFromAnotherHub: {acceptsRelayed: true, tracksHealth: true},
		ConnectionNoConnectionExpected:  {},
		ConnectionUnknown:               {},
	} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			// Present is held true so this case varies one dimension. It asks
			// what each SETTING decides; whether the Hub has reviewed the
			// Environment at all is the other half of
			// ParticipatesInPromotion and has its own case below.
			federation := Federation{ConnectionSetting: setting, Present: true}
			got := struct {
				issuesPlans, acceptsAgents, acceptsRelayed, tracksHealth, silenceExpected bool
			}{
				federation.IssuesPlans(), federation.AcceptsAgentConnections(),
				federation.AcceptsRelayedState(), federation.TracksHealth(),
				federation.SilenceIsExpected(),
			}
			if got != want {
				t.Fatalf("%s = %+v, want %+v", setting, got, want)
			}
			// An Environment that holds no observed state has nothing to attest
			// with; counting it would let a canary pass on absent evidence.
			if federation.ParticipatesInPromotion() != want.tracksHealth {
				t.Fatalf("%s participates in promotion without tracking health", setting)
			}
			// And the same setting on an Environment this Hub has never
			// reviewed vouches for nothing, whatever its topology says it
			// would track.
			unreviewed := Federation{ConnectionSetting: setting}
			if unreviewed.ParticipatesInPromotion() {
				t.Fatalf("%s vouches for a Release on a Hub that has never reviewed the Environment", setting)
			}
			// ExpectsDirectAgents is what the operational predicates are built on,
			// so it must agree with them: an Environment that issues Plans and
			// accepts agents expects direct agents, and vice versa.
			if federation.ExpectsDirectAgents() != (want.issuesPlans && want.acceptsAgents) {
				t.Fatalf("%s ExpectsDirectAgents disagrees with its operational predicates", setting)
			}
			// Expected silence is a refinement of direct agents: an intermittent
			// Environment is a direct-agent setting, so anything whose silence is
			// expected also expects direct agents. Relayed silence is not expected --
			// it means the import broke -- so relayed must not carry the flag.
			if federation.SilenceIsExpected() && !federation.ExpectsDirectAgents() {
				t.Fatalf("%s marks silence expected without expecting direct agents", setting)
			}
		})
	}

	for source, want := range map[string]struct{ edit, importSettings, breakGlass bool }{
		SourceEditOnThisHub:    {edit: true, breakGlass: true},
		SourceEditOnAnotherHub: {importSettings: true, breakGlass: true},
		// A read-only copy exists so a standby can take over unchanged. Even one
		// local override would make it diverge from the Hub it mirrors.
		SourceCopyOfAnotherHub: {importSettings: true},
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			federation := Federation{DeclaredStateSource: source}
			got := struct{ edit, importSettings, breakGlass bool }{
				federation.AllowsSettingsEdit(), federation.AllowsSettingsImport(),
				federation.AllowsBreakGlassEntityConfig(),
			}
			if got != want {
				t.Fatalf("%s = %+v, want %+v", source, got, want)
			}
		})
	}
}

// An undeclared Environment issues Plans. Platform's Environment state is
// Git-owned and immutable per generation, so there is nowhere to record the
// transition Apollo makes in place; defaulting to UNKNOWN would stop every
// existing Environment instead.
func TestResolveFederationDefaultsToAnOperationalTopology(t *testing.T) {
	t.Parallel()
	federation := ResolveFederation(FederationSettings{Environment: "dev"})
	if federation.ConnectionSetting != ConnectionAgentsConnected {
		t.Fatalf("connection setting = %q", federation.ConnectionSetting)
	}
	if federation.DeclaredStateSource != SourceEditOnThisHub {
		t.Fatalf("declared state source = %q", federation.DeclaredStateSource)
	}
	if !federation.IssuesPlans() {
		t.Fatal("an undeclared Environment stopped issuing Plans")
	}
	// Whitespace is not a declaration.
	padded := ResolveFederation(FederationSettings{
		Environment: "dev", ConnectionSetting: "  " + ConnectionRelayedFromAnotherHub + "  ",
		DeclaredStateSource: "\t" + SourceCopyOfAnotherHub + "\n",
	})
	if padded.ConnectionSetting != ConnectionRelayedFromAnotherHub || padded.DeclaredStateSource != SourceCopyOfAnotherHub {
		t.Fatalf("padded settings = %+v", padded)
	}
	// An explicit UNKNOWN is the bootstrap gate and must survive resolution.
	gated := ResolveFederation(FederationSettings{Environment: "dev", ConnectionSetting: ConnectionUnknown})
	if gated.IssuesPlans() {
		t.Fatal("an explicitly UNKNOWN Environment issued Plans")
	}
}

func TestValidateSettingsRefusesUnrecognizedValuesAndUnpermittedOverrides(t *testing.T) {
	t.Parallel()
	if issues := ValidateSettings(FederationSettings{Environment: "dev"}, nil); len(issues) != 0 {
		t.Fatalf("a defaulted Environment was invalid: %#v", issues)
	}

	bad := ValidateSettings(FederationSettings{Environment: "dev", ConnectionSetting: "SOMETIMES"}, nil)
	if len(bad) != 1 || bad[0].Path != "environment.connectionSetting" {
		t.Fatalf("issues = %#v", bad)
	}

	// An unrecognized edit source stops validation: with no known source there
	// is no answer to whether overrides are permitted, and guessing one would
	// report a second, invented problem.
	unknownSource := ValidateSettings(
		FederationSettings{Environment: "dev", DeclaredStateSource: "SOMEWHERE"},
		[]string{"", "replicas: 2"},
	)
	if len(unknownSource) != 1 || unknownSource[0].Path != "environment.declaredStateSource" {
		t.Fatalf("issues = %#v", unknownSource)
	}

	// A read-only copy refuses break-glass overrides, and names the entity.
	copyOf := ValidateSettings(
		FederationSettings{Environment: "dev", DeclaredStateSource: SourceCopyOfAnotherHub},
		[]string{"", "replicas: 2", "  "},
	)
	if len(copyOf) != 1 || copyOf[0].Path != "entities[1].configOverrides" {
		t.Fatalf("issues = %#v", copyOf)
	}
	// The same overrides are fine where break-glass editing is permitted.
	editable := ValidateSettings(
		FederationSettings{Environment: "dev", DeclaredStateSource: SourceEditOnAnotherHub},
		[]string{"", "replicas: 2"},
	)
	if len(editable) != 0 {
		t.Fatalf("break-glass overrides were refused where they are permitted: %#v", editable)
	}
}

func TestSettingsImportIsRefusedOnlyWhereThisHubIsAuthoritative(t *testing.T) {
	t.Parallel()
	// An Environment this Hub has never reviewed has no settings to protect:
	// the first Bundle carrying it creates it.
	absent := Federation{Environment: "dev", DeclaredStateSource: SourceEditOnThisHub}
	if refusal := RefuseSettingsImport(absent); refusal != nil {
		t.Fatalf("importing into an unknown Environment was refused: %#v", refusal)
	}

	authoritative := Federation{Environment: "dev", DeclaredStateSource: SourceEditOnThisHub, Present: true}
	refusal := RefuseSettingsImport(authoritative)
	if refusal == nil || refusal.Reason != ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState {
		t.Fatalf("refusal = %#v", refusal)
	}
	if refusal.Environment != "dev" {
		t.Fatalf("refusal does not name the Environment: %#v", refusal)
	}

	// A value that is not a declared state source at all is a different failure
	// from a deliberately authoritative one, and the published names differ.
	garbled := RefuseSettingsImport(Federation{Environment: "dev", DeclaredStateSource: "SOMEWHERE", Present: true})
	if garbled == nil || garbled.Reason != ErrorInvalidTargetDeclaredStateSource {
		t.Fatalf("refusal = %#v", garbled)
	}

	for _, source := range []string{SourceEditOnAnotherHub, SourceCopyOfAnotherHub} {
		handed := Federation{Environment: "dev", DeclaredStateSource: source, Present: true}
		if refusal := RefuseSettingsImport(handed); refusal != nil {
			t.Fatalf("%s refused an import it should accept: %#v", source, refusal)
		}
	}
}
