package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
	"github.com/jonathanagustin/conduit/internal/apollographql/schema"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
)

// --- doubles ----------------------------------------------------------------

type fakeApollo struct {
	environments  []apollo.Environment
	installations map[string][]apollo.Installation
	products      []apollo.Product
	releases      map[string][]apollo.Release
}

func (fake *fakeApollo) ListEnvironments() apollo.ListEnvironmentsResponse {
	return apollo.ListEnvironmentsResponse{Environments: &fake.environments}
}

func (fake *fakeApollo) GetEnvironment(rid string) (apollo.Environment, error) {
	for _, environment := range fake.environments {
		if string(environment.Rid) == rid {
			return environment, nil
		}
	}
	return apollo.Environment{}, errors.New("not found")
}

func (fake *fakeApollo) ListInstallations(environmentRID string) apollo.ListInstallationsResponse {
	installations := fake.installations[environmentRID]
	return apollo.ListInstallationsResponse{Installations: &installations}
}

func (fake *fakeApollo) ListProducts() apollo.ListProductsResponse {
	return apollo.ListProductsResponse{Products: &fake.products}
}

func (fake *fakeApollo) GetProduct(product string) (apollo.Product, error) {
	for _, candidate := range fake.products {
		if string(candidate.Id) == product {
			return candidate, nil
		}
	}
	return apollo.Product{}, errors.New("not found")
}

func (fake *fakeApollo) ListReleases(product string) apollo.ListReleasesResponse {
	releases := fake.releases[product]
	return apollo.ListReleasesResponse{Releases: &releases}
}

func (fake *fakeApollo) GetRelease(product, _ string) (apollo.Release, error) {
	releases := fake.releases[product]
	if len(releases) == 0 {
		return apollo.Release{}, errors.New("not found")
	}
	return releases[0], nil
}

type fakeOverview struct {
	plans          []reporting.PlanSummary
	installations  []reporting.InstallationSummary
	changeRequests []reporting.ChangeRequestSummary
	promotions     []reporting.PromotionSummary
	promotionErr   error
}

func (fake *fakeOverview) VisiblePlans(context.Context) ([]reporting.PlanSummary, error) {
	return fake.plans, nil
}

func (fake *fakeOverview) VisibleInstallations(
	context.Context, time.Duration, time.Time,
) ([]reporting.InstallationSummary, error) {
	return fake.installations, nil
}

func (fake *fakeOverview) VisibleChangeRequests(context.Context) ([]reporting.ChangeRequestSummary, error) {
	return fake.changeRequests, nil
}

func (fake *fakeOverview) VisiblePromotions(context.Context) ([]reporting.PromotionSummary, error) {
	return fake.promotions, fake.promotionErr
}

// fixture binds one set of sources to the shared resolver, so a test reads like
// a request does.
type fixture struct {
	*Resolver
	sources ports.Sources
}

func (f *fixture) Execute(ctx context.Context, request Request) Response {
	return f.Resolver.Execute(ctx, f.sources, request)
}

func (f *fixture) resolve(ctx context.Context, coordinate domain.Coordinate, source any, arguments map[string]any) (any, error) {
	return (&bound{Resolver: f.Resolver, sources: f.sources}).resolve(ctx, coordinate, source, arguments)
}

