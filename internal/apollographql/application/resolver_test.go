package application

import (
	"context"
	"errors"
	"slices"
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
	// environmentFailure stands in for anything that is not a missing record:
	// a store that is down, a timeout, a permission failure. The port admits
	// any error, so a test double that could only be "not found" could not
	// exercise the distinction the port draws.
	environmentFailure error
	// catalogueReads counts ListProducts calls, so a test can assert that a
	// lookup refused an identifier outright rather than walking the catalogue to
	// reach the same answer. The two are indistinguishable from the response.
	catalogueReads int
	// listingFailure is the same latitude environmentFailure gives the
	// by-identifier reads, extended to the listings now that the port lets them
	// report one. Authorization refusal arrives this way.
	listingFailure error
}

func (fake *fakeApollo) ListEnvironments() (apollo.ListEnvironmentsResponse, error) {
	return apollo.ListEnvironmentsResponse{Environments: &fake.environments}, fake.listingFailure
}

func (fake *fakeApollo) GetEnvironment(rid string) (apollo.Environment, error) {
	if fake.environmentFailure != nil {
		return apollo.Environment{}, fake.environmentFailure
	}
	for _, environment := range fake.environments {
		if string(environment.Rid) == rid {
			return environment, nil
		}
	}
	// The port's sentinel, not a bare error. A projection that spells
	// not-found any other way is asking to be propagated, which is the safe
	// default -- and this double previously returned errors.New("not found"),
	// which the resolver swallowed into a null along with everything else.
	return apollo.Environment{}, ports.ErrRecordNotFound
}

func (fake *fakeApollo) ListInstallations(environmentRID string) (apollo.ListInstallationsResponse, error) {
	installations := fake.installations[environmentRID]
	return apollo.ListInstallationsResponse{Installations: &installations}, fake.listingFailure
}

func (fake *fakeApollo) ListProducts() (apollo.ListProductsResponse, error) {
	fake.catalogueReads++
	return apollo.ListProductsResponse{Products: &fake.products}, fake.listingFailure
}

func (fake *fakeApollo) GetProduct(product string) (apollo.Product, error) {
	for _, candidate := range fake.products {
		if string(candidate.Id) == product {
			return candidate, nil
		}
	}
	return apollo.Product{}, errors.New("not found")
}

