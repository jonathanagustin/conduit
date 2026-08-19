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

// ReleaseLifecyclePort is the durable authority for what Conduit has withdrawn
// and what it has promoted. It is read on every catalog read rather than
// pushed into this process when a desired state is accepted, because the POST
// that declares a recall is served by ONE Hub replica while the GET that asks
// about it is served by either: a projection held in memory answers "not
// recalled" on the replica that never saw the write, and on every replica after
// a restart. That is the unrecoverable direction of the error -- a client
// installs a withdrawn Release on the strength of it.
type ReleaseLifecyclePort interface {
	// ReleaseLifecycle returns every recall and promotion currently declared by
	// any Environment's desired state, aggregated across Environments because
	// Apollo declares both on the Release while Conduit declares them inside an
	// Environment.
	ReleaseLifecycle(context.Context) (ReleaseLifecycle, error)
}

// EnvironmentInventoryPort is the durable authority for which Environments this
// Hub governs, what is installed in them, and which Namespaces they target. It
// is the same failure ReleaseLifecyclePort exists for, one surface over: the
// inventory was pushed into this process by whichever replica accepted a
// desired state, so a replica that had not served that POST -- and every replica
// after a restart, because New rehydrates publications alone -- answered the
// three list routes with an EMPTY list.
//
// Empty is the wrong shape of wrong. A client cannot distinguish it from "this
// Hub governs nothing", so an unanswered question reads as a settled negative:
// the unknown-reads-as-none conflation internal/apollographql/ports says the
// provenance model exists to prevent, arriving through the store rather than
// through authorization.
//
// It is a SEPARATE port from ReleaseLifecyclePort, not a widening of it, for two
// reasons that are not stylistic. First, the two project different shapes and
// are read at different times: every catalog read consults the lifecycle, and
// folding per-Environment Entities and Namespaces into ReleaseLifecycle would
// make ListReleases build a fleet inventory it never looks at. Second, and
// decisively, they need DIFFERENT Plans. The lifecycle deliberately excludes
// command Plans (latestDeclaredStates) because a command stores a desired state
// with its Recalls stripped; the inventory must INCLUDE them, because a command
// carries the Environment's full Entity set and the write path recorded it.
// One port would have to pick one filter, and either choice is a defect.
type EnvironmentInventoryPort interface {
	// EnvironmentInventory returns one projection per Environment the Hub has
	// accepted a desired state for, which is exactly the set the pushed
	// projection used to hold -- read from durable state instead of from
	// whichever process happened to serve the write.
	EnvironmentInventory(context.Context) ([]environments.CompatibilityProjection, error)
}

// ReleaseLifecycle is what a single read of durable desired state yields. Both
// halves travel together so that answering a Release costs one query rather
// than one per field, and so that recallStatus and releaseChannels on the same
// response describe the same moment.
type ReleaseLifecycle struct {
	Recalls    []environments.ProjectedRecall
	Promotions []environments.ProjectedPromotion
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
	mu           sync.RWMutex
	products     map[string]apollo.Product
	releases     map[string]map[string]releaseRecord
	entityInfo   map[string][]apollo.EntityInfo
	entityHealth map[string][]apollo.EntityHealth
	// recalls and promotions are deliberately absent from this struct, and so
	// are environments, installations and namespaces. All five were held here
	// and all five answered wrong on any replica that had not served the write:
	// the first two as "not recalled", the last three as an empty list. They are
	// read through lifecycle and inventory instead.
	lifecycle ReleaseLifecyclePort
	inventory EnvironmentInventoryPort
	catalog   CatalogPort
	reporting environmentports.ReportingPort
	now       func() time.Time
}

