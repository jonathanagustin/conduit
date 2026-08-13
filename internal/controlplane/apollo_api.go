package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/apollo"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// apolloPublicAPI is an adapter for the public Apollo API v0 contract. The
// control plane remains vendor-neutral internally; only this boundary uses
// Apollo field names, RIDs, Maven coordinates, and response envelopes.
type apolloPublicAPI struct {
	mu                  sync.RWMutex
	products            map[string]apolloProduct
	releases            map[string]map[string]apolloRelease
	environments        map[string]apolloEnvironment
	installations       map[string]map[string]apolloInstallation
	namespaces          map[string]apolloNamespace
	repository          Repository
	entityInfo          map[string][]apolloEntityInfo
	entityHealth        map[string][]apolloEntityHealth
	governance          *GovernanceService
	configuration       HandlerOptions
	initializationError error
}

type apolloProduct struct {
	ID    string `json:"id"`
	Group string `json:"group"`
}

type apolloRelease struct {
	MavenCoordinate     string                    `json:"mavenCoordinate"`
	ReleaseChannels     []string                  `json:"releaseChannels,omitempty"`
	UpgradeStrategy     string                    `json:"upgradeStrategy,omitempty"`
	RecallStatus        apolloRecallStatus        `json:"recallStatus"`
	ProductDependencies []apolloProductDependency `json:"productDependencies,omitempty"`
	publicationDigest   string
}

type apolloRecallStatus struct {
	Recalled bool `json:"recalled"`
}

type apolloProductDependency struct {
	ProductID  string `json:"productId"`
	MinVersion string `json:"minVersion,omitempty"`
	MaxVersion string `json:"maxVersion,omitempty"`
	Optional   bool   `json:"optional,omitempty"`
}

type apolloDependencyLocator struct {
	ProductGroup       string `json:"productGroup"`
	ProductName        string `json:"productName"`
	MinimumVersion     string `json:"minimumVersion"`
	MaximumVersion     string `json:"maximumVersion"`
	RecommendedVersion string `json:"recommendedVersion,omitempty"`
	Optional           bool   `json:"optional,omitempty"`
}

type apolloHelmChartLocator struct {
	HelmChartName     string `json:"helmChartName"`
	HelmChartVersion  string `json:"helmChartVersion"`
	HelmRepositoryURL string `json:"helmRepositoryUrl"`
}

type apolloArtifactLocator struct {
	ArtifactURL string `json:"artifactUrl"`
}

type apolloEnvironment struct {
	RID             string `json:"rid"`
	Name            string `json:"name"`
	EnvironmentType string `json:"environmentType,omitempty"`
	Status          string `json:"status"`
}

type apolloInstallation struct {
	RID                 string   `json:"rid"`
	EnvironmentRID      string   `json:"environmentRid"`
	ApolloEntityID      string   `json:"apolloEntityId"`
	PackageRID          string   `json:"packageRid"`
	ProductID           string   `json:"productId"`
	ReleaseChannel      string   `json:"releaseChannel,omitempty"`
	IgnoredDependencies []string `json:"ignoredDependencies,omitempty"`
	Version             string   `json:"version,omitempty"`
}

type apolloNamespace struct {
	ID      string `json:"id"`
	Owners  []any  `json:"owners"`
	Editors []any  `json:"editors"`
	Viewers []any  `json:"viewers"`
}

type apolloNodeInfo struct {
	CreationTimestamp time.Time `json:"creationTimestamp"`
	HostID            string    `json:"hostId"`
	NodeID            string    `json:"nodeId"`
	ProductVersion    string    `json:"productVersion"`
}

type apolloEntityInfo struct {
	EntityType   string           `json:"entityType"`
	ID           string           `json:"id"`
	ProductGroup string           `json:"productGroup"`
	ProductName  string           `json:"productName"`
	K8sNamespace string           `json:"k8sNamespace,omitempty"`
	NodesInfo    []apolloNodeInfo `json:"nodesInfo,omitempty"`
	Quorum       string           `json:"quorum,omitempty"`
}

type HealthCheck struct {
	ID      string         `json:"id"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params"`
	Result  string         `json:"result"`
}

type apolloContainerStatus struct {
	ContainerID string        `json:"containerId"`
	Checks      []HealthCheck `json:"checks,omitempty"`
	Liveness    bool          `json:"liveness"`
	Readiness   bool          `json:"readiness"`
}

type apolloEntityHealth struct {
	ProductGroup      string                  `json:"productGroup"`
	ProductName       string                  `json:"productName"`
	ProductVersion    string                  `json:"productVersion"`
	NodeID            string                  `json:"nodeId"`
	ContainerStatuses []apolloContainerStatus `json:"containerStatuses,omitempty"`
}

