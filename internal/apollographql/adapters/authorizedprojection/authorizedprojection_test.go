package authorizedprojection_test

import (
	"errors"
	"testing"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/authorizedprojection"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// recordingProjection answers everything and records that it was asked, so a
// test can tell a refusal that never reached the records from one that reached
// them and threw the answer away. Only the first is an authorization guard.
type recordingProjection struct {
	products []apollo.Product
	reached  []string
}

func (projection *recordingProjection) ListEnvironments() (apollo.ListEnvironmentsResponse, error) {
	projection.reached = append(projection.reached, "ListEnvironments")
	return apollo.ListEnvironmentsResponse{}, nil
}

func (projection *recordingProjection) GetEnvironment(rid string) (apollo.Environment, error) {
	projection.reached = append(projection.reached, "GetEnvironment")
	return apollo.Environment{Rid: apollo.EnvironmentRid(rid)}, nil
}

func (projection *recordingProjection) ListInstallations(string) (apollo.ListInstallationsResponse, error) {
	projection.reached = append(projection.reached, "ListInstallations")
	return apollo.ListInstallationsResponse{}, nil
}

func (projection *recordingProjection) ListProducts() (apollo.ListProductsResponse, error) {
	projection.reached = append(projection.reached, "ListProducts")
	products := projection.products
	return apollo.ListProductsResponse{Products: &products}, nil
}

func (projection *recordingProjection) GetProduct(product string) (apollo.Product, error) {
	projection.reached = append(projection.reached, "GetProduct")
	return apollo.Product{Id: apollo.ProductId(product)}, nil
}

func (projection *recordingProjection) ListReleases(string) (apollo.ListReleasesResponse, error) {
	projection.reached = append(projection.reached, "ListReleases")
	return apollo.ListReleasesResponse{}, nil
}

func (projection *recordingProjection) GetRelease(string, string) (apollo.Release, error) {
	projection.reached = append(projection.reached, "GetRelease")
	return apollo.Release{}, nil
}

func allowOnly(permitted ...string) authorizedprojection.ReadsResource {
	set := map[string]struct{}{}
	for _, resource := range permitted {
		set[resource] = struct{}{}
	}
	return func(resourceRID string) bool {
		_, allowed := set[resourceRID]
		return allowed
	}
}

// The coordinate table, asserted one read at a time against the same table the
// Apollo REST surface authorizes with. A reader holding platform/root reaches
// the installation-wide reads and nothing about a Product; a reader holding
// global/product reaches the catalogue and, because Governance expands
// product/<id> to include global/product, every Product under it.
func TestEachReadIsAuthorizedAgainstItsOwnCoordinate(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		permitted  []string
		environies bool
		catalogue  bool
		product    bool
	}{
		{name: "platform only", permitted: []string{"platform/root"}, environies: true},
		{name: "catalogue only", permitted: []string{"global/product", "product/com.example:app"}, catalogue: true, product: true},
		{name: "one product only", permitted: []string{"product/com.example:app"}, product: true},
		{name: "nothing", permitted: nil},
		{
			name:       "platform and catalogue",
			permitted:  []string{"platform/root", "global/product", "product/com.example:app"},
			environies: true, catalogue: true, product: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			inner := &recordingProjection{}
			guarded := authorizedprojection.New(inner, allowOnly(testCase.permitted...))

			_, environmentsErr := guarded.ListEnvironments()
			_, environmentErr := guarded.GetEnvironment("ri.conduit.main.environment.a")
			_, installationsErr := guarded.ListInstallations("ri.conduit.main.environment.a")
			_, productsErr := guarded.ListProducts()
			_, productErr := guarded.GetProduct("com.example:app")
			_, releasesErr := guarded.ListReleases("com.example:app")
			_, releaseErr := guarded.GetRelease("com.example:app", "1.0.0")

			for _, observed := range []struct {
				read  string
				err   error
				grant bool
			}{
				{read: "ListEnvironments", err: environmentsErr, grant: testCase.environies},
				{read: "GetEnvironment", err: environmentErr, grant: testCase.environies},
				{read: "ListInstallations", err: installationsErr, grant: testCase.environies},
				{read: "ListProducts", err: productsErr, grant: testCase.catalogue},
				{read: "GetProduct", err: productErr, grant: testCase.product},
				{read: "ListReleases", err: releasesErr, grant: testCase.product},
				{read: "GetRelease", err: releaseErr, grant: testCase.product},
			} {
				refused := errors.Is(observed.err, ports.ErrNotAuthorized)
				if refused == observed.grant {
					t.Errorf("%s: authorized=%v but err=%v", observed.read, observed.grant, observed.err)
				}
			}
		})
	}
}

