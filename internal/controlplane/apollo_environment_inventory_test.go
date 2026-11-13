package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
)

// environmentInventoryFixture declares an Environment with Namespaces and
// Entities and commits it as a Plan, which is what makes it durable and
// therefore visible to a replica that never served the write.
func environmentInventoryFixture(t *testing.T, repository Repository) DesiredState {
	t.Helper()
	state := assessedFoundation()
	state.Environment.Namespaces = []NamespaceTarget{
		{RID: "ri.namespace.main.namespace.example", Name: "example"},
	}
	if _, err := NewLifecycle(repository).CreatePlan(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	return state
}

// An Environment declared through one Hub replica must be listed by the other.
//
// This is the rest of the defect #153 fixed for recalls. SyncEnvironment wrote
// Environments, Installations and Namespaces into maps on the service struct,
// and it ran only in the process that accepted the desired state. The other
// replica -- and every replica after a restart, because New rehydrates
// publications alone -- served an empty list from all three reads.
//
// An empty list is the failure that matters: a client cannot tell it apart from
// "this Hub governs nothing", so a fleet reads as absent rather than as
// unanswered. That is the unknown-reads-as-none conflation the provenance model
// exists to prevent, arriving through the store rather than through
// authorization.
//
// The second API is constructed independently over the SAME repository, which
// is what a second replica and a restarted process both are. A restart-only
// test would pass while the replica case stayed broken.
func TestEnvironmentInventoryReadsFromASecondReplica(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)

	writer := newApolloPublicAPI(repository, governance, HandlerOptions{})
	if writer.initializationError != nil {
		t.Fatal(writer.initializationError)
	}
	state := environmentInventoryFixture(t, repository)
	writer.syncDesiredState(ctx, state)

	rid := apolloapp.EnvironmentRID(state.Environment.ID)

	// Stated rather than assumed: the replica that served the write can see the
	// Environment, so a failure below is about which process answered and not
	// about the fixture.
	served, err := writer.application.ListEnvironments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if served.Environments == nil || len(*served.Environments) != 1 {
		t.Fatalf("the fixture never established an Environment to read back: %#v", served.Environments)
	}

	reader := newApolloPublicAPI(repository, governance, HandlerOptions{})
	if reader.initializationError != nil {
		t.Fatal(reader.initializationError)
	}

	listed, err := reader.application.ListEnvironments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Environments == nil || len(*listed.Environments) != 1 {
		t.Fatalf("a second replica listed no Environments; the fleet was answered from "+
			"process memory only the writing replica held: %#v", listed.Environments)
	}
	if got := (*listed.Environments)[0]; got.Rid != rid || got.Name != state.Environment.ID {
		t.Errorf("the listed Environment did not survive the replica boundary: %#v", got)
	}

	if _, err := reader.application.GetEnvironment(ctx, rid); err != nil {
		t.Errorf("a second replica reported a governed Environment as absent: %v", err)
	}

	installations, err := reader.application.ListInstallations(ctx, rid)
	if err != nil {
		t.Fatal(err)
	}
	if installations.Installations == nil || len(*installations.Installations) != len(state.Entities) {
		t.Fatalf("a second replica listed %v Installations, want %d",
			installations.Installations, len(state.Entities))
	}
	products := map[string]string{}
	for _, installation := range *installations.Installations {
		products[installation.ProductId] = installation.Rid
		if installation.EnvironmentRid != rid {
			t.Errorf("an Installation was attributed to the wrong Environment: %#v", installation)
		}
	}
	for _, entity := range state.Entities {
		if _, found := products[entity.Product]; !found {
			t.Errorf("the second replica lost the Installation of %s", entity.Product)
		}
	}
	// The keyed read is a separate call site, and half a fix leaves the list and
	// the single read disagreeing about whether an Entity is installed at all.
	for product, installationRID := range products {
		if _, err := reader.application.GetInstallation(ctx, rid, installationRID); err != nil {
			t.Errorf("a second replica reported the Installation of %s as absent: %v", product, err)
		}
	}

	namespaces, err := reader.application.ListNamespaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if namespaces.Namespaces == nil || len(*namespaces.Namespaces) != 1 {
		t.Fatalf("a second replica listed no Namespaces: %#v", namespaces.Namespaces)
	}
	if got := (*namespaces.Namespaces)[0].Id; got != "example" {
		t.Errorf("the Namespace did not survive the replica boundary: %q", got)
	}
}

// The inventory reads the newest Plan per Environment INCLUDING command Plans,
// where #153's lifecycle port deliberately excludes them. This test is what
// makes that a decision rather than an accident, and it is the reason the two
// reads do not share a port.
//
// A command stores a desired state of its own, and IssueCommand retargets the
// Entity version in it (retargetEntityVersion, commands.go) -- an uninstall or
// a restart pins the OBSERVED version, an install pins the requested one. So a
// command Plan is not a copy of the declaration it follows: it is the Hub's
// newest statement of what should be installed. Filtering it out the way the
// lifecycle does would make Apollo report the superseded version for as long as
// no further Plan was proposed.
//
// The lifecycle cannot include command Plans, because a command strips Recalls
// and reading one as the Environment's declaration reports a recalled Release
// as not recalled. One port would have to pick one filter, and each read needs
// the other one.
func TestACommandPlanIsTheNewestDeclarationOfWhatIsInstalled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := environmentInventoryFixture(t, repository)

	// What IssueCommand persists: Recalls stripped, and the commanded Entity
	// retargeted to a different version than the declaration carried.
	commandState := cloneJSON(state)
	commandState.Recalls = nil
	for index := range commandState.Entities {
		if commandState.Entities[index].Name == "example-api" {
			commandState.Entities[index].Version = "0.0.9"
		}
	}
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "plan-command", CreatedAt: time.Now().UTC().Add(time.Hour),
		Plan:         Plan{Environment: state.Environment.ID},
		DesiredState: &commandState,
		Command: &EntityCommand{
			InstallationID: "example-api", Type: CommandRestartResource,
			ResourceKind: "Deployment", ResourceName: "example-api",
		},
	}); err != nil {
		t.Fatal(err)
	}

	reader := newApolloPublicAPI(repository, NewGovernanceService(repository), HandlerOptions{})
	if reader.initializationError != nil {
		t.Fatal(reader.initializationError)
	}
	installations, err := reader.application.ListInstallations(ctx, apolloapp.EnvironmentRID(state.Environment.ID))
	if err != nil {
		t.Fatal(err)
	}
	if installations.Installations == nil || len(*installations.Installations) != len(state.Entities) {
		t.Fatalf("a command Plan erased the Environment's Installations: %#v", installations.Installations)
	}
	var served *apollo.Installation
	for index, installation := range *installations.Installations {
		if installation.ProductId == "app.example:example-api" {
			served = &(*installations.Installations)[index]
		}
	}
	if served == nil {
		t.Fatal("the commanded Entity lost its Installation entirely")
	}
	if served.Version == nil {
		t.Fatal("the commanded Entity was served with no version at all")
	}
	if *served.Version != "0.0.9" {
		t.Errorf("the command's retargeted version was ignored and a superseded "+
			"declaration was served instead: %q", *served.Version)
	}
}