func (fake *fakeApollo) ListReleases(product string) (apollo.ListReleasesResponse, error) {
	releases := fake.releases[product]
	return apollo.ListReleasesResponse{Releases: &releases}, fake.listingFailure
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
				// The Entity id carries the shape the projection actually
				// mints, aeid:<environment>:helm-chart:<entity>, because the
				// health join recovers the Entity name from it. A placeholder
				// here would make the join untestable and was why this fixture
				// previously had to be hand-aligned to the rid.
				ApolloEntityId: "aeid:prod:helm-chart:checkout-api",
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
		// Reporting keys on plain identifiers -- the Environment's name and the
		// Entity's name -- which is what a Hub records when an Agent reports.
		// This fixture previously carried the minted rid here instead, which
		// made a broken join look like a working one: the resolver compared
		// rid to rid and passed, while a real Hub compared rid to "prod" and
		// answered UNKNOWN for every Environment however healthy the fleet was.
		// The plans entry beside it always used the plain name.
		installations: []reporting.InstallationSummary{{
			Environment: "prod", InstallationID: "checkout-api",
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
	return &fixture{Resolver: resolver, sources: ports.Sources{Apollo: apolloSource, Overview: overview, Grouping: &fakeGrouping{ids: []string{"bundle-1"}}}}, apolloSource, overview
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
	if unserved != 1 {
		t.Errorf(
			"%d recovered coordinate(s) are declared and unanswerable by Conduit; the pinned count is 1. "+
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
		"ApolloBundle":      bundleSource{id: "bundle-1"},
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
	// One rid does not fit every entry point. The Conduit-namespaced identifiers
	// exist precisely so they cannot be mistaken for Apollo's, so a lookup keyed
	// on one has to be given its own -- and being handed an Apollo rid is a case
	// that must return no record rather than scanning for something that could
	// not be there.
	perCoordinate := map[domain.Coordinate]map[string]any{
		at("ApolloQuery", "bundle"):  {"rid": bundleRID("bundle-1")},
		at("ApolloQuery", "bundles"): {"rids": bundleRID("bundle-1")},
		at("ApolloQuery", "productRelease"): {
			"rid":      productReleaseRID("com.example:checkout:1.4.2"),
			"pageSize": 10,
		},
	}

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
		selected := arguments
		if override, special := perCoordinate[coordinate]; special {
			selected = override
		}
		if _, err := resolver.resolve(t.Context(), coordinate, source, selected); err != nil {
			t.Errorf("%s is covered but did not resolve: %v", coordinate, err)
		}
	}
}

// --- refusal ----------------------------------------------------------------

// The remaining namespace field has no native governed-resource source.
// Its refusal must remain distinct from a configured source observing absence.
func TestUnservedRecoveredCoordinateIsRefusedRatherThanNulled(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { namespaceId } } }`,
	})
	if len(response.Errors) == 0 {
		t.Fatal("Conduit answered for a namespace it does not model instead of refusing")
	}
	message := response.Errors[0].Message
	for _, want := range []string{"ApolloEnvironment.namespaceId", "Apollo Hub", "null"} {
		if !strings.Contains(message, want) {
			t.Errorf("refusal does not mention %q: %s", want, message)
		}
	}
}

// A refused coordinate is a null in `data` PLUS an entry in `errors`, and the
// errors entry is the whole of the refusal.
//
// This test used to claim the opposite -- "a client that reads only `data` must
// not see the refused field as a resolved null" -- and the wire shape does not
// have that property. GraphQL requires a nullable field that failed to resolve
// to be null in `data`, so a data-only client does see a null there. What
// stops that null being read as an observed absence is the errors entry naming
// the coordinate, which is why the assertions below are on it.
//
// The old assertion was also a tautology: `environment["namespaceId"] != nil`
// passes whether the key is absent or present-and-null, because a Go map
// lookup of a missing key returns nil either way. It could not distinguish the
// two cases it existed to distinguish. The comma-ok form below can.
func TestARefusalIsNotReportedAsAResolvedValue(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name namespaceId } } }`,
	})
	environment := response.
		Data["apollo"].(map[string]any)["environment"].(map[string]any)
	if environment["name"] != "prod" {
		t.Fatalf("a served coordinate did not answer: %v", environment["name"])
	}
	value, present := environment["namespaceId"]
	if !present {
		t.Fatal("the refused coordinate was omitted from data; GraphQL requires it present and null")
	}
	if value != nil {
		t.Fatalf("an unserved coordinate answered with %v", value)
	}
	if len(response.Errors) != 1 || !strings.Contains(response.Errors[0].Message, "ApolloEnvironment.namespaceId") {
		t.Fatalf("the unserved coordinate produced no refusal naming it: %v", response.Errors)
	}
	if got := response.Errors[0].Path.String(); got != "apollo.environment.namespaceId" {
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
		Environment: "prod", InstallationID: "checkout-worker",
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
		`{ apollo { entity(rid: "aeid:prod:helm-chart:checkout-api") { installedVersion } } }`,
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
		Query: `{ apollo { entity(rid: "aeid:prod:helm-chart:checkout-api") { version } } }`,
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

// ApolloEnvironment.id was declared and refused, which meant Apollo's own
// published example -- `apollo { environments(pageSize: 100) { environments { id } } }`
// -- could not be answered even in the part Conduit does hold. Conduit has one
// human identifier for an Environment and Apollo's contract accepts that value
// as `{environmentId}` on its status routes, so refusing `id` while holding the
// value was the wrong answer.
func TestEnvironmentIdAnswersWithTheIdentifierApollosStatusRoutesAccept(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { id name rid } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	environment := response.Data["apollo"].(map[string]any)["environment"].(map[string]any)
	want := string(apolloSource.environments[0].Name)
	if environment["id"] != want {
		t.Errorf("id resolved to %v, want %q", environment["id"], want)
	}
	// id and name coincide because Conduit holds one identifier, and rid is a
	// different value derived from it. Asserting the rid differs is what stops a
	// later change from quietly answering all three from the same field.
	if environment["rid"] == environment["id"] {
		t.Errorf("rid and id are the same value (%v); they are different identifiers", environment["rid"])
	}
}

// Apollo's own published example, quoted at the top of
// docs/apollo/reference/graphql-api.md as proof the shape is real. It refused
// until now: validation probing cannot see through a list type, so no recovered
// field declared one and every page collection was navigable only in principle.
//
// This is the end-to-end assertion for that fix -- the query Apollo publishes,
// answered with data.
func TestApollosPublishedExampleQueryAnswersWithData(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `query GetEnvironments {
			apollo { environments(pageSize: 100) { environments { id } } }
		}`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("the published example still refuses: %v", response.Errors)
	}
	environments := response.Data["apollo"].(map[string]any)["environments"].(map[string]any)["environments"].([]any)
	if len(environments) != len(apolloSource.environments) {
		t.Fatalf("returned %d environment(s), the source holds %d", len(environments), len(apolloSource.environments))
	}
	if got := environments[0].(map[string]any)["id"]; got != string(apolloSource.environments[0].Name) {
		t.Errorf("id resolved to %v", got)
	}
}