type EntityHealthDetails struct {
	Quorum string             `json:"quorum"`
	Nodes  []EntityNodeHealth `json:"nodes"`
}

type EntityNodeHealth struct {
	NodeID         string                  `json:"nodeId"`
	ProductVersion string                  `json:"productVersion"`
	Result         string                  `json:"result"`
	Containers     []EntityContainerHealth `json:"containers"`
}

type EntityContainerHealth struct {
	ContainerID string        `json:"containerId"`
	Liveness    bool          `json:"liveness"`
	Readiness   bool          `json:"readiness"`
	Result      string        `json:"result"`
	Checks      []HealthCheck `json:"checks,omitempty"`
}

func newApolloPublicAPI(repository Repository, governance *GovernanceService, configuration HandlerOptions) *apolloPublicAPI {
	api := &apolloPublicAPI{
		products: make(map[string]apolloProduct), releases: make(map[string]map[string]apolloRelease),
		environments: make(map[string]apolloEnvironment), installations: make(map[string]map[string]apolloInstallation),
		namespaces: make(map[string]apolloNamespace),
		repository: repository, entityInfo: make(map[string][]apolloEntityInfo), entityHealth: make(map[string][]apolloEntityHealth),
		governance: governance, configuration: configuration,
	}
	publications, err := repository.ListProductPublications(context.Background(), "")
	if err != nil {
		api.initializationError = err
		return api
	}
	for _, publication := range publications {
		api.restorePublication(publication)
	}
	return api
}

// operationHandlers binds each pinned public operation to the handler that
// answers it. The patterns are not spelled here: apollo.Routes is generated from
// the pinned OpenAPI snapshot, so a path this adapter serves cannot diverge
// from one the published contract declares.
func (api *apolloPublicAPI) operationHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"publishHelmChart":        api.publishHelmChart,
		"publishService":          api.publishService,
		"listProducts":            api.listProducts,
		"getProduct":              api.getProduct,
		"listReleases":            api.listReleases,
		"getRelease":              api.getRelease,
		"listEnvironments":        api.listEnvironments,
		"getEnvironment":          api.getEnvironment,
		"listInstallations":       api.listInstallations,
		"getInstallation":         api.getInstallation,
		"putStatusEntitiesInfo":   api.putEntityInfo,
		"putStatusEntitiesHealth": api.putEntityHealth,
		"listNamespaces":          api.listNamespaces,
	}
}

func (api *apolloPublicAPI) register(mux *http.ServeMux) {
	handlers := api.operationHandlers()
	for _, route := range apollo.Routes {
		handler, bound := handlers[route.Operation]
		if !bound {
			// The snapshot declares an operation this adapter cannot answer.
			// Serving the other twelve and quietly 404ing the thirteenth would
			// misreport the contract as met, so refuse to start.
			panic("Apollo public operation " + route.Operation + " has no handler")
		}
		mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, request *http.Request) {
			if api.initializationError != nil {
				writeLifecycleError(w, api.initializationError)
				return
			}
			handler(w, request)
		})
	}
}

func (api *apolloPublicAPI) publishHelmChart(w http.ResponseWriter, request *http.Request) {
	principal, ok := api.resolveOperator(w, request)
	if !ok {
		return
	}
	coordinate := request.PathValue("mavenCoordinate")
	if !api.authorizePublication(w, request, principal, coordinate) {
		return
	}
	body, ok := decodeJSON[struct {
		Extensions struct {
			HelmChart           apolloHelmChartLocator    `json:"helmChart"`
			Artifacts           []apolloArtifactLocator   `json:"artifacts,omitempty"`
			ProductDependencies []apolloDependencyLocator `json:"productDependencies,omitempty"`
		} `json:"extensions"`
	}](w, request)
	if !ok {
		return
	}
	if body.Extensions.HelmChart.HelmChartName == "" ||
		!versionPattern.MatchString(body.Extensions.HelmChart.HelmChartVersion) ||
		!validApolloRepositoryURL(body.Extensions.HelmChart.HelmRepositoryURL) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid Helm chart extension"})
		return
	}
	for _, artifact := range body.Extensions.Artifacts {
		if strings.TrimSpace(artifact.ArtifactURL) == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "artifactUrl is required"})
			return
		}
	}
	api.publishPayload(request.Context(), w, coordinate, body.Extensions.ProductDependencies, "helm-chart", body)
}

