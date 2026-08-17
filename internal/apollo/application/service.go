// Package application owns the Apollo compatibility use cases. Generated
// transport models enter here, are validated and translated once, and never
// leak into Conduit's Catalog or Environments domains.
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	environmentports "github.com/jonathanagustin/conduit/internal/environments/ports"
)

type ErrorKind string

const (
	Invalid  ErrorKind = "invalid"
	NotFound ErrorKind = "not_found"
	Conflict ErrorKind = "conflict"
)

type Error struct {
	Kind    ErrorKind
	Message string
}

func (err *Error) Error() string { return err.Message }

func IsKind(err error, kind ErrorKind) bool {
	var problem *Error
	return errors.As(err, &problem) && problem.Kind == kind
}

// CatalogPort is the only Catalog authority Apollo receives. The retirement
// query deliberately returns a boolean so foreign code cannot depend on
// governance records or repository errors from the legacy control plane.
type CatalogPort interface {
	ListPublications(context.Context, string) ([]catalog.Publication, error)
	CreatePublication(context.Context, catalog.Publication) (catalog.Publication, bool, error)
	CoordinateRetired(context.Context, string, string) (bool, error)
}

// PublicationAssets are optional Conduit-native exact bytes associated with a
// Helm publication. They are never part of the public Apollo request model.
type PublicationAssets struct {
	DefaultConfig *catalog.PublicationAsset `json:"defaultConfig,omitempty"`
	ConfigSchema  *catalog.PublicationAsset `json:"configSchema,omitempty"`
}

type NativeHelmPublication struct {
	ChartName       string
	ChartVersion    string
	ChartRepository string
	Artifacts       []string
	Dependencies    []NativeDependency
	// UpgradeStrategy is the publisher's declaration from the Product manifest.
	// Empty means they declared nothing, which is the only thing a publisher on
	// the pinned Apollo REST routes can say: neither published request model
	// carries the field, so those routes pass empty and always will.
	UpgradeStrategy string
	Assets          PublicationAssets
}

type NativeDependency struct {
	ProductGroup       string
	ProductName        string
	MinimumVersion     string
	MaximumVersion     string
	RecommendedVersion string
	Optional           bool
}

type releaseRecord struct {
	model  apollo.Release
	digest string
}

// Service holds compatibility projections, not authoritative desired state.
// Durable Publications and Environment Reports cross explicit domain ports.
type Service struct {
	mu            sync.RWMutex
	products      map[string]apollo.Product
	releases      map[string]map[string]releaseRecord
	environments  map[string]apollo.Environment
	installations map[string]map[string]apollo.Installation
	namespaces    map[string]apollo.Namespace
	entityInfo    map[string][]apollo.EntityInfo
	entityHealth  map[string][]apollo.EntityHealth
	// recalls is keyed by Environment, because that is how Conduit declares a
	// withdrawal. Apollo's recallStatus is a property of the Release rather
	// than of an Environment, so the read below aggregates.
	recalls map[string][]environments.ProjectedRecall
	// promotions is keyed by Environment for the same reason, and aggregated on
	// read for the same reason: a Channel a Release has reached is a property of
	// the Release, and Conduit records the crossing per Environment.
	promotions map[string][]environments.ProjectedPromotion
	catalog    CatalogPort
	reporting  environmentports.ReportingPort
	now        func() time.Time
}

func New(ctx context.Context, catalogPort CatalogPort, reporting environmentports.ReportingPort) (*Service, error) {
	service := &Service{
		products: make(map[string]apollo.Product), releases: make(map[string]map[string]releaseRecord),
		environments: make(map[string]apollo.Environment), installations: make(map[string]map[string]apollo.Installation),
		namespaces: make(map[string]apollo.Namespace), entityInfo: make(map[string][]apollo.EntityInfo),
		entityHealth: make(map[string][]apollo.EntityHealth), catalog: catalogPort, reporting: reporting,
		now: time.Now,
	}
	publications, err := catalogPort.ListPublications(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("load Apollo catalog projection: %w", err)
	}
	for _, publication := range publications {
		service.restorePublication(publication)
	}
	return service, nil
}

func (service *Service) HasProduct(product string) bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	_, exists := service.products[product]
	return exists
}

func (service *Service) ListProducts() apollo.ListProductsResponse {
	service.mu.RLock()
	values := make([]apollo.Product, 0, len(service.products))
	for _, value := range service.products {
		values = append(values, clone(value))
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Id < values[j].Id })
	return apollo.ListProductsResponse{Products: &values}
}

func (service *Service) GetProduct(product string) (apollo.Product, error) {
	service.mu.RLock()
	value, exists := service.products[product]
	service.mu.RUnlock()
	if !exists {
		return apollo.Product{}, &Error{Kind: NotFound, Message: "Product not found"}
	}
	return clone(value), nil
}