func newFixture(t *testing.T) (*fixture, *fakeApollo, *fakeOverview) {
	t.Helper()
	apolloSource := &fakeApollo{
		environments: []apollo.Environment{{Name: "prod", Rid: "ri.apollo.main.environment.a"}},
		installations: map[string][]apollo.Installation{
			"ri.apollo.main.environment.a": {{
				ApolloEntityId: "entity-1",
				EnvironmentRid: "ri.apollo.main.environment.a",
				PackageRid:     "ri.apollo.main.package.b",
				ProductId:      "checkout",
				Rid:            "installation-1",
				Version:        ptr("1.4.2"),
			}},
		},
		products: []apollo.Product{{Id: "checkout", Group: "com.example"}},
		releases: map[string][]apollo.Release{"checkout": {{MavenCoordinate: "com.example:checkout:1.4.2"}}},
	}
	overview := &fakeOverview{
		plans: []reporting.PlanSummary{{ID: "plan-1", Environment: "prod", Phase: reporting.PlanPhaseRunning}},
		installations: []reporting.InstallationSummary{{
			Environment: "ri.apollo.main.environment.a", InstallationID: "installation-1",
			Health: reporting.HealthHealthy,
		}},
		changeRequests: []reporting.ChangeRequestSummary{{ID: "cr-1", Title: "Raise replicas", AwaitingApproval: true}},
		promotions:     []reporting.PromotionSummary{{ID: "promo-1", Product: "checkout", Blocked: true}},
	}
	resolver, err := New()
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	resolver.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return &fixture{Resolver: resolver, sources: ports.Sources{Apollo: apolloSource, Overview: overview}}, apolloSource, overview
}

func ptr[T any](value T) *T { return &value }

// --- the reconciliation -----------------------------------------------------

// The gate on the Go side. The provenance gate keeps the SDL honest about who
// established each coordinate; this keeps the resolver honest about which of
// them it can answer. A Conduit-native field nothing backs and a resolver
// claiming a coordinate the schema does not declare are both defects here.
func TestResolverAndSchemaAgree(t *testing.T) {
	provenance, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	issues := domain.Reconcile(provenance, Coverage())
	for _, defect := range domain.Defects(issues) {
		t.Errorf("defect: %s", defect)
	}

	// The unserved recovered surface is not a defect, but it is a number worth
	// pinning: it is the distance between "Apollo-shaped" and
	// "Conduit-answerable", and it should only ever move deliberately.
	unserved := 0
	for _, issue := range issues {
		if issue.Kind == domain.UnservedObserved {
			unserved++
		}
	}
	if unserved != 18 {
		t.Errorf(
			"%d recovered coordinate(s) are declared and unanswerable by Conduit; the pinned count is 18. "+
				"If this moved on purpose, update the count and docs/apollo/reference/graphql-api.md",
			unserved,
		)
	}
}

// A coverage entry is a claim that Conduit can answer a coordinate. This drives
// every one of them so the claim is executed rather than asserted -- a covered
// coordinate the switch forgot fails here rather than at a client's first
// query.
func TestEveryCoveredCoordinateResolves(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	sources := map[string]any{
		"Query":             rootSource{},
		"ApolloQuery":       rootSource{},
		"ApolloEnvironment": apolloSource.environments[0],
		"ApolloEntity":      entitySource{installation: apolloSource.installations["ri.apollo.main.environment.a"][0], environment: apolloSource.environments[0]},
		"ApolloProduct":     apolloSource.products[0],
		"ApolloProductRelease": releaseSource{
			product: apolloSource.products[0], release: apolloSource.releases["checkout"][0],
		},
		"ApolloProductReleaseManifest": apolloSource.releases["checkout"][0],
		"ApolloProductDependency":      apollo.ProductDependency{ProductId: ptr(apollo.ProductId("checkout")), Optional: ptr(true)},
		"ApolloPlan":                   reporting.PlanSummary{ID: "plan-1"},
		"ApolloChangeRequest":          reporting.ChangeRequestSummary{ID: "cr-1"},
		"ApolloPromotion":              reporting.PromotionSummary{ID: "promo-1"},
		"ApolloEnvironmentPage":        page{},
		"ApolloEntityPage":             page{},
		"ApolloProductPage":            page{},
		"ApolloProductReleasePage":     page{},
		"ApolloPlanPage":               page{records: []any{reporting.PlanSummary{ID: "plan-1"}}},
		"ApolloChangeRequestPage":      page{records: []any{reporting.ChangeRequestSummary{ID: "cr-1"}}},
		"ApolloPromotionPage":          page{records: []any{reporting.PromotionSummary{ID: "promo-1"}}},
	}
	arguments := map[string]any{"rid": "ri.apollo.main.environment.a", "pageSize": 10}

	provenance, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	for _, coordinate := range provenance.Coordinates() {
		if _, covered := Coverage().SourceOf(coordinate); !covered {
			continue
		}
		source, known := sources[coordinate.Type]
		if !known {
			t.Errorf("%s is covered but this test has no source for %s", coordinate, coordinate.Type)
			continue
		}
		if _, err := resolver.resolve(t.Context(), coordinate, source, arguments); err != nil {
			t.Errorf("%s is covered but did not resolve: %v", coordinate, err)
		}
	}
}

