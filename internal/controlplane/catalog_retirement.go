package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	CatalogRetirementProduct       = "PRODUCT"
	CatalogRetirementRelease       = "RELEASE"
	CatalogRetirementChangeTarget  = "CATALOG_RETIREMENT"
	ChangeTypeRetireProduct        = "RETIRE_PRODUCT"
	ChangeTypeRetireProductRelease = "RETIRE_PRODUCT_RELEASE"
)

type CatalogRetirementRepository interface {
	CreateCatalogRetirement(context.Context, CatalogRetirement) (CatalogRetirement, error)
	GetCatalogRetirement(context.Context, string, string) (CatalogRetirement, error)
	ListCatalogRetirements(context.Context, string) ([]CatalogRetirement, error)
}

// CatalogRetirement is an immutable tombstone. Product and Release bytes,
// prior desired states, Plans, reports, and execution evidence remain intact.
type CatalogRetirement struct {
	Scope           string    `json:"scope"`
	Product         string    `json:"product"`
	Version         string    `json:"version,omitempty"`
	Rationale       string    `json:"rationale"`
	RetiredBy       string    `json:"retiredBy"`
	ChangeRequestID string    `json:"changeRequestId"`
	RetiredAt       time.Time `json:"retiredAt"`
	Revision        uint64    `json:"revision"`
}

type CatalogRetirementService struct {
	repository Repository
	now        func() time.Time
}

func (repository *MemoryRepository) CreateCatalogRetirement(
	_ context.Context, retirement CatalogRetirement,
) (CatalogRetirement, error) {
	if err := validateCatalogRetirement(retirement, true); err != nil {
		return CatalogRetirement{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := catalogRetirementKey(retirement.Product, retirement.Version)
	if existing, exists := repository.catalogRetirements[key]; exists {
		return cloneJSON(existing), ErrConflict
	}
	plans := make([]PlanRecord, 0, len(repository.plans))
	for _, record := range repository.plans {
		plans = append(plans, clonePlanRecord(record))
	}
	reportsByEnvironment := make(map[string]EnvironmentReport, len(repository.reports))
	for environment, report := range repository.reports {
		reportsByEnvironment[environment] = cloneEnvironmentReport(report)
	}
	for _, catalog := range repository.importedCatalogs {
		if relayed, ok := relayedReportFromCatalog(catalog); ok {
			if local, exists := reportsByEnvironment[relayed.Environment]; !exists || relayed.ObservedAt.After(local.ObservedAt) {
				reportsByEnvironment[relayed.Environment] = relayed
			}
		}
	}
	reports := make([]EnvironmentReport, 0, len(reportsByEnvironment))
	for _, report := range reportsByEnvironment {
		reports = append(reports, report)
	}
	installations := make([]ModuleInstallation, 0, len(repository.moduleInstallations))
	for _, installation := range repository.moduleInstallations {
		installations = append(installations, cloneJSON(installation))
	}
	moduleReleases := make([]ModuleRelease, 0, len(repository.moduleReleases))
	for _, release := range repository.moduleReleases {
		moduleReleases = append(moduleReleases, cloneJSON(release))
	}
	if err := catalogRetirementPreconditions(retirement, plans, reports, installations, moduleReleases, slices.Collect(maps.Values(repository.productPublications))); err != nil {
		return CatalogRetirement{}, err
	}
	repository.catalogRetirements[key] = cloneJSON(retirement)
	return cloneJSON(retirement), nil
}

func (repository *MemoryRepository) GetCatalogRetirement(
	_ context.Context, product, version string,
) (CatalogRetirement, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	retirement, exists := repository.catalogRetirements[catalogRetirementKey(product, version)]
	if !exists {
		return CatalogRetirement{}, ErrNotFound
	}
	return cloneJSON(retirement), nil
}

func (repository *MemoryRepository) ListCatalogRetirements(
	_ context.Context, product string,
) ([]CatalogRetirement, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]CatalogRetirement, 0)
	for _, retirement := range repository.catalogRetirements {
		if product == "" || retirement.Product == product {
			result = append(result, cloneJSON(retirement))
		}
	}
	sortCatalogRetirements(result)
	return result, nil
}