func (service *Service) ListReleases(product string) apollo.ListReleasesResponse {
	service.mu.RLock()
	values := make([]apollo.Release, 0, len(service.releases[product]))
	for version, value := range service.releases[product] {
		model := clone(value.model)
		model.RecallStatus = service.recallStatusLocked(product, version)
		channels := service.effectiveChannelsLocked(product, version, value.model.ReleaseChannels)
		model.ReleaseChannels = &channels
		values = append(values, model)
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool {
		_, _, left, _ := parseMavenCoordinate(values[i].MavenCoordinate)
		_, _, right, _ := parseMavenCoordinate(values[j].MavenCoordinate)
		return catalog.CompareVersions(left, right) < 0
	})
	return apollo.ListReleasesResponse{Releases: &values}
}

func (service *Service) GetRelease(product, version string) (apollo.Release, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	value, exists := service.releases[product][version]
	if !exists {
		return apollo.Release{}, &Error{Kind: NotFound, Message: "Release not found"}
	}
	model := clone(value.model)
	model.RecallStatus = service.recallStatusLocked(product, version)
	channels := service.effectiveChannelsLocked(product, version, value.model.ReleaseChannels)
	model.ReleaseChannels = &channels
	return model, nil
}

// effectiveChannelsLocked answers Apollo's releaseChannels from the channels the
// Release has actually reached. The caller holds at least a read lock.
//
// Computed on read rather than stamped at publication, for the reason
// recallStatusLocked records below: a Release is published once and promoted
// later, so the stamped value was the version's default channels and stayed
// them however many promotions passed. A Release promoted to a channel was
// served on the channel it started on.
//
// The rule is not restated here. catalog.EffectiveChannels owns which channels
// a Release has reached, including the transitive walk over passed promotions,
// and this only supplies it with what Conduit recorded.
func (service *Service) effectiveChannelsLocked(product, version string, declared *[]string) []string {
	passed := make([]catalog.Promotion, 0)
	for _, environmentPromotions := range service.promotions {
		for _, promotion := range environmentPromotions {
			passed = append(passed, catalog.Promotion{
				Product: promotion.Product, Version: promotion.Version,
				FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
				Passed: promotion.Passed,
			})
		}
	}
	channels := []string(nil)
	if declared != nil {
		channels = append(channels, *declared...)
	}
	return catalog.EffectiveChannels(catalog.Release{
		Product: product, Version: version, Channels: channels,
	}, passed)
}

// recallStatusLocked answers Apollo's recallStatus from the recalls Conduit
// actually holds. The caller holds at least a read lock.
//
// It is computed on read rather than stamped at publication, because a Release
// is published once and recalled later; the stamped value was necessarily
// false, and stayed false however many recalls arrived afterwards.
//
// Aggregated across Environments, because Apollo declares recallStatus on the
// Release while Conduit declares a recall inside an Environment's desired
// state. "Recalled somewhere" is the only statement the catalog level can
// honestly make, and it is the safe direction: reporting a Release as not
// recalled when some Environment has withdrawn it is the error a client cannot
// recover from, since it installs on the strength of it.
//
// recalledBy and recalledAt stay absent. The contract permits it -- "if
// recalled is false, other properties will not be filled out" -- and Conduit's
// Recall carries neither an actor nor a timestamp, so filling them would be
// the invention this surface exists to avoid.
func (service *Service) recallStatusLocked(product, version string) apollo.RecallStatus {
	for _, environmentRecalls := range service.recalls {
		for _, recall := range environmentRecalls {
			if recall.Reverted || recall.Product != product {
				continue
			}
			if !catalog.RecallAppliesToVersion(catalog.Recall{
				Product:                recall.Product,
				StartingVersion:        recall.StartingVersion,
				EndingVersionExclusive: recall.EndingVersionExclusive,
				ExcludedVersions:       recall.ExcludedVersions,
				SingleVersion:          recall.SingleVersion,
			}, version) {
				continue
			}
			reason := recall.Reason
			status := apollo.RecallStatus{Recalled: true}
			if strings.TrimSpace(reason) != "" {
				status.Reason = &reason
			}
			return status
		}
	}
	return apollo.RecallStatus{Recalled: false}
}