// publishHelmBundle is the Platform-native semantic publication route. Keeping
// it outside register() prevents platformBundle from becoming an undocumented
// extension of the pinned public Apollo API operation.
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
	assets, err := api.storeHelmPublicationAssets(request.Context(), bundle)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	payload := struct {
		Extensions struct {
			HelmChart           apolloHelmChartLocator    `json:"helmChart"`
			Artifacts           []apolloArtifactLocator   `json:"artifacts,omitempty"`
			ProductDependencies []apolloDependencyLocator `json:"productDependencies,omitempty"`
			PlatformBundle      helmPublicationAssets     `json:"platformBundle,omitempty"`
		} `json:"extensions"`
	}{}
	payload.Extensions.HelmChart = apolloHelmChartLocator{
		HelmChartName: bundle.Manifest.Extensions.HelmChart.Name, HelmChartVersion: bundle.Manifest.Extensions.HelmChart.Version,
		HelmRepositoryURL: bundle.Manifest.Extensions.HelmChart.RepositoryURL,
	}
	for _, artifact := range bundle.Manifest.Extensions.Artifacts {
		payload.Extensions.Artifacts = append(payload.Extensions.Artifacts, apolloArtifactLocator{ArtifactURL: artifact.URI})
	}
	for _, dependency := range bundle.Manifest.Extensions.ProductDependencies {
		payload.Extensions.ProductDependencies = append(payload.Extensions.ProductDependencies, apolloDependencyLocator(dependency))
	}
	payload.Extensions.PlatformBundle = assets
	api.publishPayload(request.Context(), w, coordinate, payload.Extensions.ProductDependencies, "helm-chart", payload)
}

type helmPublicationAssets struct {
	DefaultConfig *ProductPublicationAsset `json:"defaultConfig,omitempty"`
	ConfigSchema  *ProductPublicationAsset `json:"configSchema,omitempty"`
}

func (api *apolloPublicAPI) storeHelmPublicationAssets(ctx context.Context, bundle HelmProductReleaseBundle) (helmPublicationAssets, error) {
	var assets helmPublicationAssets
	store := func(contents []byte, mediaType string) (*ProductPublicationAsset, error) {
		if len(contents) == 0 {
			return nil, nil
		}
		descriptor, _, err := api.repository.PutBlob(ctx, "", mediaType, bytes.NewReader(contents))
		if err != nil {
			return nil, err
		}
		return &ProductPublicationAsset{MediaType: descriptor.MediaType, Digest: descriptor.Digest, Size: descriptor.Size}, nil
	}
	var err error
	if assets.DefaultConfig, err = store(bundle.DefaultConfig, "application/yaml"); err != nil {
		return helmPublicationAssets{}, err
	}
	if assets.ConfigSchema, err = store(bundle.ConfigSchema, "application/schema+json"); err != nil {
		return helmPublicationAssets{}, err
	}
	return assets, nil
}

func (api *apolloPublicAPI) publishService(w http.ResponseWriter, request *http.Request) {
	principal, ok := api.resolveOperator(w, request)
	if !ok {
		return
	}
	coordinate := request.PathValue("mavenCoordinate")
	if !api.authorizePublication(w, request, principal, coordinate) {
		return
	}
	body, ok := decodeJSON[struct {
		Extensions struct {
			ProductDependencies []apolloDependencyLocator `json:"productDependencies,omitempty"`
		} `json:"extensions,omitempty"`
	}](w, request)
	if !ok {
		return
	}
	api.publishPayload(request.Context(), w, coordinate, body.Extensions.ProductDependencies, "service", body)
}

// authorizePublication models the Hub side of a connected publisher. Network
// admission, OAuth client-secret custody, Marketplace-store whitelisting, and
// source-side folder eligibility remain at the external publisher/front door.
// The Hub admits the authenticated service principal only when it independently
// has Artifact Creator plus Product Creator and Release Creator authority.
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
	api.mu.RLock()
	_, exists := api.products[product]
	api.mu.RUnlock()
	if !exists && !requireResourceOperation(w, request, api.governance, principal, api.configuration, "global/product", OperationCreateProduct) {
		return false
	}
	return requireResourceOperation(w, request, api.governance, principal, api.configuration, productRID(product), OperationCreateRelease)
}

func (api *apolloPublicAPI) publish(ctx context.Context, w http.ResponseWriter, coordinate string, dependencies []apolloDependencyLocator) {
	api.publishPayload(ctx, w, coordinate, dependencies, "service", struct {
		Dependencies []apolloDependencyLocator `json:"dependencies,omitempty"`
	}{Dependencies: dependencies})
}