// The other four collections, and the distinction that makes them worth
// asserting separately: an empty collection is a LIST, not a null. "This
// Environment runs nothing" is an answer; a null would say the question went
// unanswered, which is the same conflation the refusals exist to avoid.
func TestEveryDocumentedCollectionAnswersAsAList(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo {
			environments(pageSize: 10) { environments { id } }
			products(pageSize: 10) { products { productId productReleases(pageSize: 10) {
				productReleases { mavenCoordinate manifest { productDependencies { optional } } }
			} } }
			environment(rid: "ri.apollo.main.environment.a") { entities(pageSize: 10) { entities { id } } }
		} }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	root := response.Data["apollo"].(map[string]any)
	products := root["products"].(map[string]any)["products"].([]any)
	if len(products) == 0 {
		t.Fatal("products answered empty")
	}
	releases := products[0].(map[string]any)["productReleases"].(map[string]any)["productReleases"].([]any)
	if len(releases) == 0 {
		t.Fatal("productReleases answered empty")
	}
	entities := root["environment"].(map[string]any)["entities"].(map[string]any)["entities"].([]any)
	if len(entities) == 0 {
		t.Fatal("entities answered empty")
	}
	// The fixture's release declares no dependencies. That must be [] rather
	// than nil: the manifest states the Release depends on nothing.
	dependencies := releases[0].(map[string]any)["manifest"].(map[string]any)["productDependencies"]
	if dependencies == nil {
		t.Error("productDependencies answered null; an empty collection is a list, not an absence")
	}
}

// TestABackendFailureIsNotServedAsAnAbsence is the MAPPED-or-REFUSED rule at
// its sharpest: the interface must not answer a coordinate it could not read.
//
// The resolver swallowed every error from GetEnvironment into a null, on the
// assumption that the only error a projection can raise is not-found. The port
// admits any error. So a backing store that was down was served as
// {"environment": null} with zero errors -- an absence Conduit never observed,
// recorded by any client that reads data.
func TestABackendFailureIsNotServedAsAnAbsence(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	apolloSource.environmentFailure = errors.New("backing store unavailable")

	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name } } }`,
	})

	if len(response.Errors) == 0 {
		t.Fatalf("a failed read was served as an absence: %v", response.Data)
	}
	if !strings.Contains(response.Errors[0].Message, "backing store unavailable") {
		t.Errorf("the failure is not reported: %v", response.Errors[0].Message)
	}
}

// A genuinely missing record still nulls. The fix must not turn every absence
// into an error, which would be the same conflation in the other direction.
func TestAMissingEnvironmentIsStillANull(t *testing.T) {
	resolver, _, _ := newFixture(t)

	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.absent") { name } } }`,
	})

	if len(response.Errors) != 0 {
		t.Fatalf("a missing environment was reported as a failure: %v", response.Errors)
	}
	if got := response.Data["apollo"].(map[string]any)["environment"]; got != nil {
		t.Fatalf("a missing environment answered with %v", got)
	}
}