// --- refusal ----------------------------------------------------------------

// The distinction the whole interface rests on. ApolloBundle is a shape an
// Apollo Hub answers for and Conduit has never held; answering null would state
// that this bundle has no resource identifier, which Conduit cannot know.
func TestUnservedRecoveredCoordinateIsRefusedRatherThanNulled(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { bundle(rid: "ri.apollo.main.bundle.a") { rid } } }`,
	})
	if len(response.Errors) == 0 {
		t.Fatal("Conduit answered for a bundle it has never held instead of refusing")
	}
	message := response.Errors[0].Message
	for _, want := range []string{"ApolloQuery.bundle", "Apollo Hub", "null"} {
		if !strings.Contains(message, want) {
			t.Errorf("refusal does not mention %q: %s", want, message)
		}
	}
}

// The refusal has to survive the thing it is protecting against: a client that
// reads only `data` must not see the refused field as a resolved null.
func TestARefusalIsNotReportedAsAResolvedValue(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name spaceId } } }`,
	})
	environment := response.
		Data["apollo"].(map[string]any)["environment"].(map[string]any)
	if environment["name"] != "prod" {
		t.Fatalf("a served coordinate did not answer: %v", environment["name"])
	}
	if environment["spaceId"] != nil {
		t.Fatalf("an unserved coordinate answered with %v", environment["spaceId"])
	}
	if len(response.Errors) != 1 || !strings.Contains(response.Errors[0].Message, "ApolloEnvironment.spaceId") {
		t.Fatalf("the unserved coordinate produced no refusal naming it: %v", response.Errors)
	}
	if got := response.Errors[0].Path.String(); got != "apollo.environment.spaceId" {
		t.Errorf("refusal is not anchored to the field that caused it: %q", got)
	}
}

// --- provenance disclosure --------------------------------------------------

