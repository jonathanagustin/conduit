package controlplane

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/apollo"
	reportadapter "github.com/jonathanagustin/conduit/internal/apollo/adapters/reporting"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// compatibilityProjectionRepository composes only the existing publication,
// asset-retention and Environment projection capabilities used by this facade.
// Governance remains a separate service; lifecycle commands and leases are absent.
type compatibilityProjectionRepository interface {
	publicationCatalogRepository
	publicationAssetWriter
	environmentReportingProjectionRepository
}

// apolloPublicAPI is the outer anti-corruption adapter. Identity and governance
// stay at the HTTP edge; generated DTO translation and compatibility state live
// in the Apollo application context behind Catalog and Environment ports.
type apolloPublicAPI struct {
	publicationAssets   publicationAssetWriter
	governance          *GovernanceService
	configuration       HandlerOptions
	initializationError error
	application         *apolloapp.Service
}

func newApolloPublicAPI(repository compatibilityProjectionRepository, governance *GovernanceService, configuration HandlerOptions) *apolloPublicAPI {
	api := &apolloPublicAPI{publicationAssets: repository, governance: governance, configuration: configuration}
	service, err := apolloapp.New(
		context.Background(),
		publicationCatalogPort{repository: repository},
		reportadapter.New(environmentReportingProjectionPort{repository: repository}),
		releaseLifecycleProjectionPort{repository: repository},
		environmentInventoryProjectionPort{repository: repository},
	)
	if err != nil {
		api.initializationError = err
		return api
	}
	api.application = service
	return api
}

func (api *apolloPublicAPI) register(mux *http.ServeMux) {
	registerApolloOpenAPI(mux, api)
}