// TestAMistypedArgumentIsRefusedRatherThanCoercedToEmpty covers the second
// collapse. `rid: 5` passes gqlparser's custom-scalar validation, and the
// discarded type assertion turned it into "", so a malformed query looked up
// the environment whose id is the empty string and returned null -- identical
// to a missing environment.
func TestAMistypedArgumentIsRefusedRatherThanCoercedToEmpty(t *testing.T) {
	resolver, _, _ := newFixture(t)

	for name, query := range map[string]string{
		"literal":  `{ apollo { environment(rid: 5) { name } } }`,
		"variable": `query($rid: RID!) { apollo { environment(rid: $rid) { name } } }`,
	} {
		t.Run(name, func(t *testing.T) {
			request := Request{Query: query}
			if name == "variable" {
				request.Variables = map[string]any{"rid": 5}
			}
			response := resolver.Execute(t.Context(), request)
			if len(response.Errors) == 0 {
				t.Fatalf("a mistyped argument was answered rather than refused: %v", response.Data)
			}
		})
	}
}

// The health fields join two vocabularies, and for a long time they joined the
// wrong ones: Reporting keys an installation by the Environment's plain name
// and the Entity's plain name, while the Apollo projection mints a rid for
// each. Comparing rid to name never matched, so both fields answered UNKNOWN
// for every Environment however healthy the fleet was -- and said in the
// provenance extension that they had answered, which is the part that makes it
// a contract defect rather than a display bug.
//
// It survived because this file's own fixture had been aligned to the broken
// code: reporting's installation carried the rid as its Environment, so the
// resolver compared rid to rid and passed. The fixture beside it always
// described plans by the plain name.
//
// This test pins the vocabularies apart. It fails if either side is re-keyed
// back onto a minted identifier, because then no fixture value can satisfy both
// the projection and reporting at once.
func TestHealthJoinsOnReportingVocabularyNotMintedIdentifiers(t *testing.T) {
	resolver, apolloSource, overview := newFixture(t)

	// Stated rather than assumed: the projection's identifiers and reporting's
	// are different strings, which is the whole reason the join can be wrong.
	minted := string(apolloSource.environments[0].Rid)
	if minted == overview.installations[0].Environment {
		t.Fatal("the fixture no longer distinguishes the minted rid from the reported name")
	}
	if strings.HasPrefix(overview.installations[0].InstallationID, "ri.apollo.main.") {
		t.Fatal("the fixture reports an installation under a minted identifier, which no Hub does")
	}

	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environments(pageSize: 10) { environments { health entities(pageSize: 10) { entities { health } } } } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("the health query was refused: %v", response.Errors)
	}
	environments := response.Data["apollo"].(map[string]any)["environments"].(map[string]any)["environments"].([]any)
	environment := environments[0].(map[string]any)
	if environment["health"] != reporting.HealthHealthy {
		t.Errorf("ApolloEnvironment.health resolved to %v against a healthy fleet", environment["health"])
	}
	entities := environment["entities"].(map[string]any)["entities"].([]any)
	if len(entities) == 0 {
		t.Fatal("the environment reported no entities, so the entity join was never exercised")
	}
	if entities[0].(map[string]any)["health"] != reporting.HealthHealthy {
		t.Errorf("ApolloEntity.health resolved to %v against a healthy fleet",
			entities[0].(map[string]any)["health"])
	}
}