func TestResponseNamesTheUnrecoveredCoordinatesItServed(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { plans(pageSize: 10) { plans { rid status } } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	provenance := response.Extensions["provenance"].(map[string]any)
	documented := provenance["documentedServed"].([]string)
	want := []string{"ApolloPlan.rid", "ApolloPlan.status", "ApolloPlanPage.plans", "ApolloQuery.plans"}
	if strings.Join(documented, ",") != strings.Join(want, ",") {
		t.Fatalf("the response did not disclose exactly the documented coordinates it served:\n got %v\nwant %v", documented, want)
	}
	// Every one of them carries an Apollo name or an Apollo pattern, so none is
	// Conduit's own. Asserting the empty list rather than ignoring it keeps the
	// two disclosures from silently collapsing into one.
	if native := provenance["conduitNativeServed"].([]string); len(native) != 0 {
		t.Fatalf("a fully-cited query disclosed invention: %v", native)
	}
}

// The two lower tiers must stay distinguishable in the disclosure. A DOCUMENTED
// coordinate is a name Apollo publishes; a CONDUIT_NATIVE one is Conduit's own
// idea. Reporting them under one heading would overstate the invention in one
// direction and hide it in the other, and the disclosure is only worth reading
// if it does neither.
func TestTheDisclosureSeparatesDocumentedFromInvented(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { plans(pageSize: 10) { nextPageToken } } }`,
	})
	provenance := response.Extensions["provenance"].(map[string]any)
	documented := provenance["documentedServed"].([]string)
	native := provenance["conduitNativeServed"].([]string)
	// ApolloPlanPage.nextPageToken is DOCUMENTED because the recovery record
	// carries that exact name on four sibling page types -- Apollo's own GraphQL
	// name, established by capture. ApolloQuery.plans is DOCUMENTED from the
	// published Plans documentation.
	if strings.Join(documented, ",") != "ApolloPlanPage.nextPageToken,ApolloQuery.plans" {
		t.Fatalf("documented disclosure is wrong: %v", documented)
	}
	if len(native) != 0 {
		t.Fatalf("a coordinate Apollo names was disclosed as Conduit's invention: %v", native)
	}
}

// A query touching only recovered coordinates must not be labelled as serving
// anything unrecovered. The disclosure is worthless if it cries wolf.
func TestAPurelyRecoveredQueryDisclosesNothingUnrecovered(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name rid } } }`,
	})
	provenance := response.Extensions["provenance"].(map[string]any)
	if native := provenance["conduitNativeServed"].([]string); len(native) != 0 {
		t.Fatalf("a recovered-only query disclosed invention: %v", native)
	}
	if documented := provenance["documentedServed"].([]string); len(documented) != 0 {
		t.Fatalf("a recovered-only query disclosed unrecovered coordinates: %v", documented)
	}
	if provenance["observedServed"].(int) != 4 {
		t.Fatalf("observed selection count is wrong: %v", provenance["observedServed"])
	}
}

// --- the served data --------------------------------------------------------

func TestNativeRecordsAnswerFromReporting(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo {
			plans(pageSize: 10) { plans { rid environmentRid status } }
			changeRequests(pageSize: 10) { changeRequests { rid title status } }
			promotions(pageSize: 10) { promotions { rid productId status } }
		} }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	root := response.Data["apollo"].(map[string]any)
	plan := root["plans"].(map[string]any)["plans"].([]any)[0].(map[string]any)
	if plan["rid"] != "plan-1" || plan["environmentRid"] != "prod" || plan["status"] != string(reporting.PlanPhaseRunning) {
		t.Errorf("plan resolved to %v", plan)
	}
	change := root["changeRequests"].(map[string]any)["changeRequests"].([]any)[0].(map[string]any)
	if change["status"] != "AWAITING_APPROVAL" {
		t.Errorf("change request status resolved to %v", change["status"])
	}
	promotion := root["promotions"].(map[string]any)["promotions"].([]any)[0].(map[string]any)
	if promotion["status"] != "BLOCKED" {
		t.Errorf("promotion status resolved to %v", promotion["status"])
	}
}

// A stale report is not evidence of current health. An environment whose only
// installation reported healthy an hour ago must read UNKNOWN, not HEALTHY.
func TestStaleHealthReadsUnknownRatherThanHealthy(t *testing.T) {
	resolver, _, overview := newFixture(t)
	overview.installations[0].Stale = true
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { health } } }`,
	})
	environment := response.Data["apollo"].(map[string]any)["environment"].(map[string]any)
	if environment["health"] != reporting.HealthUnknown {
		t.Fatalf("a stale report was read as %v", environment["health"])
	}
}

// One unhealthy installation makes its environment unhealthy, whatever else
// reports. The aggregate must not be dominated by the healthy majority.
func TestOneUnhealthyInstallationMakesTheEnvironmentUnhealthy(t *testing.T) {
	resolver, _, overview := newFixture(t)
	overview.installations = append(overview.installations, reporting.InstallationSummary{
		Environment: "ri.apollo.main.environment.a", InstallationID: "installation-2",
		Health: reporting.HealthUnhealthy,
	})
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { health } } }`,
	})
	environment := response.Data["apollo"].(map[string]any)["environment"].(map[string]any)
	if environment["health"] != reporting.HealthUnhealthy {
		t.Fatalf("an unhealthy installation was averaged away into %v", environment["health"])
	}
}