func (service *Service) PublishHelmChart(ctx context.Context, coordinate string, body apollo.PublishHelmChartRequest) error {
	extensions := body.Extensions
	if strings.TrimSpace(extensions.HelmChart.HelmChartName) == "" ||
		!catalog.VersionPattern.MatchString(extensions.HelmChart.HelmChartVersion) ||
		!validRepositoryURL(extensions.HelmChart.HelmRepositoryUrl) {
		return &Error{Kind: Invalid, Message: "invalid Helm chart extension"}
	}
	if extensions.Artifacts != nil {
		for _, artifact := range *extensions.Artifacts {
			if strings.TrimSpace(artifact.ArtifactUrl) == "" {
				return &Error{Kind: Invalid, Message: "artifactUrl is required"}
			}
		}
	}
	dependencies, err := normalizeDependencies(dereference(extensions.ProductDependencies))
	if err != nil {
		return err
	}
	return service.publish(
		ctx, coordinate, dependencies, "helm-chart", helmPublicationPayload(body),
		executableRelease(coordinate, body), PublicationAssets{}, "",
	)
}

func (service *Service) PublishService(ctx context.Context, coordinate string, body apollo.PublishServiceRequest) error {
	var locators []apollo.ProductDependencyLocator
	if body.Extensions != nil {
		locators = dereference(body.Extensions.ProductDependencies)
	}
	dependencies, err := normalizeDependencies(locators)
	if err != nil {
		return err
	}
	return service.publish(ctx, coordinate, dependencies, "service", servicePublicationPayload(body), nil, PublicationAssets{}, "")
}