// The disclosure names "the coordinates this response actually answered from".
// A refused coordinate was counted among them: state.selected was recorded
// before the refusal gate, so a query mixing a served field with a refused one
// reported both, and the extension overstated the very compatibility claim it
// exists to bound. Selecting the two together is the only way to see it -- the
// refusal tests assert the error and the count test uses an all-served query,
// so nothing crossed them and the defect sat in plain sight.
func TestARefusedCoordinateIsNotCountedAmongThoseAnswered(t *testing.T) {
	resolver, _, _ := newFixture(t)
	served := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name } } }`,
	})
	withRefusal := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name namespaceId } } }`,
	})
	if len(withRefusal.Errors) != 1 {
		t.Fatalf("the refused coordinate did not refuse: %v", withRefusal.Errors)
	}
	before := served.Extensions["provenance"].(map[string]any)["observedServed"].(int)
	after := withRefusal.Extensions["provenance"].(map[string]any)["observedServed"].(int)
	if after != before {
		t.Fatalf("selecting a refused coordinate raised observedServed from %d to %d; "+
			"a coordinate Conduit refused was reported as one it answered from", before, after)
	}
}

// The same line governs the two lists, and there the failure would be worse
// than an inflated count: a refused coordinate would appear BY NAME among those
// served. That path is unreachable today, and this test is what says so rather
// than leaving a reader to assume it -- every refused coordinate is OBSERVED,
// so refusals only ever reach the integer.
//
// It is written as an assertion about the coverage table rather than as a query
// because there is no query that can exercise the naming path while this holds.
// If someone refuses a DOCUMENTED or CONDUIT_NATIVE coordinate later, this
// fails and tells them the list case now needs its own test.
func TestEveryRefusedCoordinateIsObservedSoRefusalsNeverReachTheNamedLists(t *testing.T) {
	declared, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, coordinate := range declared.Coordinates() {
		if _, served := coverage.SourceOf(coordinate); served {
			continue
		}
		refused++
		tier, _ := declared.TierOf(coordinate)
		if tier != domain.Observed {
			t.Errorf("%s is refused at tier %v; a refusal can now reach documentedServed or "+
				"conduitNativeServed by name, which no test covers", coordinate, tier)
		}
	}
	if refused == 0 {
		t.Fatal("no coordinate is refused, so this test asserts nothing")
	}
}

// An errored coordinate reaches the same recording line by a third path, and it
// is a genuinely independent one: a fix that moved the record past the refusal
// gate but stopped short of resolution would close the refusal case and leave
// this open. That is not hypothetical -- it was a surviving mutation until this
// test existed.
//
// The error has to come from INSIDE resolve. A mistyped argument is rejected
// before the walk begins, so the coordinate is never reached and never counted
// under any variant; an unavailable source is reached, asked, and fails, which
// is the case that distinguishes them.
func TestACoordinateThatFailedToResolveIsNotCountedAmongThoseAnswered(t *testing.T) {
	working, _, _ := newFixture(t)
	served := working.Execute(t.Context(), Request{
		Query: `{ apollo { promotions(pageSize: 10) { promotions { rid } } } }`,
	})
	before := served.Extensions["provenance"].(map[string]any)["documentedServed"].([]string)
	if len(before) == 0 {
		t.Fatal("the working query named no DOCUMENTED coordinate, so this test asserts nothing")
	}

	resolver, _, overview := newFixture(t)
	overview.promotionErr = errors.New("promotions unavailable to this reader")
	failed := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { promotions(pageSize: 10) { promotions { rid } } } }`,
	})
	if len(failed.Errors) == 0 {
		t.Fatal("the unavailable source answered rather than failing")
	}
	after := failed.Extensions["provenance"].(map[string]any)["documentedServed"].([]string)
	for _, named := range after {
		if named == "ApolloQuery.promotions" {
			t.Fatalf("the coordinate whose source failed was named among those answered: %v "+
				"(a working query names %v)", after, before)
		}
	}
}

// The disclosure that was missing, and the reason ADR 0007 needed a mechanism
// rather than a note in a document.
//
// ApolloEnvironment.rid is a recovered coordinate answered from a value Conduit
// computes: the grammar ri.apollo.main.environment.<locator> is published, the
// locator is a truncated digest Conduit chose. Before this, the response said
// OBSERVED and the note said OBSERVED coordinates "were recovered from an
// authorized Apollo Hub", so a client had no way to learn the string had never
// been near one. That is not a documentation problem -- the client never reads
// our documentation.
func TestARecoveredCoordinateAnsweredFromAConduitBuiltValueSaysSo(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name rid } } }`,
	})
	provenance := response.Extensions["provenance"].(map[string]any)
	minted := provenance["conduitMintedValues"].([]string)
	if len(minted) != 1 || minted[0] != "ApolloEnvironment.rid" {
		t.Fatalf("the minted-value disclosure is wrong: %v", minted)
	}
	// It stays counted as observed. The coordinate really was recovered, and
	// moving it out would understate the recovery to fix an overstatement about
	// the value -- two errors rather than none.
	if provenance["observedServed"].(int) != 4 {
		t.Fatalf("disclosing a minted value changed the observed count: %v", provenance["observedServed"])
	}
	// And it is not laundered into either lower tier, which would say Apollo
	// makes no claim about the coordinate. Apollo declares it; only the value is
	// ours.
	if native := provenance["conduitNativeServed"].([]string); len(native) != 0 {
		t.Fatalf("a recovered coordinate was disclosed as Conduit's invention: %v", native)
	}
	note := provenance["note"].(string)
	for _, required := range []string{"conduitMintedValues", "subset of observedServed", "round-trip"} {
		if !strings.Contains(note, required) {
			t.Errorf("the note no longer explains %q, so the list has no stated meaning", required)
		}
	}
}