// An unavailable store must refuse its panel rather than report an empty one.
// "No promotions" and "I could not ask" are different facts.
func TestAnUnavailableSourceRefusesRatherThanReportsEmpty(t *testing.T) {
	resolver, _, overview := newFixture(t)
	overview.promotionErr = errors.New("promotions unavailable to this reader")
	response := resolver.Execute(t.Context(), Request{Query: `{ apollo { promotions(pageSize: 10) { promotions { rid } } } }`})
	if len(response.Errors) == 0 {
		t.Fatal("an unavailable promotions store was reported as an empty page")
	}
	if !strings.Contains(response.Errors[0].Message, "unavailable to this reader") {
		t.Errorf("the refusal lost the store's own reason: %v", response.Errors[0])
	}
}

// Displacement, executed rather than asserted in a comment. Apollo publishes
// these three names -- Installation.version, Installation.environmentRid,
// Product.id/Installation.productId -- and Conduit had invented different ones.
// The invented names must be gone from the served interface, not merely
// deprecated beside the real ones: a client that can still select
// `installedVersion` is a client Conduit taught the wrong vocabulary.
func TestTheDisplacedNamesAreNoLongerSelectable(t *testing.T) {
	resolver, _, _ := newFixture(t)
	for _, invented := range []string{
		`{ apollo { entity(rid: "entity-1") { installedVersion } } }`,
		`{ apollo { plans(pageSize: 1) { plans { environment } } } }`,
		`{ apollo { promotions(pageSize: 1) { promotions { product } } } }`,
	} {
		response := resolver.Execute(t.Context(), Request{Query: invented})
		if len(response.Errors) == 0 {
			t.Errorf("the interface still answers an invented name Apollo displaced: %s", invented)
		}
		if response.Data != nil {
			t.Errorf("an invented name resolved to data: %s", invented)
		}
	}

	// And the published names do answer, so this is displacement rather than
	// removal. Proving only the refusal would pass on an interface that dropped
	// the field entirely.
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { entity(rid: "entity-1") { version } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("the published name does not answer: %v", response.Errors)
	}
	entity := response.Data["apollo"].(map[string]any)["entity"].(map[string]any)
	if entity["version"] != "1.4.2" {
		t.Errorf("ApolloEntity.version resolved to %v", entity["version"])
	}
}

// --- the contract's edges ---------------------------------------------------

func TestAFieldTheSchemaDoesNotDeclareIsRejectedBeforeResolution(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{Query: `{ apollo { inventedByTheClient } }`})
	if len(response.Errors) == 0 || response.Data != nil {
		t.Fatalf("an undeclared field was answered: %v", response)
	}
}

// The recovered surface established no mutation, and Conduit will not invent
// one. Nothing in this package enforces that: the served SDL declares no
// mutation root, so gqlparser refuses the operation during validation. The
// check lives here to pin that the schema -- not a hand-written guard -- is
// what makes the interface read-only.
func TestMutationIsRefusedByTheSchemaItself(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{Query: `mutation { apollo { plans(pageSize: 1) { nextPageToken } } }`})
	if len(response.Errors) == 0 {
		t.Fatal("a mutation was accepted against an interface with no recovered mutation surface")
	}
	if resolver.Schema().Mutation != nil {
		t.Fatal("the served interface declares a mutation root; the recovered surface established none")
	}
}

func TestAnAbsentSourceRefusesRatherThanPanics(t *testing.T) {
	resolver, err := New()
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	response := resolver.Execute(t.Context(), ports.Sources{}, Request{Query: `{ apollo { plans(pageSize: 1) { nextPageToken } } }`})
	if len(response.Errors) == 0 {
		t.Fatal("a resolver with no sources answered as though it had them")
	}
}