func (service *Service) PublishNativeHelmChart(ctx context.Context, coordinate string, publication NativeHelmPublication) error {
	if strings.TrimSpace(publication.ChartName) == "" || !catalog.VersionPattern.MatchString(publication.ChartVersion) ||
		!validRepositoryURL(publication.ChartRepository) {
		return &Error{Kind: Invalid, Message: "invalid Helm chart extension"}
	}
	dependencies := make([]catalog.Dependency, 0, len(publication.Dependencies))
	for _, dependency := range publication.Dependencies {
		if dependency.ProductGroup == "" || dependency.ProductName == "" ||
			!catalog.VersionPattern.MatchString(dependency.MinimumVersion) ||
			!catalog.VersionMatcherPattern.MatchString(dependency.MaximumVersion) {
			return &Error{Kind: Invalid, Message: "invalid Product dependency"}
		}
		dependencies = append(dependencies, catalog.Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	release := executableNativeRelease(coordinate, publication, dependencies)
	return service.publish(ctx, coordinate, dependencies, "helm-chart", nativePublicationPayload(publication),
		release, publication.Assets, publication.UpgradeStrategy)
}

type publicationHelmChart struct {
	HelmChartName     string `json:"helmChartName"`
	HelmChartVersion  string `json:"helmChartVersion"`
	HelmRepositoryURL string `json:"helmRepositoryUrl"`
}

type publicationArtifact struct {
	ArtifactURL string `json:"artifactUrl"`
}

type publicationDependency struct {
	ProductGroup       string `json:"productGroup"`
	ProductName        string `json:"productName"`
	MinimumVersion     string `json:"minimumVersion"`
	MaximumVersion     string `json:"maximumVersion"`
	RecommendedVersion string `json:"recommendedVersion,omitempty"`
	Optional           bool   `json:"optional,omitempty"`
}

type helmPayload struct {
	Extensions struct {
		HelmChart           publicationHelmChart    `json:"helmChart"`
		Artifacts           []publicationArtifact   `json:"artifacts,omitempty"`
		ProductDependencies []publicationDependency `json:"productDependencies,omitempty"`
		// UpgradeStrategy is in the payload because the payload is what the
		// publication digest covers. Left out, republishing one coordinate with
		// a different declared strategy would be digest-identical, so the
		// immutability check would read it as an idempotent retry and swallow
		// the second declaration in silence. omitempty keeps a payload carrying
		// no declaration byte-identical to one produced before this field
		// existed, so retrying an older publication still succeeds.
		UpgradeStrategy string             `json:"upgradeStrategy,omitempty"`
		PlatformBundle  *PublicationAssets `json:"platformBundle,omitempty"`
	} `json:"extensions"`
}

func helmPublicationPayload(body apollo.PublishHelmChartRequest) helmPayload {
	var result helmPayload
	result.Extensions.HelmChart = publicationHelmChart{
		HelmChartName:     body.Extensions.HelmChart.HelmChartName,
		HelmChartVersion:  body.Extensions.HelmChart.HelmChartVersion,
		HelmRepositoryURL: body.Extensions.HelmChart.HelmRepositoryUrl,
	}
	for _, artifact := range dereference(body.Extensions.Artifacts) {
		result.Extensions.Artifacts = append(result.Extensions.Artifacts, publicationArtifact{ArtifactURL: artifact.ArtifactUrl})
	}
	for _, dependency := range dereference(body.Extensions.ProductDependencies) {
		result.Extensions.ProductDependencies = append(result.Extensions.ProductDependencies, publicationDependency{
			ProductGroup: dependency.ProductGroup, ProductName: dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion,
		})
	}
	return result
}

func nativePublicationPayload(publication NativeHelmPublication) helmPayload {
	var result helmPayload
	result.Extensions.HelmChart = publicationHelmChart{
		HelmChartName: publication.ChartName, HelmChartVersion: publication.ChartVersion,
		HelmRepositoryURL: publication.ChartRepository,
	}
	for _, artifact := range publication.Artifacts {
		result.Extensions.Artifacts = append(result.Extensions.Artifacts, publicationArtifact{ArtifactURL: artifact})
	}
	for _, dependency := range publication.Dependencies {
		// A conversion rather than a field-by-field literal: the two structs
		// differ only in JSON tags, which conversion ignores. It also fails
		// closed — if NativeDependency gains a field the conversion stops
		// compiling, where the literal would silently drop it from every
		// published payload.
		result.Extensions.ProductDependencies = append(
			result.Extensions.ProductDependencies, publicationDependency(dependency))
	}
	result.Extensions.UpgradeStrategy = publication.UpgradeStrategy
	result.Extensions.PlatformBundle = &publication.Assets
	return result
}

func servicePublicationPayload(body apollo.PublishServiceRequest) any {
	var dependencies []publicationDependency
	if body.Extensions != nil {
		for _, dependency := range dereference(body.Extensions.ProductDependencies) {
			dependencies = append(dependencies, publicationDependency{
				ProductGroup: dependency.ProductGroup, ProductName: dependency.ProductName,
				MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion,
			})
		}
	}
	return struct {
		Extensions struct {
			ProductDependencies []publicationDependency `json:"productDependencies,omitempty"`
		} `json:"extensions,omitempty"`
	}{Extensions: struct {
		ProductDependencies []publicationDependency `json:"productDependencies,omitempty"`
	}{ProductDependencies: dependencies}}
}

func (service *Service) publish(
	ctx context.Context,
	coordinate string,
	dependencies []catalog.Dependency,
	kind string,
	payload any,
	release *catalog.Release,
	assets PublicationAssets,
	upgradeStrategy string,
) error {
	if upgradeStrategy != "" && !apollo.UpgradeStrategy(upgradeStrategy).Valid() {
		return &Error{Kind: Invalid, Message: "invalid upgrade strategy"}
	}
	group, name, version, valid := parseMavenCoordinate(coordinate)
	if !valid {
		return &Error{Kind: Invalid, Message: "invalid Maven coordinate"}
	}
	product := group + ":" + name
	for _, candidateVersion := range []string{"", version} {
		retired, err := service.catalog.CoordinateRetired(ctx, product, candidateVersion)
		if err != nil {
			return fmt.Errorf("read catalog retirement: %w", err)
		}
		if retired {
			message := "Product coordinate is retired"
			if candidateVersion != "" {
				message = "Product Release coordinate is retired"
			}
			return &Error{Kind: Conflict, Message: message}
		}
	}
	encoded, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		Payload any    `json:"payload"`
	}{Kind: kind, Payload: payload})
	if err != nil {
		return &Error{Kind: Invalid, Message: "invalid publication payload"}
	}
	digest := sha256.Sum256(encoded)
	publication := catalog.Publication{
		MavenCoordinate: coordinate, Product: product, Version: version, Kind: kind,
		PublicationDigest: "sha256:" + hex.EncodeToString(digest[:]),
		ReleaseChannels:   catalog.DefaultChannelsForVersion(version), UpgradeStrategy: upgradeStrategy,
		Dependencies: dependencies, Release: release, CreatedAt: service.now().UTC(),
		DefaultConfig: assets.DefaultConfig, ConfigSchema: assets.ConfigSchema,
	}
	stored, immutableConflict, err := service.catalog.CreatePublication(ctx, publication)
	if immutableConflict {
		return &Error{Kind: Conflict, Message: "Product Release coordinate is immutable"}
	}
	if err != nil {
		return fmt.Errorf("create catalog publication: %w", err)
	}
	service.restorePublication(stored)
	return nil
}

func normalizeDependencies(values []apollo.ProductDependencyLocator) ([]catalog.Dependency, error) {
	converted := make([]catalog.Dependency, 0, len(values))
	for _, dependency := range values {
		if dependency.ProductGroup == "" || dependency.ProductName == "" ||
			!catalog.VersionPattern.MatchString(dependency.MinimumVersion) ||
			!catalog.VersionMatcherPattern.MatchString(dependency.MaximumVersion) {
			return nil, &Error{Kind: Invalid, Message: "invalid Product dependency"}
		}
		converted = append(converted, catalog.Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion,
		})
	}
	return converted, nil
}