func (api *apolloPublicAPI) publishPayload(
	ctx context.Context,
	w http.ResponseWriter,
	coordinate string,
	dependencies []apolloDependencyLocator,
	kind string,
	payload any,
) {
	group, name, version, ok := parseMavenCoordinate(coordinate)
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid Maven coordinate"})
		return
	}
	converted := make([]apolloProductDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.ProductGroup == "" || dependency.ProductName == "" ||
			!versionPattern.MatchString(dependency.MinimumVersion) ||
			!versionMatcherPattern.MatchString(dependency.MaximumVersion) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid Product dependency"})
			return
		}
		converted = append(converted, apolloProductDependency{
			ProductID:  dependency.ProductGroup + ":" + dependency.ProductName,
			MinVersion: dependency.MinimumVersion, MaxVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	encodedPublication, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		Payload any    `json:"payload"`
	}{Kind: kind, Payload: payload})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid publication payload"})
		return
	}
	publicationSum := sha256.Sum256(encodedPublication)
	publicationDigest := "sha256:" + hex.EncodeToString(publicationSum[:])
	productID := group + ":" + name
	if _, err := api.repository.GetCatalogRetirement(ctx, productID, ""); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Product coordinate is retired"})
		return
	} else if !errors.Is(err, ErrNotFound) {
		writeLifecycleError(w, err)
		return
	}
	if _, err := api.repository.GetCatalogRetirement(ctx, productID, version); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Product Release coordinate is retired"})
		return
	} else if !errors.Is(err, ErrNotFound) {
		writeLifecycleError(w, err)
		return
	}
	persistedDependencies := make([]Dependency, 0, len(converted))
	for _, dependency := range converted {
		persistedDependencies = append(persistedDependencies, Dependency{
			Product: dependency.ProductID, MinimumVersion: dependency.MinVersion,
			MaximumVersion: dependency.MaxVersion, Optional: dependency.Optional,
		})
	}
	publication := ProductPublication{
		MavenCoordinate: coordinate, Product: productID, Version: version, Kind: kind,
		PublicationDigest: publicationDigest, ReleaseChannels: defaultChannelsForVersion(version),
		UpgradeStrategy: "shutdown", Dependencies: persistedDependencies, CreatedAt: time.Now().UTC(),
	}
	var publicationAssets struct {
		Extensions struct {
			PlatformBundle helmPublicationAssets `json:"platformBundle"`
		} `json:"extensions"`
	}
	encodedPayload, _ := json.Marshal(payload)
	_ = json.Unmarshal(encodedPayload, &publicationAssets)
	publication.DefaultConfig = publicationAssets.Extensions.PlatformBundle.DefaultConfig
	publication.ConfigSchema = publicationAssets.Extensions.PlatformBundle.ConfigSchema
	if release, exact := executableReleaseFromPublication(coordinate, kind, payload); exact {
		publication.Release = &release
	}
	stored, err := api.repository.CreateProductPublication(ctx, publication)
	if errors.Is(err, ErrConflict) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Product Release coordinate is immutable"})
		return
	}
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	api.restorePublication(stored)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (api *apolloPublicAPI) restorePublication(publication ProductPublication) {
	group, _, _, ok := parseMavenCoordinate(publication.MavenCoordinate)
	if !ok {
		return
	}
	dependencies := make([]apolloProductDependency, 0, len(publication.Dependencies))
	for _, dependency := range publication.Dependencies {
		dependencies = append(dependencies, apolloProductDependency{
			ProductID: dependency.Product, MinVersion: dependency.MinimumVersion,
			MaxVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	api.products[publication.Product] = apolloProduct{ID: publication.Product, Group: group}
	if api.releases[publication.Product] == nil {
		api.releases[publication.Product] = map[string]apolloRelease{}
	}
	api.releases[publication.Product][publication.Version] = apolloRelease{
		MavenCoordinate: publication.MavenCoordinate, ReleaseChannels: publication.ReleaseChannels,
		UpgradeStrategy: publication.UpgradeStrategy, RecallStatus: apolloRecallStatus{},
		ProductDependencies: dependencies, publicationDigest: publication.PublicationDigest,
	}
}

func executableReleaseFromPublication(coordinate, kind string, payload any) (Release, bool) {
	if kind != "helm-chart" {
		return Release{}, false
	}
	group, name, version, ok := parseMavenCoordinate(coordinate)
	if !ok {
		return Release{}, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Release{}, false
	}
	var body struct {
		Extensions struct {
			HelmChart           apolloHelmChartLocator    `json:"helmChart"`
			Artifacts           []apolloArtifactLocator   `json:"artifacts"`
			ProductDependencies []apolloDependencyLocator `json:"productDependencies"`
		} `json:"extensions"`
	}
	if json.Unmarshal(encoded, &body) != nil {
		return Release{}, false
	}
	manifest := ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: group, ProductName: name, ProductVersion: version, ProductType: "helm-chart.v1",
		Extensions: ApolloProductManifestExtensions{HelmChart: &ApolloManifestHelmChart{
			Name: body.Extensions.HelmChart.HelmChartName, Version: body.Extensions.HelmChart.HelmChartVersion,
			RepositoryURL: body.Extensions.HelmChart.HelmRepositoryURL,
		}},
	}
	for _, artifact := range body.Extensions.Artifacts {
		manifest.Extensions.Artifacts = append(manifest.Extensions.Artifacts, ApolloManifestArtifact{Type: "oci", URI: artifact.ArtifactURL})
	}
	for _, dependency := range body.Extensions.ProductDependencies {
		// A conversion rather than a field-by-field literal: the two shapes are
		// the same dependency in wire and manifest form, and this stops
		// compiling the day they diverge instead of silently dropping whichever
		// field was added to only one of them.
		manifest.Extensions.ProductDependencies = append(
			manifest.Extensions.ProductDependencies, ApolloManifestDependency(dependency),
		)
	}
	release, err := ExecutableReleaseFromApolloManifest(manifest)
	return release, err == nil
}

func (api *apolloPublicAPI) listProducts(w http.ResponseWriter, request *http.Request) {
	if !api.requireProductReader(w, request, "global/product") {
		return
	}
	api.mu.RLock()
	products := make([]apolloProduct, 0, len(api.products))
	for _, product := range api.products {
		products = append(products, product)
	}
	api.mu.RUnlock()
	sort.Slice(products, func(i, j int) bool { return products[i].ID < products[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"products": products})
}

func (api *apolloPublicAPI) getProduct(w http.ResponseWriter, request *http.Request) {
	if !api.requireProductReader(w, request, productRID(request.PathValue("productId"))) {
		return
	}
	api.mu.RLock()
	product, exists := api.products[request.PathValue("productId")]
	api.mu.RUnlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Product not found"})
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (api *apolloPublicAPI) listReleases(w http.ResponseWriter, request *http.Request) {
	if !api.requireProductReader(w, request, productRID(request.PathValue("productId"))) {
		return
	}
	api.mu.RLock()
	stored := api.releases[request.PathValue("productId")]
	releases := make([]apolloRelease, 0, len(stored))
	for _, release := range stored {
		releases = append(releases, release)
	}
	api.mu.RUnlock()
	sort.Slice(releases, func(i, j int) bool {
		_, _, left, _ := parseMavenCoordinate(releases[i].MavenCoordinate)
		_, _, right, _ := parseMavenCoordinate(releases[j].MavenCoordinate)
		return compareVersions(left, right) < 0
	})
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func (api *apolloPublicAPI) getRelease(w http.ResponseWriter, request *http.Request) {
	if !api.requireProductReader(w, request, productRID(request.PathValue("productId"))) {
		return
	}
	api.mu.RLock()
	release, exists := api.releases[request.PathValue("productId")][request.PathValue("productVersion")]
	api.mu.RUnlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Release not found"})
		return
	}
	writeJSON(w, http.StatusOK, release)
}

func (api *apolloPublicAPI) syncDesiredState(state DesiredState) {
	rid := apolloEnvironmentRID(state.Environment.ID)
	api.mu.Lock()
	defer api.mu.Unlock()
	api.environments[rid] = apolloEnvironment{RID: rid, Name: state.Environment.ID, EnvironmentType: "production", Status: "managed"}
	installations := make(map[string]apolloInstallation, len(state.Entities))
	for _, entity := range state.Entities {
		installationRID := "ri.apollo.main.installation." + stableApolloSuffix(state.Environment.ID+":"+entity.Name)
		// A malformed or over-long identifier is left empty rather than emitted:
		// this projection is read by external callers, and a truncated aeid still
		// parses as one while naming the wrong Entity.
		entityID, _ := ApolloHelmChartEntityID(state.Environment.ID, entity.Name)
		installation := apolloInstallation{
			RID: installationRID, EnvironmentRID: rid,
			ApolloEntityID: entityID,
			PackageRID:     "ri.apollo.main.package." + stableApolloSuffix(entity.Product), ProductID: entity.Product,
			ReleaseChannel: entity.ReleaseChannel, Version: entity.Version,
		}
		installations[installationRID] = installation
	}
	api.installations[rid] = installations
	for _, namespace := range state.Environment.Namespaces {
		id := namespace.Name
		if id == "" {
			id = namespace.RID
		}
		api.namespaces[id] = apolloNamespace{ID: id, Owners: []any{}, Editors: []any{}, Viewers: []any{}}
	}
}

func (api *apolloPublicAPI) listEnvironments(w http.ResponseWriter, request *http.Request) {
	if !api.requireOperator(w, request, OperationReadResource) {
		return
	}
	api.mu.RLock()
	environments := make([]apolloEnvironment, 0, len(api.environments))
	for _, environment := range api.environments {
		environments = append(environments, environment)
	}
	api.mu.RUnlock()
	sort.Slice(environments, func(i, j int) bool { return environments[i].RID < environments[j].RID })
	writeJSON(w, http.StatusOK, map[string]any{"environments": environments})
}

func (api *apolloPublicAPI) getEnvironment(w http.ResponseWriter, request *http.Request) {
	if !api.requireOperator(w, request, OperationReadResource) {
		return
	}
	api.mu.RLock()
	environment, exists := api.environments[request.PathValue("environmentRid")]
	api.mu.RUnlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Environment not found"})
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (api *apolloPublicAPI) listInstallations(w http.ResponseWriter, request *http.Request) {
	if !api.requireOperator(w, request, OperationReadResource) {
		return
	}
	api.mu.RLock()
	stored := api.installations[request.PathValue("environmentRid")]
	installations := make([]apolloInstallation, 0, len(stored))
	for _, installation := range stored {
		installations = append(installations, installation)
	}
	api.mu.RUnlock()
	sort.Slice(installations, func(i, j int) bool { return installations[i].RID < installations[j].RID })
	writeJSON(w, http.StatusOK, map[string]any{"installations": installations})
}

func (api *apolloPublicAPI) getInstallation(w http.ResponseWriter, request *http.Request) {
	if !api.requireOperator(w, request, OperationReadResource) {
		return
	}
	api.mu.RLock()
	installation, exists := api.installations[request.PathValue("environmentRid")][request.PathValue("installationRid")]
	api.mu.RUnlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Installation not found"})
		return
	}
	writeJSON(w, http.StatusOK, installation)
}

func (api *apolloPublicAPI) putEntityInfo(w http.ResponseWriter, request *http.Request) {
	if !api.requireAgent(w, request) {
		return
	}
	body, ok := decodeJSONWithLimit[struct {
		EntitiesInfo []apolloEntityInfo `json:"entitiesInfo,omitempty"`
	}](w, request, 1<<20)
	if !ok {
		return
	}
	if err := validateApolloEntityInfo(body.EntitiesInfo); err != nil {
		writeLifecycleError(w, err)
		return
	}
	environment := request.PathValue("environmentId")
	api.mu.Lock()
	api.entityInfo[environment] = cloneJSON(body.EntitiesInfo)
	api.mu.Unlock()
	if err := api.persistReportedState(request.Context(), environment); err != nil {
		writeLifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (api *apolloPublicAPI) putEntityHealth(w http.ResponseWriter, request *http.Request) {
	if !api.requireAgent(w, request) {
		return
	}
	// The 10,000-byte contract bound is per Entity, not per request: one array
	// element exists for every (Product, node), so an Environment reports one
	// element per replica. Bounding the whole request at 10,000 capped the
	// Environment near fifteen nodes and silently 413'd every fleet past it.
	// The request bound matches entities/info, which describes the same fleet
	// in the same reporting cycle; validateApolloEntityHealth carries the
	// per-Entity bound the contract actually specifies.
	body, ok := decodeJSONWithLimit[struct {
		EntitiesHealth []apolloEntityHealth `json:"entitiesHealth,omitempty"`
	}](w, request, 1<<20)
	if !ok {
		return
	}
	if err := validateApolloEntityHealth(body.EntitiesHealth); err != nil {
		writeLifecycleError(w, err)
		return
	}
	environment := request.PathValue("environmentId")
	api.mu.Lock()
	api.entityHealth[environment] = cloneJSON(body.EntitiesHealth)
	api.mu.Unlock()
	if err := api.persistReportedState(request.Context(), environment); err != nil {
		writeLifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (api *apolloPublicAPI) requireOperator(w http.ResponseWriter, request *http.Request, operation string) bool {
	principal, ok := api.resolveOperator(w, request)
	return ok && requirePlatformOperation(w, request, api.governance, principal, api.configuration, operation)
}

func (api *apolloPublicAPI) resolveOperator(w http.ResponseWriter, request *http.Request) (Principal, bool) {
	return requireOperatorPrincipal(w, request, api.governance, api.configuration)
}

func (api *apolloPublicAPI) requireProductReader(w http.ResponseWriter, request *http.Request, resource string) bool {
	principal, ok := api.resolveOperator(w, request)
	return ok && requireResourceOperation(w, request, api.governance, principal, api.configuration, resource, OperationReadResource)
}

func (api *apolloPublicAPI) requireAgent(w http.ResponseWriter, request *http.Request) bool {
	identity, ok := requireAgentIdentity(w, request, api.configuration.AllowInsecureAgentIdentityHeaders)
	if !ok {
		return false
	}
	if identity.Environment != request.PathValue("environmentId") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "client identity is not authorized for this Environment"})
		return false
	}
	return true
}

func validateApolloEntityInfo(entities []apolloEntityInfo) error {
	seen := map[string]struct{}{}
	for index, entity := range entities {
		if (entity.EntityType != "HELM_CHART" && entity.EntityType != "SERVICE") || strings.TrimSpace(entity.ID) == "" ||
			strings.TrimSpace(entity.ProductGroup) == "" || strings.TrimSpace(entity.ProductName) == "" {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesInfo[%d]", index), Message: "entityType, id, productGroup, and productName are required"}}
		}
		if _, exists := seen[entity.ID]; exists {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesInfo[%d].id", index), Message: "must be unique"}}
		}
		seen[entity.ID] = struct{}{}
		if entity.Quorum != "" && !contains([]string{"UNKNOWN", "ANY", "ALL", "ALL_BUT_ONE", "SIMPLE_MAJORITY", "NO_GUARANTEE"}, entity.Quorum) {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesInfo[%d].quorum", index), Message: "is invalid"}}
		}
		for nodeIndex, node := range entity.NodesInfo {
			if node.CreationTimestamp.IsZero() || node.HostID == "" || node.NodeID == "" || !versionPattern.MatchString(node.ProductVersion) {
				return ValidationErrors{{Path: fmt.Sprintf("entitiesInfo[%d].nodesInfo[%d]", index, nodeIndex), Message: "complete valid node metadata is required"}}
			}
		}
	}
	return nil
}

func validateApolloEntityHealth(entities []apolloEntityHealth) error {
	seen := map[string]struct{}{}
	for index, entity := range entities {
		if entity.ProductGroup == "" || entity.ProductName == "" || !versionPattern.MatchString(entity.ProductVersion) || entity.NodeID == "" {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d]", index), Message: "product, version, and nodeId are required"}}
		}
		key := entity.ProductGroup + ":" + entity.ProductName + "\x00" + entity.NodeID
		if _, exists := seen[key]; exists {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d]", index), Message: "duplicates a Product node"}}
		}
		seen[key] = struct{}{}
		containers := map[string]struct{}{}
		for containerIndex, container := range entity.ContainerStatuses {
			if container.ContainerID == "" {
				return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].containerId", index, containerIndex), Message: "must not be empty"}}
			}
			if _, exists := containers[container.ContainerID]; exists {
				return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].containerId", index, containerIndex), Message: "must be unique per node"}}
			}
			containers[container.ContainerID] = struct{}{}
			for checkIndex, check := range container.Checks {
				if check.ID == "" || check.Params == nil || !contains([]string{HealthHealthy, HealthRepairing, HealthWarning, HealthError, HealthTerminal}, check.Result) {
					return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].checks[%d]", index, containerIndex, checkIndex), Message: "id, params, and valid result are required"}}
				}
			}
		}
		// Mirrors the native path's per-observation bound in applyHealthObservations,
		// which this adapter does not route through. Bounding each Entity keeps one
		// oversized report from growing without limit while leaving fleet size to
		// the request bound.
		if encoded, err := json.Marshal(entity); err == nil && len(encoded) > 10_000 {
			return ValidationErrors{{Path: fmt.Sprintf("entitiesHealth[%d]", index), Message: "must not exceed 10,000 bytes"}}
		}
	}
	return nil
}