// The other half: a query that touches no minted value must say so. A
// disclosure that always fires tells a client nothing, and this one names
// exactly the fields whose values cannot be used against a real Apollo API.
func TestAQueryTouchingNoMintedValueDisclosesNone(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environment(rid: "ri.apollo.main.environment.a") { name } } }`,
	})
	provenance := response.Extensions["provenance"].(map[string]any)
	if minted := provenance["conduitMintedValues"].([]string); len(minted) != 0 {
		t.Fatalf("a query selecting no minted value disclosed one: %v", minted)
	}
}

// TestPageSizeIsNotHonouredAndTheDeclarationSaysSo pins a DECLARED divergence
// rather than a fixed defect, and it is written that way on purpose.
//
// `pageSize` is a required argument on every collection field and no resolver
// reads it (issue #112, finding 5). The undisclosed part was the defect; the
// behaviour itself cannot be corrected without minting a page-token grammar no
// capture establishes, which would put a Conduit-invented string behind an
// OBSERVED coordinate -- the exact shape issues #57 and #112 report. Truncating
// without a token would be worse: the client would read a short page as the end
// of the collection.
//
// So the divergence stands, is declared in docs/operations/TENANT-FIDELITY.md,
// and is pinned here so that "we know and we said so" cannot quietly become
// "we know and we stopped saying so". If someone does implement paging, this
// test fails and the declaration has to be revisited in the same change.
func TestPageSizeIsNotHonouredAndTheDeclarationSaysSo(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	apolloSource.environments = append(apolloSource.environments,
		apollo.Environment{Name: "staging", Rid: "ri.apollo.main.environment.b"},
		apollo.Environment{Name: "dev", Rid: "ri.apollo.main.environment.c"})
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { environments(pageSize: 1) { nextPageToken environments { id } } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	page := response.Data["apollo"].(map[string]any)["environments"].(map[string]any)
	if got := len(page["environments"].([]any)); got != 3 {
		t.Fatalf("environments(pageSize: 1) returned %d of 3; pageSize is now honoured, so the "+
			"declared divergence in docs/operations/TENANT-FIDELITY.md is stale and the page token "+
			"grammar it warns about needs an answer", got)
	}
	// Null is consistent with the above and is a value, not a refusal: a page
	// that is the whole collection has no next one.
	if token, present := page["nextPageToken"]; !present || token != nil {
		t.Fatalf("nextPageToken = %#v (present=%v); a token here would be a Conduit-invented cursor "+
			"behind a recovered coordinate", token, present)
	}
}

// The minted value that is not an identifier, and the one issues #57 and #112
// named: a dependency's optionality.
//
// Neither pinned publish request model carries an optionality input, so a
// dependency published on the public Apollo routes arrives with nobody having
// stated whether it is optional, and this coordinate answered "false" for it
// under a bare OBSERVED annotation -- telling the client an Apollo Hub had said
// so. The value does not change; what changes is that the response now says
// whose it is.
func TestAnUndeclaredDependencyOptionalityIsDisclosedAsConduitsOwn(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	// Optional is nil, which is what the public publish path can express: the
	// request model has no field for it.
	dependencies := []apollo.ProductDependency{{ProductId: ptr(apollo.ProductId("billing"))}}
	apolloSource.releases["checkout"] = []apollo.Release{{
		MavenCoordinate: "com.example:checkout:1.4.2", ProductDependencies: &dependencies,
	}}
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { products(pageSize: 10) { products { productReleases(pageSize: 10) {
			productReleases { manifest { productDependencies { optional } } }
		} } } } }`,
	})
	if len(response.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", response.Errors)
	}
	products := response.Data["apollo"].(map[string]any)["products"].(map[string]any)["products"].([]any)
	releases := products[0].(map[string]any)["productReleases"].(map[string]any)["productReleases"].([]any)
	served := releases[0].(map[string]any)["manifest"].(map[string]any)["productDependencies"].([]any)
	if len(served) != 1 || served[0].(map[string]any)["optional"] != "false" {
		t.Fatalf("optional resolved to %#v; the value itself is unchanged", served)
	}

	provenance := response.Extensions["provenance"].(map[string]any)
	minted := provenance["conduitMintedValues"].([]string)
	found := false
	for _, coordinate := range minted {
		if coordinate == "ApolloProductDependency.optional" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a query that read a dependency's optionality disclosed no minted value: %v; "+
			"the client is being told an Apollo Hub answered with Conduit's default", minted)
	}
	// Still OBSERVED. Apollo declares the coordinate; only the value is ours,
	// and demoting the coordinate would understate the recovery to correct an
	// overstatement about the value.
	if native := provenance["conduitNativeServed"].([]string); len(native) != 0 {
		t.Fatalf("a recovered coordinate was disclosed as Conduit's invention: %v", native)
	}
}