// publishHelmBundle is a Conduit-native semantic publication route. It remains
// separate from the pinned public OpenAPI so platform assets cannot silently
// become undocumented Apollo request fields.
func (api *apolloPublicAPI) publishHelmBundle(w http.ResponseWriter, request *http.Request) {
	principal, ok := api.resolveOperator(w, request)
	if !ok {
		return
	}
	bundle, ok := decodeJSONWithLimit[HelmProductReleaseBundle](w, request, 3<<20)
	if !ok {
		return
	}
	if err := ValidateHelmProductReleaseBundle(bundle); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	coordinate := bundle.Manifest.ProductGroup + ":" + bundle.Manifest.ProductName + ":" + bundle.Manifest.ProductVersion
	if !api.authorizePublication(w, request, principal, coordinate) {
		return
	}
	if !reservePublicationSpace(w, request, principal, api.configuration, bundle.Manifest.ProductGroup+":"+bundle.Manifest.ProductName) {
		return
	}
	assets, err := api.storeHelmPublicationAssets(request.Context(), bundle)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	publication := apolloapp.NativeHelmPublication{
		ChartName: bundle.Manifest.Extensions.HelmChart.Name, ChartVersion: bundle.Manifest.Extensions.HelmChart.Version,
		ChartRepository: bundle.Manifest.Extensions.HelmChart.RepositoryURL, Assets: assets,
		UpgradeStrategy: bundle.Manifest.Extensions.UpgradeStrategy,
	}
	for _, artifact := range bundle.Manifest.Extensions.Artifacts {
		publication.Artifacts = append(publication.Artifacts, artifact.URI)
	}
	for _, dependency := range bundle.Manifest.Extensions.ProductDependencies {
		publication.Dependencies = append(publication.Dependencies, apolloapp.NativeDependency{
			ProductGroup: dependency.ProductGroup, ProductName: dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion,
			RecommendedVersion: dependency.RecommendedVersion, Optional: dependency.Optional,
		})
	}
	if err := api.application.PublishNativeHelmChart(request.Context(), coordinate, publication); err != nil {
		writeApolloResponseError(w, request, apolloApplicationError(err))
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (api *apolloPublicAPI) storeHelmPublicationAssets(
	ctx context.Context, bundle HelmProductReleaseBundle,
) (apolloapp.PublicationAssets, error) {
	var assets apolloapp.PublicationAssets
	store := func(contents []byte, mediaType string) (*catalog.PublicationAsset, error) {
		if len(contents) == 0 {
			return nil, nil
		}
		descriptor, _, err := api.publicationAssets.PutBlob(ctx, "", mediaType, bytes.NewReader(contents))
		if err != nil {
			return nil, err
		}
		return &catalog.PublicationAsset{MediaType: descriptor.MediaType, Digest: descriptor.Digest, Size: descriptor.Size}, nil
	}
	var err error
	if assets.DefaultConfig, err = store(bundle.DefaultConfig, "application/yaml"); err != nil {
		return apolloapp.PublicationAssets{}, err
	}
	if assets.ConfigSchema, err = store(bundle.ConfigSchema, "application/schema+json"); err != nil {
		return apolloapp.PublicationAssets{}, err
	}
	return assets, nil
}

func (api *apolloPublicAPI) authorizePublication(
	w http.ResponseWriter, request *http.Request, principal Principal, coordinate string,
) bool {
	group, name, _, valid := parseMavenCoordinate(coordinate)
	if !valid {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid Maven coordinate"})
		return false
	}
	if !requireResourceOperation(w, request, api.governance, principal, api.configuration, "global/artifact", OperationCreateArtifact) {
		return false
	}
	product := group + ":" + name
	if !api.application.HasProduct(product) &&
		!requireResourceOperation(w, request, api.governance, principal, api.configuration, "global/product", OperationCreateProduct) {
		return false
	}
	return requireResourceOperation(w, request, api.governance, principal, api.configuration, productRID(product), OperationCreateRelease)
}

func (api *apolloPublicAPI) resolveOperator(w http.ResponseWriter, request *http.Request) (Principal, bool) {
	return requireOperatorPrincipal(w, request, api.governance, api.configuration)
}

func (api *apolloPublicAPI) requireAgent(w http.ResponseWriter, request *http.Request) bool {
	identity, ok := requireAgentIdentity(w, request, api.configuration.AllowInsecureAgentIdentityHeaders)
	if !ok {
		return false
	}
	if environment := request.PathValue("environmentId"); identity.Environment != environment {
		// The published envelope for exactly this refusal. NotEnvironmentOwner
		// is described in the snapshot as "You are not the environment owner of
		// this Apollo environment and will be unable to leverage the APIs", and
		// its parameters carry the environmentId a client needs in order to say
		// which environment it was refused for.
		writeApolloConjureError(w, http.StatusForbidden,
			apollo.ErrorCodePermissionDenied, apollo.ErrorNameNotEnvironmentOwner,
			"client identity is not authorized for this Environment",
			map[string]string{"environmentId": environment})
		return false
	}
	return true
}

func (api *apolloPublicAPI) syncDesiredState(ctx context.Context, state DesiredState) {
	// A recall carried by an accepted DesiredState has come into force, and this
	// is the one funnel every accepted state passes through. The error is
	// dropped deliberately: the state is committed and the Plan is written by
	// the time this runs, so refusing here would report a failure for work that
	// happened. The adapter reports its own.
	if openings := api.configuration.RecallOpenings; openings != nil {
		_ = openings.RecallsInForce(ctx, state.Environment.ID, state.Recalls)
	}
	// Nothing about the accepted state is pushed into the Apollo service any
	// more. It used to receive a CompatibilityProjection here, and that made the
	// Environment list, the Installation list and the Namespace list artifacts
	// of which replica served the write: this funnel runs only in the process
	// that accepted the desired state, so the other replica -- and every replica
	// after a restart -- served all three EMPTY, which a client cannot tell
	// apart from a Hub that governs nothing. releaseLifecycleProjectionPort and
	// environmentInventoryProjectionPort read the same facts from the Plans, which
	// both replicas share.
	//
	// This function is kept for the recall openings above; it is deliberately no
	// longer a projection push, and adding one back would reintroduce the defect.
}

func parseMavenCoordinate(coordinate string) (string, string, string, bool) {
	parts := strings.Split(coordinate, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || !catalog.VersionPattern.MatchString(parts[2]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