// A refusal must not have touched the records. Reading and discarding would
// leave the projection free to log, count or cache a read the reader was never
// entitled to make, and would make a later "filter instead of refuse" change
// look like a small one.
func TestARefusedReadNeverReachesTheRecords(t *testing.T) {
	t.Parallel()
	inner := &recordingProjection{}
	guarded := authorizedprojection.New(inner, allowOnly())

	_, _ = guarded.ListEnvironments()
	_, _ = guarded.ListProducts()
	_, _ = guarded.ListReleases("com.example:app")
	_, _ = guarded.GetRelease("com.example:app", "1.0.0")

	if len(inner.reached) != 0 {
		t.Fatalf("a refused reader reached the projection: %v", inner.reached)
	}
}

// A refusal is ErrNotAuthorized and never ErrRecordNotFound, because the
// resolver turns the latter into a null. A null would tell the reader the
// record does not exist, when what happened is that they may not see it.
func TestARefusalIsNeverSpelledAsAMissingRecord(t *testing.T) {
	t.Parallel()
	guarded := authorizedprojection.New(&recordingProjection{}, allowOnly())

	_, err := guarded.GetProduct("com.example:app")

	if errors.Is(err, ports.ErrRecordNotFound) {
		t.Fatalf("a refusal was spelled as a missing record, which the resolver serves as null: %v", err)
	}
	if !errors.Is(err, ports.ErrNotAuthorized) {
		t.Fatalf("GetProduct refusal = %v", err)
	}
}

// A guard built without an authorizer denies. A wiring mistake that opened the
// projection is the exact failure this package exists to prevent, so the
// degenerate case has to fail closed rather than fail open.
func TestAGuardWithNoAuthorizerDenies(t *testing.T) {
	t.Parallel()
	guarded := authorizedprojection.New(&recordingProjection{}, nil)

	if _, err := guarded.ListEnvironments(); !errors.Is(err, ports.ErrNotAuthorized) {
		t.Fatalf("a guard with no authorizer served: %v", err)
	}
	if authorizedprojection.AnyReadable(&recordingProjection{}, nil) {
		t.Error("a nil authorizer reported the projection readable")
	}
}

// A nil projection stays nil. The binder uses nil for "this half is
// unavailable", and a guard over nothing would report a refusal instead --
// which says the reader lacks a grant when the truth is that Conduit has no
// projection to offer.
func TestANilProjectionIsNotWrapped(t *testing.T) {
	t.Parallel()
	if authorizedprojection.New(nil, allowOnly("platform/root")) != nil {
		t.Fatal("a nil projection was wrapped in a guard")
	}
}

// AnyReadable is the door check, and it must open for a reader whose only grant
// is on one Product -- the Apollo REST surface serves that reader, so refusing
// them at the door would put back a smaller copy of the same asymmetry.
func TestAnyReadableOpensForASingleProductGrant(t *testing.T) {
	t.Parallel()
	inner := &recordingProjection{products: []apollo.Product{{Id: "com.example:app"}}}

	if !authorizedprojection.AnyReadable(inner, allowOnly("product/com.example:app")) {
		t.Error("a reader granted one Product was refused at the door")
	}
	if authorizedprojection.AnyReadable(inner, allowOnly("product/com.other:thing")) {
		t.Error("a reader granted a Product this installation does not hold was let through")
	}
	if authorizedprojection.AnyReadable(inner, allowOnly("platform/root")) == false {
		t.Error("a platform-scoped reader was refused at the door")
	}
}

// The broad coordinates settle the door check without a catalogue read, so a
// platform-scoped reader does not pay for an enumeration that cannot change the
// answer.
func TestAnyReadableSkipsTheCatalogueWhenABroadGrantAnswers(t *testing.T) {
	t.Parallel()
	inner := &recordingProjection{products: []apollo.Product{{Id: "com.example:app"}}}

	authorizedprojection.AnyReadable(inner, allowOnly("global/product"))

	for _, reached := range inner.reached {
		if reached == "ListProducts" {
			t.Fatal("the door check enumerated the catalogue although a broad grant already answered")
		}
	}
}

// A nil projection is not a refusal for a reader holding a broad grant. Only
// the per-Product enumeration needs records, so only it stops; the reader still
// reaches the other half of the interface.
func TestAnyReadableWithNoProjectionStillHonoursABroadGrant(t *testing.T) {
	t.Parallel()
	if !authorizedprojection.AnyReadable(nil, allowOnly("platform/root")) {
		t.Error("a platform-scoped reader was refused because the Apollo projection was unavailable")
	}
	if authorizedprojection.AnyReadable(nil, allowOnly("product/com.example:app")) {
		t.Error("a per-Product grant was honoured with no catalogue to check it against")
	}
}