// --- Conduit-namespaced resource identifiers --------------------------------

// The grammar is Apollo's and the namespace is not, which is the whole design.
// A value that leaked into a log or a bug report has to be recognisable as ours
// without anyone holding the schema, and it must never be mistakable for an
// identifier that names something on a real Hub.
func TestMintedIdentifiersFollowTheGrammarInConduitsOwnNamespace(t *testing.T) {
	t.Parallel()
	for name, minted := range map[string]string{
		"product": productRID("checkout"),
		"release": productReleaseRID("com.example:checkout:1.4.2"),
	} {
		if !strings.HasPrefix(minted, "ri.conduit.main.") {
			t.Errorf("%s identifier %q is not in Conduit's namespace", name, minted)
		}
		if strings.HasPrefix(minted, "ri.apollo.") {
			t.Errorf("%s identifier %q claims Apollo's namespace", name, minted)
		}
		// ri.<service>.<instance>.<type>.<locator>, which is the published shape.
		if segments := strings.Split(minted, "."); len(segments) != 5 {
			t.Errorf("%s identifier %q has %d segments, want the published five", name, minted, len(segments))
		}
	}
}

// Stable across calls, because the identifier is derived rather than stored: if
// it moved between requests a client could not hold one, and the release lookup
// -- which re-mints every candidate to compare -- would never match.
func TestMintedIdentifiersAreStableAndDistinct(t *testing.T) {
	t.Parallel()
	// Through variables rather than inline, because staticcheck reads two
	// identical call expressions as a mistake (SA4000) -- reasonably, since it
	// cannot know the point is that the function is deterministic.
	first, second := productRID("checkout"), productRID("checkout")
	if first != second {
		t.Errorf("the same Product minted two different identifiers: %q and %q", first, second)
	}
	if productRID("checkout") == productRID("billing") {
		t.Error("two Products minted the same identifier")
	}
	// A Product and a Release of it must differ, or a lookup keyed on one would
	// answer for the other.
	if productRID("checkout") == productReleaseRID("checkout") {
		t.Error("a Product and a Release with the same key minted the same identifier")
	}
	if productReleaseRID("com.example:checkout:1.4.2") == productReleaseRID("com.example:checkout:1.4.3") {
		t.Error("two versions of one Product minted the same identifier")
	}
}