func (api *apolloPublicAPI) persistReportedState(ctx context.Context, environment string) error {
	api.mu.RLock()
	info := cloneJSON(api.entityInfo[environment])
	health := cloneJSON(api.entityHealth[environment])
	api.mu.RUnlock()
	plans, err := api.repository.ListPlans(ctx, environment)
	if err != nil || len(plans) == 0 {
		return err
	}
	latest := latestPlanRecords(plans)[environment]
	existing := map[string]ReportedEntity{}
	if report, reportErr := api.repository.GetEnvironmentReport(ctx, environment); reportErr == nil {
		for _, entity := range report.Entities {
			existing[entity.InstallationID] = entity
		}
	}
	now := time.Now().UTC()
	reported := make([]ReportedEntity, 0, len(latest.Plan.Steps))
	for _, step := range latest.Plan.Steps {
		entity := existing[step.InstallationID]
		if entity.InstallationID == "" {
			entity = ReportedEntity{InstallationID: step.InstallationID, Product: step.Product, Version: step.Version, Namespace: step.Namespace,
				Health: HealthUnknown, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigIndeterminate, Lifecycle: LifecycleUnknown}
		}
		matchingInfo := matchApolloInfo(info, step)
		matchingHealth := matchApolloHealth(health, matchingInfo, step.Product)
		if len(matchingInfo.NodesInfo) > 0 || len(matchingHealth) > 0 {
			details, result := aggregateApolloHealth(matchingInfo.Quorum, matchingHealth)
			entity.Health, entity.HealthDetails, entity.ObservedAt = result, &details, now
			if matchingInfo.K8sNamespace != "" {
				entity.Namespace = matchingInfo.K8sNamespace
			}
			if len(matchingHealth) > 0 {
				entity.Version = matchingHealth[0].ProductVersion
			}
			if len(matchingInfo.NodesInfo) > 0 {
				entity.Lifecycle = LifecycleInstalled
			}
		}
		reported = append(reported, entity)
	}
	_, err = api.repository.PutEnvironmentReport(ctx, EnvironmentReport{Environment: environment, AgentID: "apollo-public-api", ObservedAt: now, Entities: reported})
	return err
}

