package application

import "github.com/jonathanagustin/conduit/internal/apollographql/domain"

func at(typeName, field string) domain.Coordinate {
	return domain.Coordinate{Type: typeName, Field: field}
}

// coverage is the single statement of what Conduit can answer and from where.
//
// It is a table rather than a property of the resolver switch because the two
// questions it has to survive are different: the switch answers "how", and this
// answers "whether". Keeping "whether" as data lets Reconcile compare it
// against the served SDL, so a coordinate the schema declares and nothing
// serves is a build failure when it was invented and a published gap when it
// was recovered -- neither of which a switch statement could be asked.
//
// A coordinate absent from this table is refused at request time. That is the
// default on purpose: forgetting to add a field here makes it unanswerable,
// while forgetting to remove one makes the build fail. Silence should cost
// coverage, never truth.
var coverage = domain.NewCoverage(map[domain.Coordinate]string{
	at("Query", "me"):                     "native: request-bound resolved credential owner",
	at("ConduitCurrentUser", "id"):        "native: resolved credential owner username, valueTier CONDUIT_NATIVE",
	at("ConduitCurrentUser", "fullName"):  "native: optional identity display name, valueTier CONDUIT_NATIVE",
	at("ApolloEnvironment", "spaceId"):    "native: persisted authorized Environment Space assignment",
	at("ApolloEntity", "spaceId"):         "native: persisted authorized owning Environment Space assignment",
	at("ApolloProduct", "spaceId"):        "native: persisted authorized Product Space assignment",
	at("ApolloProductRelease", "spaceId"): "native: persisted authorized owning Product Space assignment",
	at("ApolloBundle", "spaceId"):         "native: persisted authorized Bundle Space assignment",
	at("ApolloQuery", "bundle"):           "native: authorized Bundle identity catalogue",
	at("ApolloQuery", "bundles"):          "native: authorized Bundle identity catalogue; recovered scalar argument and result",
	at("ApolloBundle", "rid"):             "native: minted from persisted Bundle identity, valueTier CONDUIT_NATIVE",

	// --- Recovered from an Apollo Hub, and answerable from Conduit's Apollo
	// compatibility projection. These are the same records the Apollo REST
	// surface serves, so the protocol a client picks cannot change the answer.
	at("Query", "apollo"):                        "the entry point the recovery record walked from",
	at("ApolloQuery", "environment"):             "apollo: Service.GetEnvironment",
	at("ApolloQuery", "environments"):            "apollo: Service.ListEnvironments",
	at("ApolloQuery", "entity"):                  "apollo: Service.ListInstallations",
	at("ApolloQuery", "products"):                "apollo: Service.ListProducts",
	at("ApolloEnvironment", "name"):              "apollo: Environment.Name",
	at("ApolloEnvironment", "id"):                "apollo: Environment.Name, the value the status routes accept as {environmentId}",
	at("ApolloEnvironment", "rid"):               "apollo: Environment.Rid",
	at("ApolloEnvironment", "entities"):          "apollo: Service.ListInstallations",
	at("ApolloEnvironmentPage", "nextPageToken"): "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	// The five collections the probe could not type. Their cardinality is cited
	// in documented-cardinality.json and verified against the pinned snapshot;
	// the records themselves were always here.
	at("ApolloEnvironmentPage", "environments"):               "apollo: Service.ListEnvironments",
	at("ApolloEntityPage", "entities"):                        "apollo: Service.ListInstallations",
	at("ApolloProductPage", "products"):                       "apollo: Service.ListProducts",
	at("ApolloProductReleasePage", "productReleases"):         "apollo: Service.ListReleases",
	at("ApolloProductReleaseManifest", "productDependencies"): "apollo: Release.ProductDependencies",
	at("ApolloEntity", "id"):                                  "apollo: Installation.ApolloEntityId",
	at("ApolloEntity", "packageRid"):                          "apollo: Installation.PackageRid",
	at("ApolloEntity", "environment"):                         "apollo: Installation.EnvironmentRid",
	at("ApolloEntityPage", "nextPageToken"):                   "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	at("ApolloProduct", "productId"):                          "apollo: Product.Id",
	at("ApolloProduct", "rid"):                                "conduit: minted from Product.Id, disclosed as valueTier CONDUIT_NATIVE",
	at("ApolloProductRelease", "rid"):                         "conduit: minted from Release.MavenCoordinate, disclosed as valueTier CONDUIT_NATIVE",
	at("ApolloQuery", "productRelease"):                       "apollo: Service.ListProducts and Service.ListReleases, matched on the minted rid",
	at("ApolloProduct", "productReleases"):                    "apollo: Service.ListReleases",
	at("ApolloProductPage", "nextPageToken"):                  "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	at("ApolloProductRelease", "mavenCoordinate"):             "apollo: Release.MavenCoordinate",
	at("ApolloProductRelease", "product"):                     "apollo: Product owning the release",
	at("ApolloProductRelease", "manifest"):                    "apollo: Release upgrade strategy and dependencies",
	at("ApolloProductReleasePage", "nextPageToken"):           "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	at("ApolloProductReleaseManifest", "upgradeStrategy"):     "apollo: Release.UpgradeStrategy",
	at("ApolloProductDependency", "optional"):                 "apollo: ProductDependency.Optional, defaulted and rendered by Conduit, disclosed as valueTier CONDUIT_NATIVE",
	at("ApolloProductDependency", "product"):                  "apollo: ProductDependency product coordinate",

	// --- Added to the interface by Conduit because no Hub established them in
	// GraphQL, and answered from Reporting's already-authorized fleet snapshot.
	// Every one of these is displaced the moment a capture recovers the same
	// coordinate.
	//
	// Most of them carry Apollo's own published name rather than one Conduit
	// chose (the SDL tiers them DOCUMENTED); the field names here are the names
	// the pinned OpenAPI and the CLI publish, which is what displacement means.
	// The coverage table does not restate the tier, because the SDL states it and
	// a second copy would be a second thing to drift.
	at("ApolloQuery", "plans"):          "reporting: OverviewSource.VisiblePlans",
	at("ApolloQuery", "plan"):           "reporting: OverviewSource.VisiblePlans",
	at("ApolloQuery", "changeRequests"): "reporting: OverviewSource.VisibleChangeRequests",
	at("ApolloQuery", "promotions"):     "reporting: OverviewSource.VisiblePromotions",
	at("ApolloEnvironment", "health"):   "reporting: OverviewSource.VisibleInstallations, aggregated per environment",
	at("ApolloEntity", "health"):        "reporting: OverviewSource.VisibleInstallations",
	// NOT "as reported by the Apollo Agent", which is what this said and what
	// the pinned snapshot's own description of Installation.version claims.
	// Conduit populates that field from the reviewed DESIRED state, and reported
	// versions reach the Environments Report without ever flowing back into this
	// projection (issues #57, #112). The value is real Conduit data under a
	// contract sentence that describes different data, so the coverage table
	// states which data it actually is; the divergence is declared in
	// docs/reference-platform/public-api.md and docs/operations/TENANT-FIDELITY.md.
	at("ApolloEntity", "version"):                   "apollo: Installation.Version, the reviewed desired version, NOT an Agent report",
	at("ApolloPlan", "rid"):                         "reporting: PlanSummary.ID",
	at("ApolloPlan", "environmentRid"):              "reporting: PlanSummary.Environment",
	at("ApolloPlan", "status"):                      "reporting: PlanSummary.Phase",
	at("ApolloPlanPage", "plans"):                   "reporting: OverviewSource.VisiblePlans",
	at("ApolloPlanPage", "nextPageToken"):           "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	at("ApolloChangeRequest", "rid"):                "reporting: ChangeRequestSummary.ID",
	at("ApolloChangeRequest", "title"):              "reporting: ChangeRequestSummary.Title",
	at("ApolloChangeRequest", "status"):             "reporting: ChangeRequestSummary.AwaitingApproval",
	at("ApolloChangeRequestPage", "changeRequests"): "reporting: OverviewSource.VisibleChangeRequests",
	at("ApolloChangeRequestPage", "nextPageToken"):  "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
	at("ApolloPromotion", "rid"):                    "reporting: PromotionSummary.ID",
	at("ApolloPromotion", "productId"):              "reporting: PromotionSummary.Product",
	at("ApolloPromotion", "status"):                 "reporting: PromotionSummary.Blocked",
	at("ApolloPromotionPage", "promotions"):         "reporting: OverviewSource.VisiblePromotions",
	at("ApolloPromotionPage", "nextPageToken"):      "pagination: always absent; pageSize is not honoured, so every page is the whole collection",
})

// Coverage is the table above, exposed so a test and the operator-facing report
// can be built from the same statement the resolver refuses against.
func Coverage() domain.Coverage { return coverage }