func (service *Service) restorePublication(publication catalog.Publication) {
	group, _, _, valid := parseMavenCoordinate(publication.MavenCoordinate)
	if !valid {
		return
	}
	dependencies := make([]apollo.ProductDependency, 0, len(publication.Dependencies))
	for _, dependency := range publication.Dependencies {
		product, minimum, maximum, optional := dependency.Product, dependency.MinimumVersion, dependency.MaximumVersion, dependency.Optional
		dependencies = append(dependencies, apollo.ProductDependency{
			ProductId: &product, MinVersion: &minimum, MaxVersion: &maximum, Optional: &optional,
		})
	}
	channels := append([]string(nil), publication.ReleaseChannels...)
	model := apollo.Release{
		MavenCoordinate: publication.MavenCoordinate, RecallStatus: apollo.RecallStatus{Recalled: false},
		ProductDependencies: &dependencies, ReleaseChannels: &channels,
	}
	// Only a declaration is served. A pointer to the empty string is not an
	// omission: omitempty tests the pointer, so it would put `"upgradeStrategy":
	// ""` on the wire, and "" is not a member of the pinned enum. That is not
	// hypothetical -- the Terraform publication route stores no strategy and was
	// served exactly that way. An absent key says the manifest declared nothing,
	// which is true and is what the pinned snapshot permits: Release requires
	// only mavenCoordinate and recallStatus.
	if publication.UpgradeStrategy != "" {
		upgrade := apollo.UpgradeStrategy(publication.UpgradeStrategy)
		model.UpgradeStrategy = &upgrade
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.products[publication.Product] = apollo.Product{Id: publication.Product, Group: group}
	if service.releases[publication.Product] == nil {
		service.releases[publication.Product] = make(map[string]releaseRecord)
	}
	service.releases[publication.Product][publication.Version] = releaseRecord{model: model, digest: publication.PublicationDigest}
}

func (service *Service) SyncEnvironment(projection environments.CompatibilityProjection) {
	rid := environmentRID(projection.ID)
	environmentType, status := apollo.EnvironmentType("production"), apollo.ManagementStatus("managed")
	service.mu.Lock()
	defer service.mu.Unlock()
	service.environments[rid] = apollo.Environment{Rid: rid, Name: projection.ID, EnvironmentType: &environmentType, Status: status}
	installations := make(map[string]apollo.Installation, len(projection.Entities))
	for _, entity := range projection.Entities {
		installationRID := "ri.apollo.main.installation." + stableSuffix(projection.ID+":"+entity.Name)
		entityID := helmChartEntityID(projection.ID, entity.Name)
		packageRID := "ri.apollo.main.package." + stableSuffix(entity.Product)
		channel, version := apollo.ReleaseChannel(entity.ReleaseChannel), entity.Version
		installations[installationRID] = apollo.Installation{
			Rid: installationRID, EnvironmentRid: rid, ApolloEntityId: entityID,
			PackageRid: packageRID, ProductId: entity.Product, ReleaseChannel: &channel, Version: &version,
		}
	}
	service.installations[rid] = installations
	if service.recalls == nil {
		service.recalls = make(map[string][]environments.ProjectedRecall)
	}
	// Replaced wholesale rather than merged: this projection is the whole of
	// what the Environment currently declares, so a recall that has been
	// removed from desired state must stop being reported.
	service.recalls[rid] = append([]environments.ProjectedRecall(nil), projection.Recalls...)
	if service.promotions == nil {
		service.promotions = make(map[string][]environments.ProjectedPromotion)
	}
	service.promotions[rid] = append([]environments.ProjectedPromotion(nil), projection.Promotions...)
	for _, namespace := range projection.Namespaces {
		id := namespace.Name
		if id == "" {
			id = namespace.RID
		}
		owners, editors, viewers := []string{}, []string{}, []string{}
		service.namespaces[id] = apollo.Namespace{Id: id, Owners: &owners, Editors: &editors, Viewers: &viewers}
	}
}

func (service *Service) ListEnvironments() apollo.ListEnvironmentsResponse {
	service.mu.RLock()
	values := make([]apollo.Environment, 0, len(service.environments))
	for _, value := range service.environments {
		values = append(values, clone(value))
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Rid < values[j].Rid })
	return apollo.ListEnvironmentsResponse{Environments: &values}
}

func (service *Service) GetEnvironment(rid string) (apollo.Environment, error) {
	service.mu.RLock()
	value, exists := service.environments[rid]
	service.mu.RUnlock()
	if !exists {
		return apollo.Environment{}, &Error{Kind: NotFound, Message: "Environment not found"}
	}
	return clone(value), nil
}

