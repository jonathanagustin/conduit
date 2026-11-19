package compatibilityprojection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo"
	app "github.com/jonathanagustin/conduit/internal/apollo/application"
	ports "github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

type readCall struct {
	ctx    context.Context
	method string
	args   []string
}
type readerFixture struct {
	calls   []readCall
	failure error
}

func (r *readerFixture) call(ctx context.Context, method string, args ...string) {
	r.calls = append(r.calls, readCall{ctx, method, args})
}
func (r *readerFixture) ListEnvironments(ctx context.Context) (apollo.ListEnvironmentsResponse, error) {
	r.call(ctx, "environments")
	v := []apollo.Environment{{Name: "native", Rid: "environment-rid"}}
	return apollo.ListEnvironmentsResponse{Environments: &v}, r.failure
}
func (r *readerFixture) ListInstallations(ctx context.Context, id string) (apollo.ListInstallationsResponse, error) {
	r.call(ctx, "installations", id)
	v := []apollo.Installation{{ApolloEntityId: "entity", EnvironmentRid: id}}
	return apollo.ListInstallationsResponse{Installations: &v}, r.failure
}
func (r *readerFixture) ListProducts() apollo.ListProductsResponse {
	r.calls = append(r.calls, readCall{method: "products"})
	v := []apollo.Product{{Id: "group:product"}}
	return apollo.ListProductsResponse{Products: &v}
}
func (r *readerFixture) ListReleases(ctx context.Context, id string) (apollo.ListReleasesResponse, error) {
	r.call(ctx, "releases", id)
	v := []apollo.Release{{MavenCoordinate: "coordinate"}}
	return apollo.ListReleasesResponse{Releases: &v}, r.failure
}
func (r *readerFixture) GetEnvironment(ctx context.Context, id string) (apollo.Environment, error) {
	r.call(ctx, "environment", id)
	return apollo.Environment{Rid: id}, r.failure
}
func (r *readerFixture) GetProduct(id string) (apollo.Product, error) {
	r.calls = append(r.calls, readCall{method: "product", args: []string{id}})
	return apollo.Product{Id: id}, r.failure
}
func (r *readerFixture) GetRelease(ctx context.Context, id, version string) (apollo.Release, error) {
	r.call(ctx, "release", id, version)
	return apollo.Release{MavenCoordinate: id + ":" + version}, r.failure
}

func TestProjectionForwardsAllReadsAndRetainsRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &readerFixture{}
	projection := New(ctx, reader)
	environments, err := projection.ListEnvironments()
	if err != nil || (*environments.Environments)[0].Rid != "environment-rid" {
		t.Fatalf("environments %#v %v", environments, err)
	}
	installs, err := projection.ListInstallations("env")
	if err != nil || (*installs.Installations)[0].EnvironmentRid != "env" {
		t.Fatalf("installations %#v %v", installs, err)
	}
	products, err := projection.ListProducts()
	if err != nil || (*products.Products)[0].Id != "group:product" {
		t.Fatalf("products %#v %v", products, err)
	}
	releases, err := projection.ListReleases("group:product")
	if err != nil || (*releases.Releases)[0].MavenCoordinate != "coordinate" {
		t.Fatalf("releases %#v %v", releases, err)
	}
	env, err := projection.GetEnvironment("env")
	if err != nil || env.Rid != "env" {
		t.Fatalf("environment %#v %v", env, err)
	}
	product, err := projection.GetProduct("group:product")
	if err != nil || product.Id != "group:product" {
		t.Fatalf("product %#v %v", product, err)
	}
	release, err := projection.GetRelease("group:product", "V98")
	if err != nil || release.MavenCoordinate != "group:product:V98" {
		t.Fatalf("release %#v %v", release, err)
	}
	expected := []readCall{{ctx, "environments", nil}, {ctx, "installations", []string{"env"}}, {nil, "products", nil}, {ctx, "releases", []string{"group:product"}}, {ctx, "environment", []string{"env"}}, {nil, "product", []string{"group:product"}}, {ctx, "release", []string{"group:product", "V98"}}}
	if !reflect.DeepEqual(reader.calls, expected) {
		t.Fatalf("request scope changed: %#v", reader.calls)
	}
	if New(ctx, nil) != nil {
		t.Fatal("absent projection manufactured a reader")
	}
}
func TestOnlyClassifiedGetAbsenceMapsToGraphQLNullSentinel(t *testing.T) {
	missing := fmt.Errorf("retained cause: %w", &app.Error{Kind: app.NotFound, Message: "absent"})
	for _, failure := range []error{nil, missing, &app.Error{Kind: app.Invalid, Message: "invalid"}, &app.Error{Kind: app.Conflict, Message: "conflict"}, context.Canceled, errors.New("backend unavailable")} {
		reader := &readerFixture{failure: failure}
		projection := New(t.Context(), reader)
		for _, get := range []func() error{
			func() error { _, err := projection.GetEnvironment("env"); return err },
			func() error { _, err := projection.GetProduct("product"); return err },
			func() error { _, err := projection.GetRelease("product", "version"); return err },
		} {
			err := get()
			if failure == missing {
				if !errors.Is(err, ports.ErrRecordNotFound) {
					t.Fatalf("absence lost: %v", err)
				}
			} else if err != failure {
				t.Fatalf("failure changed from %v to %v", failure, err)
			}
		}
		for _, list := range []func() error{
			func() error { _, err := projection.ListEnvironments(); return err },
			func() error { _, err := projection.ListInstallations("env"); return err },
			func() error { _, err := projection.ListReleases("product"); return err },
		} {
			if err := list(); err != failure {
				t.Fatalf("listing error changed: %v -> %v", failure, err)
			}
		}
	}
}