func (repository *FileRepository) CreateCatalogRetirement(
	_ context.Context, retirement CatalogRetirement,
) (CatalogRetirement, error) {
	if err := validateCatalogRetirement(retirement, true); err != nil {
		return CatalogRetirement{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, err := repository.readCatalogRetirement(retirement.Product, retirement.Version); err == nil {
		return CatalogRetirement{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return CatalogRetirement{}, err
	}
	plans, err := repository.listPlansUnlocked("")
	if err != nil {
		return CatalogRetirement{}, err
	}
	reports, err := repository.listEnvironmentReportsUnlocked()
	if err != nil {
		return CatalogRetirement{}, err
	}
	installations, err := repository.listModuleInstallationsUnlocked("")
	if err != nil {
		return CatalogRetirement{}, err
	}
	moduleReleases, err := repository.listModuleReleasesUnlocked("")
	if err != nil {
		return CatalogRetirement{}, err
	}
	publications, err := repository.listProductPublicationsUnlocked("")
	if err != nil {
		return CatalogRetirement{}, err
	}
	if err := catalogRetirementPreconditions(retirement, plans, reports, installations, moduleReleases, publications); err != nil {
		return CatalogRetirement{}, err
	}
	if err := atomicWriteJSON(repository.catalogRetirementPath(retirement.Product, retirement.Version), retirement); err != nil {
		return CatalogRetirement{}, err
	}
	return retirement, nil
}

func (repository *FileRepository) GetCatalogRetirement(
	_ context.Context, product, version string,
) (CatalogRetirement, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readCatalogRetirement(product, version)
}

func (repository *FileRepository) ListCatalogRetirements(
	_ context.Context, product string,
) ([]CatalogRetirement, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listCatalogRetirementsUnlocked(product)
}

func sortCatalogRetirements(retirements []CatalogRetirement) {
	sort.Slice(retirements, func(i, j int) bool {
		if retirements[i].Product != retirements[j].Product {
			return retirements[i].Product < retirements[j].Product
		}
		return retirements[i].Version < retirements[j].Version
	})
}

func catalogRetirementIndex(retirements []CatalogRetirement) map[string]CatalogRetirement {
	result := make(map[string]CatalogRetirement, len(retirements))
	for _, retirement := range retirements {
		result[catalogRetirementKey(retirement.Product, retirement.Version)] = retirement
	}
	return result
}

func (repository *FileRepository) readCatalogRetirement(product, version string) (CatalogRetirement, error) {
	if !coordinatePattern.MatchString(product) || (version != "" && !versionPattern.MatchString(version)) {
		return CatalogRetirement{}, ErrNotFound
	}
	var retirement CatalogRetirement
	if err := readJSON(repository.catalogRetirementPath(product, version), &retirement); err != nil {
		return CatalogRetirement{}, err
	}
	if retirement.Product != product || retirement.Version != version || validateCatalogRetirement(retirement, true) != nil {
		return CatalogRetirement{}, ErrConflict
	}
	return retirement, nil
}

func (repository *FileRepository) listCatalogRetirementsUnlocked(product string) ([]CatalogRetirement, error) {
	if product != "" && !coordinatePattern.MatchString(product) {
		return nil, ErrNotFound
	}
	entries, err := os.ReadDir(repository.catalogRetirementDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]CatalogRetirement, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var retirement CatalogRetirement
		if err := readJSON(filepath.Join(repository.catalogRetirementDirectory(), entry.Name()), &retirement); err != nil {
			return nil, err
		}
		if err := validateCatalogRetirement(retirement, true); err != nil {
			return nil, err
		}
		if product == "" || retirement.Product == product {
			result = append(result, retirement)
		}
	}
	sortCatalogRetirements(result)
	return result, nil
}

func NewCatalogRetirementService(repository Repository) *CatalogRetirementService {
	return &CatalogRetirementService{repository: repository, now: time.Now}
}

func (service *CatalogRetirementService) Prepare(
	ctx context.Context, scope, product, version, subject, rationale string,
) (CatalogRetirement, error) {
	retirement := CatalogRetirement{
		Scope: strings.TrimSpace(scope), Product: strings.TrimSpace(product), Version: strings.TrimSpace(version),
		RetiredBy: strings.TrimSpace(subject), Rationale: strings.TrimSpace(rationale),
		RetiredAt: service.now().UTC(), Revision: 1,
	}
	if err := validateCatalogRetirement(retirement, false); err != nil {
		return CatalogRetirement{}, err
	}
	if _, err := service.repository.GetCatalogRetirement(ctx, retirement.Product, retirement.Version); err == nil {
		return CatalogRetirement{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return CatalogRetirement{}, err
	}
	if err := validateCatalogRetirementPreconditions(ctx, service.repository, retirement); err != nil {
		return CatalogRetirement{}, err
	}
	return retirement, nil
}

func (service *CatalogRetirementService) ApplyApprovedChangeRequest(
	ctx context.Context, request ChangeRequest,
) (CatalogRetirement, ChangeRequest, error) {
	if request.TargetKind != CatalogRetirementChangeTarget || request.Status != ChangeApproved || len(request.Changes) != 1 ||
		request.Changes[0].Request["retirement"] == nil {
		return CatalogRetirement{}, request, ErrInvalidTransition
	}
	change := request.Changes[0]
	if change.Type != ChangeTypeRetireProduct && change.Type != ChangeTypeRetireProductRelease {
		return CatalogRetirement{}, request, ErrInvalidTransition
	}
	if request.Execution != nil && request.Execution.Status == ExecutionSucceeded {
		encoded, _ := json.Marshal(change.Request["retirement"])
		var retirement CatalogRetirement
		_ = json.Unmarshal(encoded, &retirement)
		stored, err := service.repository.GetCatalogRetirement(ctx, retirement.Product, retirement.Version)
		return stored, request, err
	}
	encoded, err := json.Marshal(change.Request["retirement"])
	if err != nil {
		return CatalogRetirement{}, request, ErrConflict
	}
	var retirement CatalogRetirement
	if err := json.Unmarshal(encoded, &retirement); err != nil {
		return CatalogRetirement{}, request, ErrConflict
	}
	retirement.ChangeRequestID = request.ID
	if err := validateCatalogRetirement(retirement, true); err != nil {
		return CatalogRetirement{}, request, err
	}
	stored, err := service.repository.CreateCatalogRetirement(ctx, retirement)
	if errors.Is(err, ErrConflict) {
		if existing, getErr := service.repository.GetCatalogRetirement(ctx, retirement.Product, retirement.Version); getErr == nil &&
			catalogRetirementsEqual(existing, retirement) {
			stored, err = existing, nil
		}
	}
	now := service.now().UTC()
	if err != nil {
		failed, updateErr := service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
			record.Execution = &ChangeExecution{
				Status: ExecutionFailed, StartedAt: now, CompletedAt: now,
				Items: []ChangeExecutionItem{{
					ChangeID: change.ID, ResourceRID: catalogRetirementRID(retirement),
					Status: ExecutionFailed, Error: errorReason(err),
				}},
			}
			record.UpdatedAt = now
			return nil
		})
		if updateErr == nil {
			request = failed
		}
		return CatalogRetirement{}, request, err
	}
	request, err = service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
		if record.Status != ChangeApproved {
			return ErrInvalidTransition
		}
		if record.Execution != nil && record.Execution.Status == ExecutionSucceeded {
			return nil
		}
		record.Execution = &ChangeExecution{
			Status: ExecutionSucceeded, StartedAt: now, CompletedAt: now,
			Items: []ChangeExecutionItem{{
				ChangeID: change.ID, ResourceRID: catalogRetirementRID(retirement),
				Status: ExecutionSucceeded, ResultID: catalogRetirementRID(retirement),
			}},
		}
		record.UpdatedAt = now
		return nil
	})
	return stored, request, err
}