func (service *Service) ListInstallations(environmentRID string) apollo.ListInstallationsResponse {
	service.mu.RLock()
	values := make([]apollo.Installation, 0, len(service.installations[environmentRID]))
	for _, value := range service.installations[environmentRID] {
		values = append(values, clone(value))
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Rid < values[j].Rid })
	return apollo.ListInstallationsResponse{Installations: &values}
}

func (service *Service) GetInstallation(environmentRID, installationRID string) (apollo.Installation, error) {
	service.mu.RLock()
	value, exists := service.installations[environmentRID][installationRID]
	service.mu.RUnlock()
	if !exists {
		return apollo.Installation{}, &Error{Kind: NotFound, Message: "Installation not found"}
	}
	return clone(value), nil
}

func (service *Service) ListNamespaces() apollo.ListNamespacesResponse {
	service.mu.RLock()
	values := make([]apollo.Namespace, 0, len(service.namespaces))
	for _, value := range service.namespaces {
		values = append(values, clone(value))
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Id < values[j].Id })
	return apollo.ListNamespacesResponse{Namespaces: &values}
}

func (service *Service) PutEntityInfo(ctx context.Context, environment string, values []apollo.EntityInfo) error {
	if err := ValidateEntityInfo(values); err != nil {
		return err
	}
	service.mu.Lock()
	service.entityInfo[environment] = clone(values)
	service.mu.Unlock()
	return service.persistReportedState(ctx, environment)
}

func (service *Service) PutEntityHealth(ctx context.Context, environment string, values []apollo.EntityHealth) error {
	if err := ValidateEntityHealth(values); err != nil {
		return err
	}
	service.mu.Lock()
	service.entityHealth[environment] = clone(values)
	service.mu.Unlock()
	return service.persistReportedState(ctx, environment)
}

func (service *Service) persistReportedState(ctx context.Context, environment string) error {
	service.mu.RLock()
	info, health := clone(service.entityInfo[environment]), clone(service.entityHealth[environment])
	service.mu.RUnlock()
	plan, err := service.reporting.LatestPlan(ctx, environment)
	if err != nil {
		return err
	}
	if plan.ID == "" {
		return nil
	}
	existing := map[string]environments.ReportedEntity{}
	if report, reportErr := service.reporting.GetReport(ctx, environment); reportErr == nil {
		for _, entity := range report.Entities {
			existing[entity.InstallationID] = entity
		}
	}
	now := service.now().UTC()
	reported := make([]environments.ReportedEntity, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		entity := existing[step.InstallationID]
		if entity.InstallationID == "" {
			entity = environments.ReportedEntity{
				InstallationID: step.InstallationID, Product: step.Product, Version: step.Version, Namespace: step.Namespace,
				Health: environments.HealthUnknown, RolloutStatus: environments.RolloutOutOfDate,
				ConfigStatus: environments.ConfigIndeterminate, Lifecycle: environments.LifecycleUnknown,
			}
		}
		matchingInfo := MatchInfo(info, step)
		matchingHealth := MatchHealth(health, matchingInfo, step.Product)
		if len(dereference(matchingInfo.NodesInfo)) > 0 || len(matchingHealth) > 0 {
			details, result := AggregateHealth(matchingInfo.Quorum, matchingHealth)
			entity.Health, entity.HealthDetails, entity.ObservedAt = result, &details, now
			if matchingInfo.K8sNamespace != nil && *matchingInfo.K8sNamespace != "" {
				entity.Namespace = *matchingInfo.K8sNamespace
			}
			if len(matchingHealth) > 0 {
				entity.Version = matchingHealth[0].ProductVersion
			}
			if len(dereference(matchingInfo.NodesInfo)) > 0 {
				entity.Lifecycle = environments.LifecycleInstalled
			}
		}
		reported = append(reported, entity)
	}
	_, err = service.reporting.PutReport(ctx, environments.Report{
		Environment: environment, AgentID: "apollo-public-api", ObservedAt: now, Entities: reported,
	})
	return err
}

func ValidateEntityInfo(values []apollo.EntityInfo) error {
	seen := make(map[string]struct{}, len(values))
	for index, entity := range values {
		if (entity.EntityType != "HELM_CHART" && entity.EntityType != "SERVICE") || strings.TrimSpace(entity.Id) == "" ||
			strings.TrimSpace(entity.ProductGroup) == "" || strings.TrimSpace(entity.ProductName) == "" {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesInfo[%d]: entityType, id, productGroup, and productName are required", index)}
		}
		if _, duplicate := seen[entity.Id]; duplicate {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesInfo[%d].id must be unique", index)}
		}
		seen[entity.Id] = struct{}{}
		if entity.Quorum != nil && !oneOf(string(*entity.Quorum), "UNKNOWN", "ANY", "ALL", "ALL_BUT_ONE", "SIMPLE_MAJORITY", "NO_GUARANTEE") {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesInfo[%d].quorum is invalid", index)}
		}
		for nodeIndex, node := range dereference(entity.NodesInfo) {
			if node.CreationTimestamp.IsZero() || node.HostId == "" || node.NodeId == "" || !catalog.VersionPattern.MatchString(node.ProductVersion) {
				return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesInfo[%d].nodesInfo[%d]: complete valid node metadata is required", index, nodeIndex)}
			}
		}
	}
	return nil
}