func TestAProductAndItsReleaseServeTheirMintedIdentifiers(t *testing.T) {
	resolver, _, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { products(pageSize: 10) { products { rid productId productReleases(pageSize: 10) { productReleases { rid mavenCoordinate } } } } } }`,
	})
	if len(response.Errors) > 0 {
		t.Fatalf("query failed: %v", response.Errors)
	}
	products := response.Data["apollo"].(map[string]any)["products"].(map[string]any)["products"].([]any)
	product := products[0].(map[string]any)
	if product["rid"] != productRID("checkout") {
		t.Errorf("Product rid is %v, want the minted %v", product["rid"], productRID("checkout"))
	}
	releases := product["productReleases"].(map[string]any)["productReleases"].([]any)
	release := releases[0].(map[string]any)
	if release["rid"] != productReleaseRID("com.example:checkout:1.4.2") {
		t.Errorf("Release rid is %v, want the minted one", release["rid"])
	}
	// The disclosure is the point. Serving these without it is the state this
	// change exists to end.
	minted := response.Extensions["provenance"].(map[string]any)["conduitMintedValues"].([]string)
	for _, required := range []string{"ApolloProduct.rid", "ApolloProductRelease.rid"} {
		if !slices.Contains(minted, required) {
			t.Errorf("%s served a Conduit-built value without disclosing it: %v", required, minted)
		}
	}
}

func TestAReleaseIsReachableByItsMintedIdentifier(t *testing.T) {
	resolver, _, _ := newFixture(t)
	rid := productReleaseRID("com.example:checkout:1.4.2")
	response := resolver.Execute(t.Context(), Request{
		Query:     `query Release($rid: RID!) { apollo { productRelease(rid: $rid) { mavenCoordinate } } }`,
		Variables: map[string]any{"rid": rid},
	})
	if len(response.Errors) > 0 {
		t.Fatalf("query failed: %v", response.Errors)
	}
	release := response.Data["apollo"].(map[string]any)["productRelease"].(map[string]any)
	if release["mavenCoordinate"] != "com.example:checkout:1.4.2" {
		t.Errorf("the minted identifier resolved to %v", release["mavenCoordinate"])
	}
}

// An Apollo-shaped identifier is refused rather than searched for. Scanning
// would spend the catalogue to reach the same answer, and would then report "no
// such release" about an identifier that may well name a real one on a real Hub
// -- an absence Conduit is in no position to assert.
func TestAnApolloShapedIdentifierIsNotSearchedForAmongConduitsReleases(t *testing.T) {
	resolver, apolloSource, _ := newFixture(t)
	response := resolver.Execute(t.Context(), Request{
		Query: `{ apollo { productRelease(rid: "ri.apollo.main.product-release.123") { mavenCoordinate } } }`,
	})
	if len(response.Errors) > 0 {
		t.Fatalf("query failed: %v", response.Errors)
	}
	if release := response.Data["apollo"].(map[string]any)["productRelease"]; release != nil {
		t.Errorf("an Apollo identifier resolved against Conduit's catalogue: %v", release)
	}
	// The null alone proves nothing -- a full scan that found nothing returns the
	// same null. What is being pinned is that the catalogue was never read, which
	// is the only observable difference between refusing an identifier that
	// cannot match and searching for it anyway.
	if apolloSource.catalogueReads != 0 {
		t.Errorf("the catalogue was read %d time(s) for an identifier Conduit never minted", apolloSource.catalogueReads)
	}
}