func matchApolloInfo(info []apolloEntityInfo, step PlanStep) apolloEntityInfo {
	for _, candidate := range info {
		if candidate.ID == step.Entity && candidate.ProductGroup+":"+candidate.ProductName == step.Product {
			return candidate
		}
	}
	return apolloEntityInfo{ProductGroup: strings.SplitN(step.Product, ":", 2)[0], ProductName: strings.SplitN(step.Product, ":", 2)[1], Quorum: "UNKNOWN"}
}

func matchApolloHealth(health []apolloEntityHealth, info apolloEntityInfo, product string) []apolloEntityHealth {
	nodes := map[string]struct{}{}
	for _, node := range info.NodesInfo {
		nodes[node.NodeID] = struct{}{}
	}
	result := []apolloEntityHealth{}
	for _, candidate := range health {
		if candidate.ProductGroup+":"+candidate.ProductName == product && (len(nodes) == 0 || containsKey(nodes, candidate.NodeID)) {
			result = append(result, candidate)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeID < result[j].NodeID })
	return result
}

func aggregateApolloHealth(quorum string, health []apolloEntityHealth) (EntityHealthDetails, string) {
	if quorum == "" {
		quorum = "UNKNOWN"
	}
	details := EntityHealthDetails{Quorum: quorum}
	healthy := 0
	worst := HealthHealthy
	for _, node := range health {
		nodeResult := HealthHealthy
		containers := make([]EntityContainerHealth, 0, len(node.ContainerStatuses))
		for _, container := range node.ContainerStatuses {
			result := HealthHealthy
			if !container.Liveness || !container.Readiness {
				result = HealthError
			}
			for _, check := range container.Checks {
				result = worseHealth(result, check.Result)
			}
			nodeResult = worseHealth(nodeResult, result)
			containers = append(containers, EntityContainerHealth{ContainerID: container.ContainerID, Liveness: container.Liveness, Readiness: container.Readiness, Result: result, Checks: cloneJSON(container.Checks)})
		}
		if nodeResult == HealthHealthy {
			healthy++
		}
		worst = worseHealth(worst, nodeResult)
		details.Nodes = append(details.Nodes, EntityNodeHealth{NodeID: node.NodeID, ProductVersion: node.ProductVersion, Result: nodeResult, Containers: containers})
	}
	if len(health) == 0 {
		return details, HealthUnknown
	}
	required := len(health)
	switch quorum {
	case "ANY", "NO_GUARANTEE":
		required = 1
	case "ALL_BUT_ONE":
		required = len(health) - 1
		if required < 1 {
			required = 1
		}
	case "SIMPLE_MAJORITY":
		required = len(health)/2 + 1
	case "UNKNOWN":
		return details, HealthIndeterminate
	}
	if healthy >= required {
		return details, HealthHealthy
	}
	return details, worst
}

func worseHealth(left, right string) string {
	severity := map[string]int{HealthHealthy: 0, HealthRepairing: 1, HealthWarning: 2, HealthError: 3, HealthTerminal: 4}
	if severity[right] > severity[left] {
		return right
	}
	return left
}

func containsKey(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}

func (api *apolloPublicAPI) listNamespaces(w http.ResponseWriter, request *http.Request) {
	if !api.requireOperator(w, request, OperationReadResource) {
		return
	}
	api.mu.RLock()
	namespaces := make([]apolloNamespace, 0, len(api.namespaces))
	for _, namespace := range api.namespaces {
		namespaces = append(namespaces, namespace)
	}
	api.mu.RUnlock()
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].ID < namespaces[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
}

func parseMavenCoordinate(coordinate string) (string, string, string, bool) {
	parts := strings.Split(coordinate, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || !versionPattern.MatchString(parts[2]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func validApolloRepositoryURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "oci://")
}

func apolloEnvironmentRID(id string) string {
	if strings.HasPrefix(id, "ri.apollo.main.environment.") {
		return id
	}
	return "ri.apollo.main.environment." + stableApolloSuffix(id)
}

func stableApolloSuffix(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}