func validateCatalogRetirement(retirement CatalogRetirement, requireChangeRequest bool) error {
	if !coordinatePattern.MatchString(retirement.Product) || strings.TrimSpace(retirement.Rationale) == "" ||
		strings.TrimSpace(retirement.RetiredBy) == "" || retirement.RetiredAt.IsZero() || retirement.Revision != 1 {
		return fmt.Errorf("%w: invalid catalog retirement", ErrConflict)
	}
	switch retirement.Scope {
	case CatalogRetirementProduct:
		if retirement.Version != "" {
			return fmt.Errorf("%w: Product retirement cannot name a version", ErrConflict)
		}
	case CatalogRetirementRelease:
		if !versionPattern.MatchString(retirement.Version) {
			return fmt.Errorf("%w: Release retirement requires an exact version", ErrConflict)
		}
	default:
		return fmt.Errorf("%w: retirement scope must be PRODUCT or RELEASE", ErrConflict)
	}
	if requireChangeRequest && strings.TrimSpace(retirement.ChangeRequestID) == "" {
		return fmt.Errorf("%w: catalog retirement requires Change Request evidence", ErrConflict)
	}
	return nil
}

func catalogRetirementRID(retirement CatalogRetirement) string {
	if retirement.Scope == CatalogRetirementProduct {
		return productRID(retirement.Product)
	}
	return "release/" + retirement.Product + "/" + retirement.Version
}