func ValidateEntityHealth(values []apollo.EntityHealth) error {
	seen := make(map[string]struct{}, len(values))
	for index, entity := range values {
		if entity.ProductGroup == "" || entity.ProductName == "" || !catalog.VersionPattern.MatchString(entity.ProductVersion) || entity.NodeId == "" {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d]: product, version, and nodeId are required", index)}
		}
		key := entity.ProductGroup + ":" + entity.ProductName + "\x00" + entity.NodeId
		if _, duplicate := seen[key]; duplicate {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d] duplicates a Product node", index)}
		}
		seen[key] = struct{}{}
		containers := map[string]struct{}{}
		for containerIndex, container := range dereference(entity.ContainerStatuses) {
			if container.ContainerId == "" {
				return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].containerId must not be empty", index, containerIndex)}
			}
			if _, duplicate := containers[container.ContainerId]; duplicate {
				return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].containerId must be unique per node", index, containerIndex)}
			}
			containers[container.ContainerId] = struct{}{}
			for checkIndex, check := range dereference(container.Checks) {
				if check.Id == "" || check.Params == nil || !oneOf(string(check.Result), environments.HealthHealthy, environments.HealthRepairing, environments.HealthWarning, environments.HealthError, environments.HealthTerminal) {
					return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d].containerStatuses[%d].checks[%d]: id, params, and valid result are required", index, containerIndex, checkIndex)}
				}
			}
		}
		if encoded, err := json.Marshal(entity); err == nil && len(encoded) > 10_000 {
			return &Error{Kind: Invalid, Message: fmt.Sprintf("entitiesHealth[%d] must not exceed 10,000 bytes", index)}
		}
	}
	return nil
}

func MatchInfo(info []apollo.EntityInfo, step environments.PlanStepProjection) apollo.EntityInfo {
	for _, candidate := range info {
		if candidate.Id == step.Entity && candidate.ProductGroup+":"+candidate.ProductName == step.Product {
			return candidate
		}
	}
	parts := strings.SplitN(step.Product, ":", 2)
	group, name := step.Product, ""
	if len(parts) == 2 {
		group, name = parts[0], parts[1]
	}
	quorum := apollo.Quorum("UNKNOWN")
	return apollo.EntityInfo{ProductGroup: group, ProductName: name, Quorum: &quorum}
}

func MatchHealth(health []apollo.EntityHealth, info apollo.EntityInfo, product string) []apollo.EntityHealth {
	nodes := map[string]struct{}{}
	for _, node := range dereference(info.NodesInfo) {
		nodes[node.NodeId] = struct{}{}
	}
	result := []apollo.EntityHealth{}
	for _, candidate := range health {
		_, matchedNode := nodes[candidate.NodeId]
		if candidate.ProductGroup+":"+candidate.ProductName == product && (len(nodes) == 0 || matchedNode) {
			result = append(result, candidate)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeId < result[j].NodeId })
	return result
}

func AggregateHealth(quorum *apollo.Quorum, health []apollo.EntityHealth) (environments.EntityHealthDetails, string) {
	quorumValue := "UNKNOWN"
	if quorum != nil && *quorum != "" {
		quorumValue = string(*quorum)
	}
	details := environments.EntityHealthDetails{Quorum: quorumValue}
	healthy, worst := 0, environments.HealthHealthy
	for _, node := range health {
		nodeResult := environments.HealthHealthy
		containers := make([]environments.EntityContainerHealth, 0, len(dereference(node.ContainerStatuses)))
		for _, container := range dereference(node.ContainerStatuses) {
			result := environments.HealthHealthy
			if !container.Liveness || !container.Readiness {
				result = environments.HealthError
			}
			checks := make([]environments.HealthCheck, 0, len(dereference(container.Checks)))
			for _, check := range dereference(container.Checks) {
				result = environments.WorseHealth(result, string(check.Result))
				checks = append(checks, environments.HealthCheck{ID: check.Id, Message: check.Message, Params: clone(check.Params), Result: string(check.Result)})
			}
			nodeResult = environments.WorseHealth(nodeResult, result)
			containers = append(containers, environments.EntityContainerHealth{
				ContainerID: container.ContainerId, Liveness: container.Liveness, Readiness: container.Readiness, Result: result, Checks: checks,
			})
		}
		if nodeResult == environments.HealthHealthy {
			healthy++
		}
		worst = environments.WorseHealth(worst, nodeResult)
		details.Nodes = append(details.Nodes, environments.EntityNodeHealth{
			NodeID: node.NodeId, ProductVersion: node.ProductVersion, Result: nodeResult, Containers: containers,
		})
	}
	if len(health) == 0 {
		return details, environments.HealthUnknown
	}
	required := len(health)
	switch quorumValue {
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
		return details, environments.HealthIndeterminate
	}
	if healthy >= required {
		return details, environments.HealthHealthy
	}
	return details, worst
}