func New(
	ctx context.Context, catalogPort CatalogPort, reporting environmentports.ReportingPort,
	lifecycle ReleaseLifecyclePort, inventory EnvironmentInventoryPort,
) (*Service, error) {
	// Refused rather than tolerated: a nil lifecycle port would leave every
	// recallStatus unanswerable at request time, which is the failure this
	// surface is least able to report and least able to afford.
	if lifecycle == nil {
		return nil, errors.New("Apollo requires a Release lifecycle port")
	}
	// Refused for the same reason and with more force: a nil inventory port
	// would not fail loudly, it would serve an empty Environment list, and an
	// empty list is indistinguishable from a Hub that governs nothing.
	if inventory == nil {
		return nil, errors.New("Apollo requires an Environment inventory port")
	}
	service := &Service{
		products: make(map[string]apollo.Product), releases: make(map[string]map[string]releaseRecord),
		entityInfo:   make(map[string][]apollo.EntityInfo),
		entityHealth: make(map[string][]apollo.EntityHealth), catalog: catalogPort, reporting: reporting,
		lifecycle: lifecycle, inventory: inventory, now: time.Now,
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

// ListReleases reads the lifecycle once and applies it to every Release, so a
// list costs the same one query a single Release does, and so no two Releases
// in one response are described from different moments.
func (service *Service) ListReleases(ctx context.Context, product string) (apollo.ListReleasesResponse, error) {
	lifecycle, err := service.lifecycle.ReleaseLifecycle(ctx)
	if err != nil {
		return apollo.ListReleasesResponse{}, fmt.Errorf("read Release lifecycle: %w", err)
	}
	service.mu.RLock()
	values := make([]apollo.Release, 0, len(service.releases[product]))
	for version, value := range service.releases[product] {
		model := clone(value.model)
		model.RecallStatus = recallStatus(product, version, lifecycle.Recalls)
		channels := effectiveChannels(product, version, value.model.ReleaseChannels, lifecycle.Promotions)
		model.ReleaseChannels = &channels
		values = append(values, model)
	}
	service.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool {
		_, _, left, _ := parseMavenCoordinate(values[i].MavenCoordinate)
		_, _, right, _ := parseMavenCoordinate(values[j].MavenCoordinate)
		return catalog.CompareVersions(left, right) < 0
	})
	return apollo.ListReleasesResponse{Releases: &values}, nil
}

func (service *Service) GetRelease(ctx context.Context, product, version string) (apollo.Release, error) {
	// The lifecycle read comes first and its failure is fatal to the response.
	// Answering the rest of the Release with recallStatus false because the
	// store could not be reached would report a withdrawal that exists as an
	// absence of one; a client can retry an error, it cannot un-install.
	lifecycle, err := service.lifecycle.ReleaseLifecycle(ctx)
	if err != nil {
		return apollo.Release{}, fmt.Errorf("read Release lifecycle: %w", err)
	}
	service.mu.RLock()
	defer service.mu.RUnlock()
	value, exists := service.releases[product][version]
	if !exists {
		return apollo.Release{}, &Error{Kind: NotFound, Message: "Release not found"}
	}
	model := clone(value.model)
	model.RecallStatus = recallStatus(product, version, lifecycle.Recalls)
	channels := effectiveChannels(product, version, value.model.ReleaseChannels, lifecycle.Promotions)
	model.ReleaseChannels = &channels
	return model, nil
}

// effectiveChannels answers Apollo's releaseChannels from the channels the
// Release has actually reached, given the promotions read from durable state.
//
// Computed on read rather than stamped at publication, for the reason
// recallStatus records below: a Release is published once and promoted
// later, so the stamped value was the version's default channels and stayed
// them however many promotions passed. A Release promoted to a channel was
// served on the channel it started on.
//
// It takes the promotions rather than reading them, and holds no lock, because
// the read is a repository query the caller has already made once for the whole
// response -- doing it per Release would multiply the query by the catalog.
//
// The rule is not restated here. catalog.EffectiveChannels owns which channels
// a Release has reached, including the transitive walk over passed promotions,
// and this only supplies it with what Conduit recorded.
func effectiveChannels(
	product, version string, declared *[]string, promotions []environments.ProjectedPromotion,
) []string {
	passed := make([]catalog.Promotion, 0, len(promotions))
	for _, promotion := range promotions {
		passed = append(passed, catalog.Promotion{
			Product: promotion.Product, Version: promotion.Version,
			FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
			Passed: promotion.Passed,
		})
	}
	channels := []string(nil)
	if declared != nil {
		channels = append(channels, *declared...)
	}
	return catalog.EffectiveChannels(catalog.Release{
		Product: product, Version: version, Channels: channels,
	}, passed)
}

// recallStatus answers Apollo's recallStatus from the recalls Conduit actually
// holds, as read from durable desired state by the caller.
//
// It is computed on read rather than stamped at publication, because a Release
// is published once and recalled later; the stamped value was necessarily
// false, and stayed false however many recalls arrived afterwards.
//
// The recalls arrive already aggregated across Environments, because Apollo
// declares recallStatus on the Release while Conduit declares a recall inside
// an Environment's desired state. "Recalled somewhere" is the only statement
// the catalog level can honestly make, and it is the safe direction: reporting
// a Release as not recalled when some Environment has withdrawn it is the error
// a client cannot recover from, since it installs on the strength of it.
//
// They are passed in rather than held on the Service for the same reason the
// answer is computed rather than stamped: a copy kept in one process is right
// only in that process. The Hub runs more than one replica and restarts, and
// the copy was written solely by the request that accepted the desired state.
//
// recalledBy and recalledAt stay absent. The contract permits it -- "if
// recalled is false, other properties will not be filled out" -- and Conduit's
// Recall carries neither an actor nor a timestamp, so filling them would be
// the invention this surface exists to avoid.
func recallStatus(product, version string, recalls []environments.ProjectedRecall) apollo.RecallStatus {
	for _, recall := range recalls {
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

// projectEnvironment is the translation SyncEnvironment used to perform at write
// time, moved to read time unchanged. It stays a pure function of one durable
// projection so the answer cannot depend on which process is asked -- which is
// the whole of the defect it replaces.
//
// SyncEnvironment itself is gone rather than left in place beside these reads. A
// write path nothing reads is not free: it is a second, stale copy of the same
// facts that the next reader may reasonably mistake for the source, and it is
// how this surface acquired the bug in the first place.
func projectEnvironment(projection environments.CompatibilityProjection) (
	apollo.Environment, map[string]apollo.Installation, map[string]apollo.Namespace,
) {
	rid := EnvironmentRID(projection.ID)
	// environmentType is omitted, not stamped. It used to be the constant
	// "production" for every Environment, including the one this repository's
	// own test names dev-use2, and "production" is a legal member of the pinned
	// enum, so the shape gate passed it (#57, #112). Conduit's Environments
	// domain has no environment-type concept at all -- nothing on
	// CompatibilityProjection distinguishes one -- so there is no value to
	// derive and no publisher who stated one.
	//
	// Omission is available here and is not a null: the pinned snapshot lists
	// environmentType among Environment's properties and NOT among its required
	// ones, so an absent key is a contract-legal way to say "Conduit holds no
	// answer". ADR 0007 forbids refusing a coordinate for want of data; it does
	// not require inventing a value for an optional field the contract already
	// permits to be absent.
	//
	// status has no such option. The same snapshot lists it in Environment's
	// required set, so omitting it would break the contract to make a point
	// about provenance. It stays "managed" and is DECLARED as a Conduit-supplied
	// constant in docs/apollo/public-api.md and docs/operations/TENANT-FIDELITY.md
	// rather than left to be discovered.
	environment := apollo.Environment{
		Rid: rid, Name: projection.ID, Status: apollo.ManagementStatus("managed"),
	}
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
	namespaces := make(map[string]apollo.Namespace, len(projection.Namespaces))
	for _, namespace := range projection.Namespaces {
		id := namespace.Name
		if id == "" {
			id = namespace.RID
		}
		// owners, editors and viewers are omitted rather than served empty.
		//
		// They used to be three `[]string{}` values, and an empty JSON array is
		// a fact: it says no group holds Owner, Editor or Viewer permissions on
		// this Namespace. Governance never produced that claim --
		// ProjectedNamespace carries a RID and a Name and nothing else, and
		// Conduit has no notion of a Namespace permission group to report
		// (issues #57, #112). The empty array was the unknown-reads-as-none
		// conflation, on the surface that has no provenance tier to disclose it
		// with.
		//
		// The pinned snapshot requires only `id` on Namespace, so all three are
		// omissible and an absent key means "Conduit holds no answer". That also
		// narrows the cross-surface inconsistency #57 raised against
		// ports/ports.go's claim that a client "cannot get a different answer by
		// choosing a protocol": GraphQL still refuses ApolloEnvironment.namespaceId
		// outright, and REST no longer answers the neighbouring question with a
		// sliver that reads as a fact.
		namespaces[id] = apollo.Namespace{Id: id}
	}
	return environment, installations, namespaces
}

// ListEnvironments answers from durable state on every call, so the fleet a
// client is shown does not depend on which replica took the last write. The
// error is returned rather than swallowed into an empty list: "I could not
// reach the store" and "this Hub governs nothing" are different answers and a
// client acts differently on each.
func (service *Service) ListEnvironments(ctx context.Context) (apollo.ListEnvironmentsResponse, error) {
	inventory, err := service.inventory.EnvironmentInventory(ctx)
	if err != nil {
		return apollo.ListEnvironmentsResponse{}, fmt.Errorf("read Environment inventory: %w", err)
	}
	values := make([]apollo.Environment, 0, len(inventory))
	for _, projection := range inventory {
		environment, _, _ := projectEnvironment(projection)
		values = append(values, environment)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Rid < values[j].Rid })
	return apollo.ListEnvironmentsResponse{Environments: &values}, nil
}

func (service *Service) GetEnvironment(ctx context.Context, rid string) (apollo.Environment, error) {
	inventory, err := service.inventory.EnvironmentInventory(ctx)
	if err != nil {
		return apollo.Environment{}, fmt.Errorf("read Environment inventory: %w", err)
	}
	for _, projection := range inventory {
		if environment, _, _ := projectEnvironment(projection); environment.Rid == rid {
			return environment, nil
		}
	}
	return apollo.Environment{}, &Error{Kind: NotFound, Message: "Environment not found"}
}

func (service *Service) ListInstallations(ctx context.Context, environmentRID string) (apollo.ListInstallationsResponse, error) {
	inventory, err := service.inventory.EnvironmentInventory(ctx)
	if err != nil {
		return apollo.ListInstallationsResponse{}, fmt.Errorf("read Environment inventory: %w", err)
	}
	values := []apollo.Installation{}
	for _, projection := range inventory {
		environment, installations, _ := projectEnvironment(projection)
		if environment.Rid != environmentRID {
			continue
		}
		for _, value := range installations {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Rid < values[j].Rid })
	return apollo.ListInstallationsResponse{Installations: &values}, nil
}

func (service *Service) GetInstallation(ctx context.Context, environmentRID, installationRID string) (apollo.Installation, error) {
	inventory, err := service.inventory.EnvironmentInventory(ctx)
	if err != nil {
		return apollo.Installation{}, fmt.Errorf("read Environment inventory: %w", err)
	}
	for _, projection := range inventory {
		environment, installations, _ := projectEnvironment(projection)
		if environment.Rid != environmentRID {
			continue
		}
		if value, exists := installations[installationRID]; exists {
			return value, nil
		}
	}
	return apollo.Installation{}, &Error{Kind: NotFound, Message: "Installation not found"}
}

// ListNamespaces folds every Environment's Namespaces into one list, because
// Apollo's Namespace is Hub-wide while Conduit declares Namespaces inside an
// Environment. Two Environments naming the same Namespace collapse to one
// entry, as they did before.
func (service *Service) ListNamespaces(ctx context.Context) (apollo.ListNamespacesResponse, error) {
	inventory, err := service.inventory.EnvironmentInventory(ctx)
	if err != nil {
		return apollo.ListNamespacesResponse{}, fmt.Errorf("read Environment inventory: %w", err)
	}
	unique := make(map[string]apollo.Namespace)
	for _, projection := range inventory {
		_, _, namespaces := projectEnvironment(projection)
		for id, value := range namespaces {
			unique[id] = value
		}
	}
	values := make([]apollo.Namespace, 0, len(unique))
	for _, value := range unique {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Id < values[j].Id })
	return apollo.ListNamespacesResponse{Namespaces: &values}, nil
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
		matchingInfo, observedTheEntity := observedInfo(info, step)
		matchingHealth := MatchHealth(health, matchingInfo, step.Product)
		if len(dereference(matchingInfo.NodesInfo)) > 0 || len(matchingHealth) > 0 {
			details, result := AggregateHealth(quorumFor(matchingInfo, observedTheEntity, entity), matchingHealth)
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
	matched, _ := observedInfo(info, step)
	return matched
}

// observedInfo is MatchInfo with the one bit MatchInfo throws away: whether the
// returned EntityInfo was reported by an Agent or synthesized here because
// nothing matched. The synthesized value is indistinguishable from a real one --
// same type, a plausible ProductGroup and ProductName, and a Quorum of
// "UNKNOWN" -- and AggregateHealth turns that quorum into INDETERMINATE
// regardless of how healthy the nodes are.
//
// That is harmless when the miss means the Agent has said nothing about the
// entity. It is not harmless when the miss means THIS PROCESS has said nothing:
// entitiesInfo and entitiesHealth arrive as two requests and two Hub replicas
// can serve one each, so a replica holding only the health report reaches the
// merge with no info at all and writes INDETERMINATE over the HEALTHY the other
// replica computed from the same Agent's data (issue #326).
func observedInfo(info []apollo.EntityInfo, step environments.PlanStepProjection) (apollo.EntityInfo, bool) {
	for _, candidate := range info {
		if candidate.Id == step.Entity && candidate.ProductGroup+":"+candidate.ProductName == step.Product {
			return candidate, true
		}
	}
	parts := strings.SplitN(step.Product, ":", 2)
	group, name := step.Product, ""
	if len(parts) == 2 {
		group, name = parts[0], parts[1]
	}
	quorum := apollo.Quorum("UNKNOWN")
	return apollo.EntityInfo{ProductGroup: group, ProductName: name, Quorum: &quorum}, false
}

// quorumFor picks the quorum the health aggregation runs under. When the Agent's
// info was observed in this process, that is the answer. When it was not, the
// durable Environment Report is consulted before falling back to the
// synthesized "UNKNOWN": the report is the copy every replica can see, and a
// quorum another replica already recorded there is observed evidence, merely
// observed elsewhere. This is the same move issue #325 made for Environments,
// Installations and Namespaces -- read the shared copy rather than this
// process's memory -- and it stops a partial report from degrading a verdict.
//
// It is a floor, not the whole fix. A replica without the info still cannot
// filter health by the entity's node set or advance Lifecycle; issue #326 tracks
// persisting the raw Agent reports, which is what would close that.
func quorumFor(
	info apollo.EntityInfo, observed bool, durable environments.ReportedEntity,
) *apollo.Quorum {
	if observed || durable.HealthDetails == nil || durable.HealthDetails.Quorum == "" {
		return info.Quorum
	}
	carried := apollo.Quorum(durable.HealthDetails.Quorum)
	return &carried
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

// EnvironmentRID builds the Apollo resource identifier for an Environment.
//
// Exported because a second caller needs the same value and there must be one
// definition of it. Notifications are joined to subscriptions by resource
// identifier, so an escalation raised elsewhere in the Hub has to name an
// Environment exactly as this surface names it, or a preference set here would
// match nothing raised there. A copy of the construction in the other package
// would agree until one of them was edited.
//
// The grammar is Apollo's: the pinned OpenAPI publishes
// ri.apollo.main.environment.123, and ri.autopilot.main.team.123 alongside it
// shows the service segment naming the owning service. What Apollo does not
// publish is the locator -- its examples use the placeholder 123 -- so the
// locator is Conduit's, and the coordinates serving these values disclose that
// with valueTier: CONDUIT_NATIVE per ADR 0007.
//
// A truncated digest rather than a UUID because the identifier has to be stable:
// the same Environment must produce the same RID across restarts and across
// replicas, and a UUID would need storing to achieve that. The one Apollo
// locator whose format is documented -- a Plan's -- is a UUID, so this is a
// known divergence rather than an unconstrained choice.
// CONDUIT-INVENTED: apollo.RIDLocator
// Displaced-by: a published statement of Apollo's RID locator format, for any
// resource type Conduit holds
func EnvironmentRID(id string) string {
	if strings.HasPrefix(id, "ri.apollo.main.environment.") {
		return id
	}
	return "ri.apollo.main.environment." + stableSuffix(id)
}

// helmChartEntityID builds the Apollo entity id for an installed Helm chart.
//
// The four-segment shape is Apollo's, documented in @osdk/client.unstable as
// aeid:mojito:foundry-product:namespace+truck-locator and captured in
// docs/evidence/apollo-identifier-grammar.json. The third segment is a product
// type, which helm-chart is.
//
// The second segment carries the Environment, and that is Apollo's shape rather
// than Conduit's choice. The pinned OpenAPI publishes
// aeid:apollo-environment-demo:helm-chart:wordpress twice, beside environmentRid
// in the same object, so the second segment is an environment name and
// helm-chart is a product type Apollo accepts in the third.
//
// This was briefly registered as an invention on the reading that Apollo's
// example carried a Space there. It did not; that reading came from the OSDK
// example's 'mojito', which says nothing about what it is. The entry is recorded
// under "displaced" in docs/evidence/conduit-inventions.json.
//
// ApolloEntity.id still discloses valueTier: CONDUIT_NATIVE, because the string
// is composed here from Conduit's own names rather than answered by a Hub.
// Building a value to a published shape is still building it.
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