// A Maven-style coordinate is unambiguous for the validated Product/version
// grammar. NUL-separated map keys cannot be stored in PostgreSQL JSONB. File
// record names retain their existing independently hashed path identity.
func catalogRetirementKey(product, version string) string { return product + ":" + version }

func catalogRetirementBlocks(retirements map[string]CatalogRetirement, product, version string) bool {
	_, productRetired := retirements[catalogRetirementKey(product, "")]
	_, releaseRetired := retirements[catalogRetirementKey(product, version)]
	return productRetired || releaseRetired
}

func planTargetsRetiredCatalog(record PlanRecord, retirements map[string]CatalogRetirement) bool {
	if record.DesiredState != nil {
		for _, release := range record.DesiredState.Releases {
			if catalogRetirementBlocks(retirements, release.Product, release.Version) {
				return true
			}
			for _, dependency := range release.Dependencies {
				if _, retired := retirements[catalogRetirementKey(dependency.Product, "")]; retired {
					return true
				}
				for _, retirement := range retirements {
					if retirement.Scope == CatalogRetirementRelease && retirement.Product == dependency.Product &&
						versionWithinDependency(retirement.Version, dependency) {
						return true
					}
				}
			}
		}
	}
	for _, step := range record.Plan.Steps {
		if step.Action != planActionUninstall && catalogRetirementBlocks(retirements, step.Product, step.Version) {
			return true
		}
	}
	return false
}

func moduleEntitiesTargetRetiredProduct(entities []LockedModuleEntity, retirements map[string]CatalogRetirement) bool {
	for _, entity := range entities {
		if _, retired := retirements[catalogRetirementKey(entity.ProductID, "")]; retired {
			return true
		}
	}
	return false
}

func moduleInstallationTargetsRetiredProduct(installation ModuleInstallation, retirements map[string]CatalogRetirement) bool {
	for _, entity := range installation.Entities {
		if _, retired := retirements[catalogRetirementKey(entity.ProductID, "")]; retired {
			return true
		}
	}
	return false
}

func catalogRetirementsEqual(left, right CatalogRetirement) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func validateCatalogRetirementPreconditions(
	ctx context.Context, repository Repository, retirement CatalogRetirement,
) error {
	plans, err := repository.ListPlans(ctx, "")
	if err != nil {
		return err
	}
	reports, err := repository.ListEnvironmentReports(ctx)
	if err != nil {
		return err
	}
	installations, err := repository.ListModuleInstallations(ctx, "")
	if err != nil {
		return err
	}
	moduleReleases, err := repository.ListModuleReleases(ctx, "")
	if err != nil {
		return err
	}
	publications, err := repository.ListProductPublications(ctx, "")
	if err != nil {
		return err
	}
	return catalogRetirementPreconditions(retirement, plans, reports, installations, moduleReleases, publications)
}