func executableRelease(coordinate string, body apollo.PublishHelmChartRequest) *catalog.Release {
	group, name, version, valid := parseMavenCoordinate(coordinate)
	if !valid {
		return nil
	}
	chart, exact := immutableArtifact(body.Extensions.HelmChart.HelmRepositoryUrl)
	if !exact {
		return nil
	}
	artifacts := dereference(body.Extensions.Artifacts)
	images := make([]catalog.Artifact, 0, len(artifacts))
	for _, candidate := range artifacts {
		artifact, exact := immutableArtifact(candidate.ArtifactUrl)
		if !exact {
			return nil
		}
		images = append(images, artifact)
	}
	if len(images) == 0 {
		return nil
	}
	release := &catalog.Release{
		Product: group + ":" + name, Version: version, Chart: chart, Images: images,
		Channels: catalog.DefaultChannelsForVersion(version),
	}
	for _, dependency := range dereference(body.Extensions.ProductDependencies) {
		release.Dependencies = append(release.Dependencies, catalog.Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion,
		})
	}
	return release
}

func executableNativeRelease(
	coordinate string, publication NativeHelmPublication, dependencies []catalog.Dependency,
) *catalog.Release {
	group, name, version, valid := parseMavenCoordinate(coordinate)
	if !valid {
		return nil
	}
	chart, exact := immutableArtifact(publication.ChartRepository)
	if !exact {
		return nil
	}
	images := make([]catalog.Artifact, 0, len(publication.Artifacts))
	for _, reference := range publication.Artifacts {
		artifact, exact := immutableArtifact(reference)
		if !exact {
			return nil
		}
		images = append(images, artifact)
	}
	if len(images) == 0 {
		return nil
	}
	return &catalog.Release{
		Product: group + ":" + name, Version: version, Chart: chart, Images: images,
		Channels: catalog.DefaultChannelsForVersion(version), Dependencies: append([]catalog.Dependency(nil), dependencies...),
	}
}

func immutableArtifact(reference string) (catalog.Artifact, bool) {
	reference = strings.TrimPrefix(strings.TrimSpace(reference), "oci://")
	parts := strings.Split(reference, "@")
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "sha256:") || len(parts[1]) != len("sha256:")+64 {
		return catalog.Artifact{}, false
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(parts[1], "sha256:")); err != nil {
		return catalog.Artifact{}, false
	}
	colon, slash := strings.LastIndex(parts[0], ":"), strings.LastIndex(parts[0], "/")
	if colon <= slash || colon == len(parts[0])-1 {
		return catalog.Artifact{}, false
	}
	artifact := catalog.Artifact{Repository: parts[0][:colon], Tag: parts[0][colon+1:], Digest: parts[1]}
	if artifact.Repository == "" || artifact.Tag == "latest" {
		return catalog.Artifact{}, false
	}
	return artifact, true
}

func parseMavenCoordinate(coordinate string) (string, string, string, bool) {
	parts := strings.Split(coordinate, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || !catalog.VersionPattern.MatchString(parts[2]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func validRepositoryURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "oci://")
}

func environmentRID(id string) string {
	if strings.HasPrefix(id, "ri.apollo.main.environment.") {
		return id
	}
	return "ri.apollo.main.environment." + stableSuffix(id)
}

func helmChartEntityID(environment, entity string) string {
	if strings.TrimSpace(environment) == "" || strings.TrimSpace(entity) == "" ||
		strings.Contains(environment, ":") || strings.Contains(entity, ":") {
		return ""
	}
	id := "aeid:" + environment + ":helm-chart:" + entity
	if len(id) > 255 {
		return ""
	}
	return id
}

func stableSuffix(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func dereference[T any](values *[]T) []T {
	if values == nil {
		return nil
	}
	return *values
}

func clone[T any](value T) T {
	encoded, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(encoded, &result)
	return result
}