func catalogRetirementPreconditions(
	retirement CatalogRetirement, plans []PlanRecord, reports []EnvironmentReport,
	installations []ModuleInstallation, moduleReleases []ModuleRelease, publications []ProductPublication,
) error {
	found := catalog.HasPublication(publications, retirement.Product, retirement.Version)
	latest := latestPlansByEnvironment(plans)
	for _, record := range plans {
		if record.DesiredState == nil {
			continue
		}
		for _, release := range record.DesiredState.Releases {
			if release.Product == retirement.Product && (retirement.Scope == CatalogRetirementProduct || release.Version == retirement.Version) {
				found = true
			}
		}
	}
	for _, record := range latest {
		for _, step := range record.Plan.Steps {
			if step.Action != planActionUninstall && step.Product == retirement.Product &&
				(retirement.Scope == CatalogRetirementProduct || step.Version == retirement.Version) {
				return fmt.Errorf("%w: Entity %s in Environment %s still targets %s:%s", ErrConflict,
					step.InstallationID, record.Plan.Environment, retirement.Product, step.Version)
			}
		}
		if record.DesiredState != nil {
			for _, release := range record.DesiredState.Releases {
				for _, dependency := range release.Dependencies {
					if dependency.Product == retirement.Product &&
						(retirement.Scope == CatalogRetirementProduct || versionWithinDependency(retirement.Version, dependency)) {
						return fmt.Errorf("%w: Product Release %s:%s still depends on retired coordinate", ErrConflict, release.Product, release.Version)
					}
				}
			}
		}
	}
	for _, report := range reports {
		for _, entity := range report.Entities {
			if entity.Lifecycle != LifecycleUninstalled && entity.Product == retirement.Product &&
				(retirement.Scope == CatalogRetirementProduct || entity.Version == retirement.Version) {
				return fmt.Errorf("%w: reported Entity %s in Environment %s still runs retired coordinate", ErrConflict, entity.InstallationID, report.Environment)
			}
		}
	}
	if retirement.Scope == CatalogRetirementProduct {
		for _, installation := range installations {
			for _, entity := range installation.Entities {
				if entity.ProductID == retirement.Product {
					return fmt.Errorf("%w: Module installation %s still manages Product", ErrConflict, installation.ID)
				}
			}
		}
		latestModules := latestModuleReleases(moduleReleases)
		for _, release := range latestModules {
			for _, entity := range release.Lock.Entities {
				if entity.ProductID == retirement.Product {
					return fmt.Errorf("%w: latest Module Release %s:%s still references Product", ErrConflict, release.Name, release.Version)
				}
			}
		}
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func latestPlansByEnvironment(plans []PlanRecord) map[string]PlanRecord {
	result := map[string]PlanRecord{}
	for _, plan := range plans {
		current, exists := result[plan.Plan.Environment]
		if !exists || plan.CreatedAt.After(current.CreatedAt) ||
			(plan.CreatedAt.Equal(current.CreatedAt) && plan.ID > current.ID) {
			result[plan.Plan.Environment] = plan
		}
	}
	return result
}

func latestModuleReleases(releases []ModuleRelease) []ModuleRelease {
	latest := map[string]ModuleRelease{}
	for _, release := range releases {
		if current, exists := latest[release.Name]; !exists || compareVersions(release.Version, current.Version) > 0 {
			latest[release.Name] = release
		}
	}
	result := make([]ModuleRelease, 0, len(latest))
	for _, release := range latest {
		result = append(result, release)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func versionWithinDependency(version string, dependency Dependency) bool {
	return compareVersions(version, dependency.MinimumVersion) >= 0 && versionWithinMaximum(version, dependency.MaximumVersion)
}
