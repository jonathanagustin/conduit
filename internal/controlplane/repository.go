package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FileRepository struct {
	root string
	mu   sync.Mutex
}

type MemoryRepository struct {
	mu                          sync.Mutex
	plans                       map[string]PlanRecord
	reports                     map[string]EnvironmentReport
	apolloAgentReports          map[string]persistedApolloAgentReports
	suppressions                map[string]SuppressionRecord
	promotions                  map[string]PromotionRecord
	promotionStageControls      map[string]PromotionStageControl
	moduleReleases              map[string]ModuleRelease
	moduleInstallations         map[string]ModuleInstallation
	moduleAutoUpdatePolicies    map[string]ModuleAutoUpdatePolicy
	blobs                       map[string]memoryBlob
	exportPipelines             map[string]ExportPipeline
	exportPipelineRevisions     map[string]map[uint64]ExportPipeline
	exportPipelineScheduleRuns  map[string]ExportPipelineScheduleRun
	environmentProfiles         map[string]EnvironmentProfile
	environmentProfileRevisions map[string]map[uint64]EnvironmentProfile
	catalogRetirements          map[string]CatalogRetirement
	labelDefinitions            map[string]LabelDefinition
	labelDefinitionRevisions    map[string]map[uint64]LabelDefinition
	bundles                     map[string]BundleEnvelope
	bundleImports               map[string]BundleImport
	bundleTransfers             map[string]BundleTransferConfirmation
	importedCatalogs            map[string]ImportedCatalog
	teams                       map[string]Team
	resourceGrants              map[string]ResourceGrant
	approvalPolicies            map[string]ApprovalPolicy
	changeRequests              map[string]ChangeRequest
	authorizationAudit          map[string]AuthorizationAuditEvent
	productPublications         map[string]ProductPublication
}

type memoryBlob struct {
	descriptor BlobDescriptor
	contents   []byte
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		plans: make(map[string]PlanRecord), reports: make(map[string]EnvironmentReport),
		apolloAgentReports:          make(map[string]persistedApolloAgentReports),
		suppressions:                make(map[string]SuppressionRecord),
		promotions:                  make(map[string]PromotionRecord),
		promotionStageControls:      make(map[string]PromotionStageControl),
		moduleReleases:              make(map[string]ModuleRelease),
		moduleInstallations:         make(map[string]ModuleInstallation),
		moduleAutoUpdatePolicies:    make(map[string]ModuleAutoUpdatePolicy),
		blobs:                       make(map[string]memoryBlob),
		exportPipelines:             make(map[string]ExportPipeline),
		exportPipelineRevisions:     make(map[string]map[uint64]ExportPipeline),
		exportPipelineScheduleRuns:  make(map[string]ExportPipelineScheduleRun),
		environmentProfiles:         make(map[string]EnvironmentProfile),
		environmentProfileRevisions: make(map[string]map[uint64]EnvironmentProfile),
		catalogRetirements:          make(map[string]CatalogRetirement),
		labelDefinitions:            make(map[string]LabelDefinition),
		labelDefinitionRevisions:    make(map[string]map[uint64]LabelDefinition),
		bundles:                     make(map[string]BundleEnvelope),
		bundleImports:               make(map[string]BundleImport),
		bundleTransfers:             make(map[string]BundleTransferConfirmation),
		importedCatalogs:            make(map[string]ImportedCatalog),
		teams:                       make(map[string]Team),
		resourceGrants:              make(map[string]ResourceGrant),
		approvalPolicies:            make(map[string]ApprovalPolicy),
		changeRequests:              make(map[string]ChangeRequest),
		authorizationAudit:          make(map[string]AuthorizationAuditEvent),
		productPublications:         make(map[string]ProductPublication),
	}
}

func (repository *MemoryRepository) PutExportPipeline(
	_ context.Context, pipeline ExportPipeline,
) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := validatePipelineLabelsFromDefinitions(pipeline, repository.labelDefinitions); err != nil {
		return ExportPipeline{}, err
	}
	if existing, exists := repository.exportPipelines[pipeline.ID]; exists {
		if pipeline.Revision != existing.Revision+1 {
			return ExportPipeline{}, ErrConflict
		}
	} else if pipeline.Revision != 1 {
		return ExportPipeline{}, ErrConflict
	}
	repository.exportPipelines[pipeline.ID] = cloneJSON(pipeline)
	if repository.exportPipelineRevisions[pipeline.ID] == nil {
		repository.exportPipelineRevisions[pipeline.ID] = make(map[uint64]ExportPipeline)
	}
	repository.exportPipelineRevisions[pipeline.ID][pipeline.Revision] = cloneJSON(pipeline)
	return cloneJSON(pipeline), nil
}

func (repository *MemoryRepository) PutEnvironmentProfile(
	_ context.Context, profile EnvironmentProfile,
) (EnvironmentProfile, error) {
	if err := validateEnvironmentProfile(profile); err != nil {
		return EnvironmentProfile{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.environmentProfiles[profile.EnvironmentID]; exists {
		if profile.Revision != existing.Revision+1 {
			return EnvironmentProfile{}, ErrConflict
		}
	} else if profile.Revision != 1 {
		return EnvironmentProfile{}, ErrConflict
	}
	repository.environmentProfiles[profile.EnvironmentID] = cloneJSON(profile)
	if repository.environmentProfileRevisions[profile.EnvironmentID] == nil {
		repository.environmentProfileRevisions[profile.EnvironmentID] = make(map[uint64]EnvironmentProfile)
	}
	repository.environmentProfileRevisions[profile.EnvironmentID][profile.Revision] = cloneJSON(profile)
	return cloneJSON(profile), nil
}

func (repository *MemoryRepository) GetEnvironmentProfile(
	_ context.Context, environment string,
) (EnvironmentProfile, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	profile, exists := repository.environmentProfiles[environment]
	if !exists {
		return EnvironmentProfile{}, ErrNotFound
	}
	return cloneJSON(profile), nil
}

func (repository *MemoryRepository) ListEnvironmentProfileRevisions(
	_ context.Context, environment string,
) ([]EnvironmentProfile, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	revisions := repository.environmentProfileRevisions[environment]
	if len(revisions) == 0 {
		return nil, ErrNotFound
	}
	result := make([]EnvironmentProfile, 0, len(revisions))
	for _, profile := range revisions {
		result = append(result, cloneJSON(profile))
	}
	sortedEnvironmentProfiles(result)
	return result, nil
}

func (repository *MemoryRepository) GetExportPipeline(_ context.Context, id string) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	pipeline, exists := repository.exportPipelines[id]
	if !exists {
		return ExportPipeline{}, ErrNotFound
	}
	return cloneJSON(pipeline), nil
}

func (repository *MemoryRepository) GetExportPipelineRevision(_ context.Context, id string, revision uint64) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	pipeline, exists := repository.exportPipelineRevisions[id][revision]
	if !exists {
		return ExportPipeline{}, ErrNotFound
	}
	return cloneJSON(pipeline), nil
}

func (repository *MemoryRepository) ListExportPipelines(_ context.Context) ([]ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ExportPipeline, 0, len(repository.exportPipelines))
	for _, pipeline := range repository.exportPipelines {
		result = append(result, cloneJSON(pipeline))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func exportPipelineScheduleRunID(pipelineID string, scheduledFor time.Time) string {
	return pipelineID + "-" + scheduledFor.UTC().Format("20060102T1504Z")
}

func (repository *MemoryRepository) ClaimExportPipelineScheduleRun(
	_ context.Context, run ExportPipelineScheduleRun,
) (ExportPipelineScheduleRun, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	id := exportPipelineScheduleRunID(run.PipelineID, run.ScheduledFor)
	if existing, exists := repository.exportPipelineScheduleRuns[id]; exists {
		if err := validateExportPipelineScheduleRun(existing); err != nil {
			return ExportPipelineScheduleRun{}, false, err
		}
		return cloneJSON(existing), false, nil
	}
	run.Revision = 1
	if err := validateExportPipelineScheduleRun(run); err != nil {
		return ExportPipelineScheduleRun{}, false, err
	}
	repository.exportPipelineScheduleRuns[id] = cloneJSON(run)
	return cloneJSON(run), true, nil
}

func (repository *MemoryRepository) CompleteExportPipelineScheduleRun(
	_ context.Context, pipelineID string, scheduledFor time.Time, update func(*ExportPipelineScheduleRun) error,
) (ExportPipelineScheduleRun, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	id := exportPipelineScheduleRunID(pipelineID, scheduledFor)
	run, exists := repository.exportPipelineScheduleRuns[id]
	if !exists {
		return ExportPipelineScheduleRun{}, ErrNotFound
	}
	if err := validateExportPipelineScheduleRun(run); err != nil || run.Status != ExportScheduleRunning {
		return ExportPipelineScheduleRun{}, ErrConflict
	}
	working := cloneJSON(run)
	if err := update(&working); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	working.Revision++
	if working.PipelineID != run.PipelineID || !working.ScheduledFor.Equal(run.ScheduledFor) ||
		working.PipelineRevision != run.PipelineRevision {
		return ExportPipelineScheduleRun{}, ErrConflict
	}
	if err := validateExportPipelineScheduleRun(working); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	repository.exportPipelineScheduleRuns[id] = cloneJSON(working)
	return cloneJSON(working), nil
}

func (repository *MemoryRepository) ListExportPipelineScheduleRuns(
	_ context.Context, pipelineID string,
) ([]ExportPipelineScheduleRun, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ExportPipelineScheduleRun, 0)
	for _, run := range repository.exportPipelineScheduleRuns {
		if err := validateExportPipelineScheduleRun(run); err != nil {
			return nil, err
		}
		if pipelineID == "" || run.PipelineID == pipelineID {
			result = append(result, cloneJSON(run))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ScheduledFor.Before(result[j].ScheduledFor) })
	return result, nil
}

func (repository *MemoryRepository) CreateBundle(_ context.Context, bundle BundleEnvelope) (BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.bundles[bundle.Manifest.ID]; exists {
		if existing.ManifestDigest == bundle.ManifestDigest {
			return cloneJSON(existing), nil
		}
		return BundleEnvelope{}, ErrConflict
	}
	repository.bundles[bundle.Manifest.ID] = cloneJSON(bundle)
	return cloneJSON(bundle), nil
}

func (repository *MemoryRepository) GetBundle(_ context.Context, id string) (BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	bundle, exists := repository.bundles[id]
	if !exists {
		return BundleEnvelope{}, ErrNotFound
	}
	return cloneJSON(bundle), nil
}

func (repository *MemoryRepository) ListBundles(_ context.Context, pipelineID string) ([]BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]BundleEnvelope, 0, len(repository.bundles))
	for _, bundle := range repository.bundles {
		if pipelineID == "" || bundle.Manifest.PipelineID == pipelineID {
			result = append(result, cloneJSON(bundle))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Manifest.Sequence != result[j].Manifest.Sequence {
			return result[i].Manifest.Sequence < result[j].Manifest.Sequence
		}
		return result[i].Manifest.ID < result[j].Manifest.ID
	})
	return result, nil
}

func (repository *MemoryRepository) PutBundleTransferConfirmation(
	_ context.Context, confirmation BundleTransferConfirmation,
) (BundleTransferConfirmation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.bundleTransfers[confirmation.ID]; exists {
		if confirmation.Revision != existing.Revision+1 {
			return BundleTransferConfirmation{}, ErrConflict
		}
	} else if confirmation.Revision != 1 {
		return BundleTransferConfirmation{}, ErrConflict
	}
	repository.bundleTransfers[confirmation.ID] = cloneJSON(confirmation)
	return cloneJSON(confirmation), nil
}

func (repository *MemoryRepository) ListBundleTransferConfirmations(
	_ context.Context, pipelineID, targetHub string,
) ([]BundleTransferConfirmation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]BundleTransferConfirmation, 0)
	for _, confirmation := range repository.bundleTransfers {
		if (pipelineID == "" || confirmation.PipelineID == pipelineID) &&
			(targetHub == "" || confirmation.TargetHub == targetHub) {
			result = append(result, cloneJSON(confirmation))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *MemoryRepository) CreateBundleImport(_ context.Context, record BundleImport) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.bundleImports[record.ID]; exists {
		if existing.ManifestDigest == record.ManifestDigest {
			return cloneJSON(existing), nil
		}
		return BundleImport{}, ErrConflict
	}
	repository.bundleImports[record.ID] = cloneJSON(record)
	return cloneJSON(record), nil
}

func (repository *MemoryRepository) GetBundleImport(_ context.Context, id string) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.bundleImports[id]
	if !exists {
		return BundleImport{}, ErrNotFound
	}
	return cloneJSON(record), nil
}

func (repository *MemoryRepository) ListBundleImports(_ context.Context) ([]BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]BundleImport, 0, len(repository.bundleImports))
	for _, record := range repository.bundleImports {
		result = append(result, cloneJSON(record))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *MemoryRepository) UpdateBundleImport(
	_ context.Context, id string, update func(*BundleImport) error,
) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.bundleImports[id]
	if !exists {
		return BundleImport{}, ErrNotFound
	}
	record = cloneJSON(record)
	if err := update(&record); err != nil {
		return BundleImport{}, err
	}
	record.Revision++
	repository.bundleImports[id] = cloneJSON(record)
	return cloneJSON(record), nil
}

func (repository *MemoryRepository) PutImportedCatalog(_ context.Context, catalog ImportedCatalog) (ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if current, exists := repository.importedCatalogs[catalog.Environment]; exists {
		if catalog.Revision != current.Revision+1 {
			return ImportedCatalog{}, ErrConflict
		}
	} else if catalog.Revision != 1 {
		return ImportedCatalog{}, ErrConflict
	}
	repository.importedCatalogs[catalog.Environment] = cloneJSON(catalog)
	return cloneJSON(catalog), nil
}

func (repository *MemoryRepository) GetImportedCatalog(_ context.Context, environment string) (ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	catalog, exists := repository.importedCatalogs[environment]
	if !exists {
		return ImportedCatalog{}, ErrNotFound
	}
	return cloneJSON(catalog), nil
}

func (repository *MemoryRepository) ListImportedCatalogs(_ context.Context) ([]ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ImportedCatalog, 0, len(repository.importedCatalogs))
	for _, catalog := range repository.importedCatalogs {
		result = append(result, cloneJSON(catalog))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Environment < result[j].Environment })
	return result, nil
}

func (repository *MemoryRepository) PutBlob(
	ctx context.Context, expectedDigest, mediaType string, source io.Reader,
) (BlobDescriptor, bool, error) {
	if err := validateExpectedDigest(expectedDigest); err != nil {
		return BlobDescriptor{}, false, err
	}
	contents, err := io.ReadAll(contextReader{ctx: ctx, reader: source})
	if err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("read artifact: %w", err)
	}
	sum := sha256.Sum256(contents)
	digest := sha256Digest(sum)
	if expectedDigest != "" && expectedDigest != digest {
		return BlobDescriptor{}, false, fmt.Errorf("%w: expected %s, computed %s", ErrDigestMismatch, expectedDigest, digest)
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, found := repository.blobs[digest]; found {
		descriptor := existing.descriptor
		descriptor.MediaType = normalizeMediaType(mediaType)
		return descriptor, false, nil
	}
	descriptor := BlobDescriptor{
		MediaType: normalizeMediaType(mediaType), Digest: digest, Size: int64(len(contents)), CreatedAt: time.Now().UTC(),
	}
	repository.blobs[digest] = memoryBlob{descriptor: descriptor, contents: append([]byte(nil), contents...)}
	return descriptor, true, nil
}

func (repository *MemoryRepository) OpenBlob(
	_ context.Context, digest string,
) (BlobDescriptor, io.ReadCloser, error) {
	if !digestPattern.MatchString(digest) {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	blob, found := repository.blobs[digest]
	if !found {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	return blob.descriptor, io.NopCloser(bytes.NewReader(append([]byte(nil), blob.contents...))), nil
}

func (repository *MemoryRepository) CreateModuleRelease(
	_ context.Context, release ModuleRelease,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if moduleEntitiesTargetRetiredProduct(release.Lock.Entities, repository.catalogRetirements) {
		return ModuleRelease{}, ErrConflict
	}
	key := release.Name + ":" + release.Version
	if existing, exists := repository.moduleReleases[key]; exists {
		if existing.Digest == release.Digest {
			return cloneJSON(existing), nil
		}
		return ModuleRelease{}, ErrConflict
	}
	repository.moduleReleases[key] = cloneJSON(release)
	return cloneJSON(release), nil
}

func (repository *MemoryRepository) GetModuleRelease(
	_ context.Context, name, version string,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	release, exists := repository.moduleReleases[name+":"+version]
	if !exists {
		return ModuleRelease{}, ErrNotFound
	}
	return cloneJSON(release), nil
}

func (repository *MemoryRepository) ListModuleReleases(
	_ context.Context, name string,
) ([]ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	releases := make([]ModuleRelease, 0)
	for _, release := range repository.moduleReleases {
		if name == "" || release.Name == name {
			releases = append(releases, cloneJSON(release))
		}
	}
	sortModuleReleases(releases)
	return releases, nil
}

func (repository *MemoryRepository) UpdateModuleRelease(
	_ context.Context, name, version string, update func(*ModuleRelease) error,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := name + ":" + version
	release, exists := repository.moduleReleases[key]
	if !exists {
		return ModuleRelease{}, ErrNotFound
	}
	working := cloneJSON(release)
	if err := update(&working); err != nil {
		return ModuleRelease{}, err
	}
	// The content-addressed definition, lock, digest, version, and publication
	// time remain immutable; only channel membership and recall state may change.
	if working.Name != release.Name || working.Version != release.Version || working.Digest != release.Digest ||
		!working.CreatedAt.Equal(release.CreatedAt) ||
		moduleReleaseDigest(working.Definition, working.Lock) != release.Digest {
		return ModuleRelease{}, ErrConflict
	}
	repository.moduleReleases[key] = cloneJSON(working)
	return cloneJSON(working), nil
}

func (repository *MemoryRepository) CreateModuleInstallation(
	_ context.Context, installation ModuleInstallation,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if moduleInstallationTargetsRetiredProduct(installation, repository.catalogRetirements) {
		return ModuleInstallation{}, ErrConflict
	}
	if existing, exists := repository.moduleInstallations[installation.ID]; exists {
		return cloneJSON(existing), ErrConflict
	}
	repository.moduleInstallations[installation.ID] = cloneJSON(installation)
	return cloneJSON(installation), nil
}

func (repository *MemoryRepository) GetModuleInstallation(
	_ context.Context, id string,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	installation, exists := repository.moduleInstallations[id]
	if !exists {
		return ModuleInstallation{}, ErrNotFound
	}
	return cloneJSON(installation), nil
}

func (repository *MemoryRepository) ListModuleInstallations(
	_ context.Context, environment string,
) ([]ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	installations := make([]ModuleInstallation, 0)
	for _, installation := range repository.moduleInstallations {
		if environment == "" || installation.Environment == environment {
			installations = append(installations, cloneJSON(installation))
		}
	}
	sort.Slice(installations, func(i, j int) bool { return installations[i].ID < installations[j].ID })
	return installations, nil
}

func (repository *MemoryRepository) UpdateModuleInstallation(
	_ context.Context, id string, update func(*ModuleInstallation) error,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	installation, exists := repository.moduleInstallations[id]
	if !exists {
		return ModuleInstallation{}, ErrNotFound
	}
	working := cloneJSON(installation)
	if err := update(&working); err != nil {
		return ModuleInstallation{}, err
	}
	if moduleInstallationTargetsRetiredProduct(working, repository.catalogRetirements) {
		return ModuleInstallation{}, ErrConflict
	}
	working.Revision++
	repository.moduleInstallations[id] = cloneJSON(working)
	return cloneJSON(working), nil
}

func (repository *MemoryRepository) UpdateModuleInstallationAutomatically(
	_ context.Context, id, module string, policyRevision uint64, update func(*ModuleInstallation) error,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	policy, exists := repository.moduleAutoUpdatePolicies[module]
	if !exists || !policy.Enabled || policy.Revision != policyRevision {
		return ModuleInstallation{}, ErrConflict
	}
	installation, exists := repository.moduleInstallations[id]
	if !exists {
		return ModuleInstallation{}, ErrNotFound
	}
	if installation.Module != module {
		return ModuleInstallation{}, ErrConflict
	}
	working := cloneJSON(installation)
	if err := update(&working); err != nil {
		return ModuleInstallation{}, err
	}
	if moduleInstallationTargetsRetiredProduct(working, repository.catalogRetirements) {
		return ModuleInstallation{}, ErrConflict
	}
	working.Revision++
	repository.moduleInstallations[id] = cloneJSON(working)
	return cloneJSON(working), nil
}

func (repository *MemoryRepository) PutModuleAutoUpdatePolicy(
	_ context.Context, policy ModuleAutoUpdatePolicy,
) (ModuleAutoUpdatePolicy, error) {
	if err := validateModuleAutoUpdatePolicy(policy); err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	existing, exists := repository.moduleAutoUpdatePolicies[policy.Module]
	if (exists && policy.Revision != existing.Revision+1) || (!exists && policy.Revision != 1) {
		return ModuleAutoUpdatePolicy{}, ErrConflict
	}
	repository.moduleAutoUpdatePolicies[policy.Module] = cloneJSON(policy)
	return cloneJSON(policy), nil
}

func (repository *MemoryRepository) GetModuleAutoUpdatePolicy(
	_ context.Context, module string,
) (ModuleAutoUpdatePolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	policy, exists := repository.moduleAutoUpdatePolicies[module]
	if !exists {
		return ModuleAutoUpdatePolicy{}, ErrNotFound
	}
	return cloneJSON(policy), nil
}

func cloneJSON[T any](value T) T {
	contents, _ := json.Marshal(value)
	var clone T
	_ = json.Unmarshal(contents, &clone)
	return clone
}

func (repository *MemoryRepository) CreatePromotionUnlessStagePaused(
	_ context.Context, record PromotionRecord,
) (PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	control := repository.promotionStageControls[promotionStageControlID(record.Product, record.FromChannel, record.ToChannel)]
	if control.Status == PromotionStagePaused {
		return PromotionRecord{}, promotionStagePausedError(control)
	}
	if existing, exists := repository.promotions[record.ID]; exists {
		return existing, nil
	}
	repository.promotions[record.ID] = record
	return record, nil
}

func (repository *MemoryRepository) GetPromotionStageControl(
	_ context.Context, product, fromChannel, toChannel string,
) (PromotionStageControl, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	control, exists := repository.promotionStageControls[promotionStageControlID(product, fromChannel, toChannel)]
	if !exists {
		control = defaultPromotionStageControl(product, fromChannel, toChannel)
	}
	return control, nil
}

func (repository *MemoryRepository) UpdatePromotionStageControl(
	_ context.Context, product, fromChannel, toChannel string, update func(*PromotionStageControl) error,
) (PromotionStageControl, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	id := promotionStageControlID(product, fromChannel, toChannel)
	control, exists := repository.promotionStageControls[id]
	if !exists {
		control = defaultPromotionStageControl(product, fromChannel, toChannel)
	}
	previous := control
	if err := update(&control); err != nil {
		return PromotionStageControl{}, err
	}
	if control.ID != id || control.Product != product || control.FromChannel != fromChannel || control.ToChannel != toChannel ||
		(control.Status != PromotionStageActive && control.Status != PromotionStagePaused) {
		return PromotionStageControl{}, ErrConflict
	}
	if control == previous {
		return control, nil
	}
	control.Revision = previous.Revision + 1
	repository.promotionStageControls[id] = control
	return control, nil
}

func (repository *MemoryRepository) GetPromotion(_ context.Context, id string) (PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.promotions[id]
	if !exists {
		return PromotionRecord{}, ErrNotFound
	}
	return record, nil
}

func (repository *MemoryRepository) ListPromotions(
	_ context.Context, product string,
) ([]PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records := make([]PromotionRecord, 0, len(repository.promotions))
	for _, record := range repository.promotions {
		if product == "" || record.Product == product {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (repository *MemoryRepository) CreateSuppression(
	_ context.Context, record SuppressionRecord,
) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.suppressions[record.ID]; exists {
		if existing.Environment == record.Environment && existing.PlanID == record.PlanID && existing.Source == record.Source {
			return existing, nil
		}
		return SuppressionRecord{}, ErrConflict
	}
	repository.suppressions[record.ID] = record
	return record, nil
}

func (repository *MemoryRepository) GetSuppression(_ context.Context, id string) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.suppressions[id]
	if !exists {
		return SuppressionRecord{}, ErrNotFound
	}
	return record, nil
}

func (repository *MemoryRepository) ListSuppressions(
	_ context.Context, environment string,
) ([]SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records := make([]SuppressionRecord, 0, len(repository.suppressions))
	for _, record := range repository.suppressions {
		if environment == "" || record.Environment == environment {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (repository *MemoryRepository) UpdateSuppression(
	_ context.Context, id string, update func(*SuppressionRecord) error,
) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.suppressions[id]
	if !exists {
		return SuppressionRecord{}, ErrNotFound
	}
	if err := update(&record); err != nil {
		return SuppressionRecord{}, err
	}
	record.Revision++
	repository.suppressions[id] = record
	return record, nil
}

func (repository *MemoryRepository) CreatePlan(_ context.Context, record PlanRecord) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.plans[record.ID]; exists {
		if existing.DesiredStateDigest == record.DesiredStateDigest {
			return clonePlanRecord(existing), nil
		}
		return PlanRecord{}, ErrConflict
	}
	if record.DesiredState != nil {
		if err := validateDesiredStateLabelsFromDefinitions(*record.DesiredState, repository.labelDefinitions); err != nil {
			return PlanRecord{}, err
		}
	}
	if planTargetsRetiredCatalog(record, repository.catalogRetirements) {
		return PlanRecord{}, ErrConflict
	}
	for _, existing := range repository.plans {
		if existing.Command == nil && record.Command == nil && existing.Plan.Environment == record.Plan.Environment &&
			existing.Plan.Generation == record.Plan.Generation &&
			existing.DesiredStateDigest != record.DesiredStateDigest {
			return PlanRecord{}, ErrConflict
		}
	}
	repository.plans[record.ID] = clonePlanRecord(record)
	return clonePlanRecord(record), nil
}

func (repository *MemoryRepository) GetPlan(_ context.Context, id string) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.plans[id]
	if !exists {
		return PlanRecord{}, ErrNotFound
	}
	return clonePlanRecord(record), nil
}

func (repository *MemoryRepository) ListPlans(_ context.Context, environment string) ([]PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	plans := make([]PlanRecord, 0, len(repository.plans))
	for _, record := range repository.plans {
		if environment == "" || record.Plan.Environment == environment {
			plans = append(plans, clonePlanRecord(record))
		}
	}
	return plans, nil
}

func (repository *MemoryRepository) UpdatePlan(
	_ context.Context, id string, update func(*PlanRecord) error,
) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, exists := repository.plans[id]
	if !exists {
		return PlanRecord{}, ErrNotFound
	}
	working := clonePlanRecord(record)
	if err := update(&working); err != nil {
		return PlanRecord{}, err
	}
	working.Revision++
	repository.plans[id] = clonePlanRecord(working)
	return clonePlanRecord(working), nil
}

func (repository *MemoryRepository) PutEnvironmentReport(
	_ context.Context, report EnvironmentReport,
) (EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	report.Revision = repository.reports[report.Environment].Revision + 1
	repository.reports[report.Environment] = cloneEnvironmentReport(report)
	return cloneEnvironmentReport(report), nil
}

func (repository *MemoryRepository) GetEnvironmentReport(
	_ context.Context, environment string,
) (EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	report, localExists := repository.reports[environment]
	relayed, relayedExists := relayedReportFromCatalog(repository.importedCatalogs[environment])
	if !localExists && !relayedExists {
		return EnvironmentReport{}, ErrNotFound
	}
	if relayedExists && (!localExists || relayed.ObservedAt.After(report.ObservedAt)) {
		report = relayed
	}
	return cloneEnvironmentReport(report), nil
}

func (repository *MemoryRepository) ListEnvironmentReports(_ context.Context) ([]EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	merged := make(map[string]EnvironmentReport, len(repository.reports)+len(repository.importedCatalogs))
	for _, report := range repository.reports {
		merged[report.Environment] = report
	}
	for _, catalog := range repository.importedCatalogs {
		relayed, exists := relayedReportFromCatalog(catalog)
		if !exists {
			continue
		}
		if local, exists := merged[relayed.Environment]; !exists || relayed.ObservedAt.After(local.ObservedAt) {
			merged[relayed.Environment] = relayed
		}
	}
	result := make([]EnvironmentReport, 0, len(merged))
	for _, report := range merged {
		result = append(result, cloneEnvironmentReport(report))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Environment < result[j].Environment })
	return result, nil
}

func clonePlanRecord(record PlanRecord) PlanRecord {
	contents, _ := json.Marshal(record)
	var clone PlanRecord
	_ = json.Unmarshal(contents, &clone)
	return clone
}

func cloneEnvironmentReport(report EnvironmentReport) EnvironmentReport {
	contents, _ := json.Marshal(report)
	var clone EnvironmentReport
	_ = json.Unmarshal(contents, &clone)
	return clone
}

func relayedReportFromCatalog(catalog ImportedCatalog) (EnvironmentReport, bool) {
	if catalog.RelayedReport == nil || catalog.RelayedReport.Environment != catalog.Environment {
		return EnvironmentReport{}, false
	}
	return cloneEnvironmentReport(*catalog.RelayedReport), true
}

func NewFileRepository(root string) (*FileRepository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("repository directory is required")
	}
	repository := &FileRepository{root: root}
	if err := ensureDirectory(repository.root); err != nil {
		return nil, fmt.Errorf("create repository root %s: %w", repository.root, err)
	}
	for _, directory := range []string{
		repository.planDirectory(), repository.reportDirectory(), repository.suppressionDirectory(), repository.promotionDirectory(), repository.promotionStageControlDirectory(),
		repository.moduleReleaseDirectory(), repository.moduleInstallationDirectory(), repository.moduleAutoUpdatePolicyDirectory(), repository.blobUploadDirectory(),
		repository.exportPipelineDirectory(), repository.exportPipelineRevisionDirectory(), repository.exportPipelineScheduleRunDirectory(), repository.environmentProfileDirectory(), repository.environmentProfileRevisionDirectory(), repository.bundleDirectory(), repository.bundleImportDirectory(),
		repository.catalogRetirementDirectory(),
		repository.labelDefinitionDirectory(), repository.labelDefinitionRevisionDirectory(),
		repository.bundleTransferDirectory(),
		repository.importedCatalogDirectory(),
		repository.teamDirectory(), repository.resourceGrantDirectory(), repository.approvalPolicyDirectory(),
		repository.changeRequestDirectory(), repository.authorizationAuditDirectory(),
		repository.productPublicationDirectory(),
	} {
		if err := ensureDirectory(directory); err != nil {
			return nil, fmt.Errorf("create repository directory %s: %w", directory, err)
		}
	}
	return repository, nil
}

func (repository *FileRepository) PutExportPipeline(
	_ context.Context, pipeline ExportPipeline,
) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	definitions, err := repository.listLabelDefinitionsUnlocked(true)
	if err != nil {
		return ExportPipeline{}, err
	}
	if err := validatePipelineLabelsFromDefinitions(pipeline, labelDefinitionIndex(definitions)); err != nil {
		return ExportPipeline{}, err
	}
	if existing, err := repository.readExportPipeline(pipeline.ID); err == nil {
		if pipeline.Revision != existing.Revision+1 {
			return ExportPipeline{}, ErrConflict
		}
	} else if !errors.Is(err, ErrNotFound) {
		return ExportPipeline{}, err
	} else if pipeline.Revision != 1 {
		return ExportPipeline{}, ErrConflict
	}
	if err := ensureDirectory(filepath.Dir(repository.exportPipelineRevisionPath(pipeline.ID, pipeline.Revision))); err != nil {
		return ExportPipeline{}, err
	}
	if err := atomicWriteJSON(repository.exportPipelineRevisionPath(pipeline.ID, pipeline.Revision), pipeline); err != nil {
		return ExportPipeline{}, err
	}
	if err := atomicWriteJSON(repository.exportPipelinePath(pipeline.ID), pipeline); err != nil {
		return ExportPipeline{}, err
	}
	return pipeline, nil
}

func (repository *FileRepository) PutEnvironmentProfile(
	_ context.Context, profile EnvironmentProfile,
) (EnvironmentProfile, error) {
	if err := validateEnvironmentProfile(profile); err != nil {
		return EnvironmentProfile{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, err := repository.readEnvironmentProfile(profile.EnvironmentID); err == nil {
		if profile.Revision != existing.Revision+1 {
			return EnvironmentProfile{}, ErrConflict
		}
	} else if !errors.Is(err, ErrNotFound) {
		return EnvironmentProfile{}, err
	} else if profile.Revision != 1 {
		return EnvironmentProfile{}, ErrConflict
	}
	if err := ensureDirectory(filepath.Dir(repository.environmentProfileRevisionPath(profile.EnvironmentID, profile.Revision))); err != nil {
		return EnvironmentProfile{}, err
	}
	if err := atomicWriteJSON(repository.environmentProfileRevisionPath(profile.EnvironmentID, profile.Revision), profile); err != nil {
		return EnvironmentProfile{}, err
	}
	if err := atomicWriteJSON(repository.environmentProfilePath(profile.EnvironmentID), profile); err != nil {
		return EnvironmentProfile{}, err
	}
	return profile, nil
}

func (repository *FileRepository) GetEnvironmentProfile(
	_ context.Context, environment string,
) (EnvironmentProfile, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readEnvironmentProfile(environment)
}

func (repository *FileRepository) ListEnvironmentProfileRevisions(
	_ context.Context, environment string,
) ([]EnvironmentProfile, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !identifierPattern.MatchString(environment) {
		return nil, ErrNotFound
	}
	entries, err := os.ReadDir(filepath.Join(repository.environmentProfileRevisionDirectory(), environment))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]EnvironmentProfile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var profile EnvironmentProfile
		if err := readJSON(filepath.Join(repository.environmentProfileRevisionDirectory(), environment, entry.Name()), &profile); err != nil {
			return nil, err
		}
		if profile.EnvironmentID != environment || validateEnvironmentProfile(profile) != nil {
			return nil, ErrConflict
		}
		result = append(result, profile)
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	sortedEnvironmentProfiles(result)
	return result, nil
}

func (repository *FileRepository) GetExportPipeline(_ context.Context, id string) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readExportPipeline(id)
}

func (repository *FileRepository) GetExportPipelineRevision(_ context.Context, id string, revision uint64) (ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !identifierPattern.MatchString(id) || revision == 0 {
		return ExportPipeline{}, ErrNotFound
	}
	var pipeline ExportPipeline
	if err := readJSON(repository.exportPipelineRevisionPath(id, revision), &pipeline); err != nil {
		return ExportPipeline{}, err
	}
	return pipeline, nil
}

func (repository *FileRepository) ListExportPipelines(_ context.Context) ([]ExportPipeline, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listExportPipelinesUnlocked()
}

func (repository *FileRepository) listExportPipelinesUnlocked() ([]ExportPipeline, error) {
	entries, err := os.ReadDir(repository.exportPipelineDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]ExportPipeline, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var pipeline ExportPipeline
		if err := readJSON(filepath.Join(repository.exportPipelineDirectory(), entry.Name()), &pipeline); err != nil {
			return nil, err
		}
		result = append(result, pipeline)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *FileRepository) ClaimExportPipelineScheduleRun(
	_ context.Context, run ExportPipelineScheduleRun,
) (ExportPipelineScheduleRun, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if run.Status != ExportScheduleRunning {
		return ExportPipelineScheduleRun{}, false, ErrConflict
	}
	path := repository.exportPipelineScheduleRunPath(run.PipelineID, run.ScheduledFor)
	var existing ExportPipelineScheduleRun
	if err := readJSON(path, &existing); err == nil {
		if validateErr := validateExportPipelineScheduleRun(existing); validateErr != nil ||
			existing.PipelineID != run.PipelineID || !existing.ScheduledFor.Equal(run.ScheduledFor) {
			return ExportPipelineScheduleRun{}, false, ErrConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ExportPipelineScheduleRun{}, false, err
	}
	run.Revision = 1
	if err := validateExportPipelineScheduleRun(run); err != nil {
		return ExportPipelineScheduleRun{}, false, err
	}
	if err := atomicWriteJSON(path, run); err != nil {
		return ExportPipelineScheduleRun{}, false, err
	}
	return run, true, nil
}

func (repository *FileRepository) CompleteExportPipelineScheduleRun(
	_ context.Context, pipelineID string, scheduledFor time.Time, update func(*ExportPipelineScheduleRun) error,
) (ExportPipelineScheduleRun, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !identifierPattern.MatchString(pipelineID) || scheduledFor.IsZero() {
		return ExportPipelineScheduleRun{}, ErrNotFound
	}
	path := repository.exportPipelineScheduleRunPath(pipelineID, scheduledFor)
	var run ExportPipelineScheduleRun
	if err := readJSON(path, &run); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	if err := validateExportPipelineScheduleRun(run); err != nil || run.Status != ExportScheduleRunning {
		return ExportPipelineScheduleRun{}, ErrConflict
	}
	original := run
	if err := update(&run); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	run.Revision++
	if run.PipelineID != original.PipelineID || !run.ScheduledFor.Equal(original.ScheduledFor) ||
		run.PipelineRevision != original.PipelineRevision {
		return ExportPipelineScheduleRun{}, ErrConflict
	}
	if err := validateExportPipelineScheduleRun(run); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	if err := atomicWriteJSON(path, run); err != nil {
		return ExportPipelineScheduleRun{}, err
	}
	return run, nil
}

func (repository *FileRepository) ListExportPipelineScheduleRuns(
	_ context.Context, pipelineID string,
) ([]ExportPipelineScheduleRun, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.exportPipelineScheduleRunDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]ExportPipelineScheduleRun, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var run ExportPipelineScheduleRun
		if err := readJSON(filepath.Join(repository.exportPipelineScheduleRunDirectory(), entry.Name()), &run); err != nil {
			return nil, err
		}
		if err := validateExportPipelineScheduleRun(run); err != nil {
			return nil, err
		}
		if pipelineID == "" || run.PipelineID == pipelineID {
			result = append(result, run)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ScheduledFor.Before(result[j].ScheduledFor) })
	return result, nil
}

func (repository *FileRepository) CreateBundle(_ context.Context, bundle BundleEnvelope) (BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, err := repository.readBundle(bundle.Manifest.ID); err == nil {
		if existing.ManifestDigest == bundle.ManifestDigest {
			return existing, nil
		}
		return BundleEnvelope{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return BundleEnvelope{}, err
	}
	if err := atomicWriteJSON(repository.bundlePath(bundle.Manifest.ID), bundle); err != nil {
		return BundleEnvelope{}, err
	}
	return bundle, nil
}

func (repository *FileRepository) GetBundle(_ context.Context, id string) (BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readBundle(id)
}

func (repository *FileRepository) ListBundles(_ context.Context, pipelineID string) ([]BundleEnvelope, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.bundleDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]BundleEnvelope, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var bundle BundleEnvelope
		if err := readJSON(filepath.Join(repository.bundleDirectory(), entry.Name()), &bundle); err != nil {
			return nil, err
		}
		if pipelineID == "" || bundle.Manifest.PipelineID == pipelineID {
			result = append(result, bundle)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Manifest.Sequence != result[j].Manifest.Sequence {
			return result[i].Manifest.Sequence < result[j].Manifest.Sequence
		}
		return result[i].Manifest.ID < result[j].Manifest.ID
	})
	return result, nil
}

func (repository *FileRepository) PutBundleTransferConfirmation(
	_ context.Context, confirmation BundleTransferConfirmation,
) (BundleTransferConfirmation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	path := repository.bundleTransferPath(confirmation.ID)
	var existing BundleTransferConfirmation
	if err := readJSON(path, &existing); err == nil {
		if confirmation.Revision != existing.Revision+1 {
			return BundleTransferConfirmation{}, ErrConflict
		}
	} else if !errors.Is(err, ErrNotFound) {
		return BundleTransferConfirmation{}, err
	} else if confirmation.Revision != 1 {
		return BundleTransferConfirmation{}, ErrConflict
	}
	if err := atomicWriteJSON(path, confirmation); err != nil {
		return BundleTransferConfirmation{}, err
	}
	return confirmation, nil
}

func (repository *FileRepository) ListBundleTransferConfirmations(
	_ context.Context, pipelineID, targetHub string,
) ([]BundleTransferConfirmation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.bundleTransferDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]BundleTransferConfirmation, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var confirmation BundleTransferConfirmation
		if err := readJSON(filepath.Join(repository.bundleTransferDirectory(), entry.Name()), &confirmation); err != nil {
			return nil, err
		}
		if (pipelineID == "" || confirmation.PipelineID == pipelineID) &&
			(targetHub == "" || confirmation.TargetHub == targetHub) {
			result = append(result, confirmation)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *FileRepository) CreateBundleImport(_ context.Context, record BundleImport) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, err := repository.readBundleImport(record.ID); err == nil {
		if existing.ManifestDigest == record.ManifestDigest {
			return existing, nil
		}
		return BundleImport{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return BundleImport{}, err
	}
	if err := atomicWriteJSON(repository.bundleImportPath(record.ID), record); err != nil {
		return BundleImport{}, err
	}
	return record, nil
}

func (repository *FileRepository) GetBundleImport(_ context.Context, id string) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readBundleImport(id)
}

func (repository *FileRepository) ListBundleImports(_ context.Context) ([]BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.bundleImportDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]BundleImport, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record BundleImport
		if err := readJSON(filepath.Join(repository.bundleImportDirectory(), entry.Name()), &record); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *FileRepository) UpdateBundleImport(
	_ context.Context, id string, update func(*BundleImport) error,
) (BundleImport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, err := repository.readBundleImport(id)
	if err != nil {
		return BundleImport{}, err
	}
	if err := update(&record); err != nil {
		return BundleImport{}, err
	}
	record.Revision++
	if err := atomicWriteJSON(repository.bundleImportPath(id), record); err != nil {
		return BundleImport{}, err
	}
	return record, nil
}

func (repository *FileRepository) PutImportedCatalog(_ context.Context, catalog ImportedCatalog) (ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var existing ImportedCatalog
	err := readJSON(repository.importedCatalogPath(catalog.Environment), &existing)
	if err == nil && catalog.Revision != existing.Revision+1 {
		return ImportedCatalog{}, ErrConflict
	}
	if errors.Is(err, ErrNotFound) && catalog.Revision != 1 {
		return ImportedCatalog{}, ErrConflict
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ImportedCatalog{}, err
	}
	if err := atomicWriteJSON(repository.importedCatalogPath(catalog.Environment), catalog); err != nil {
		return ImportedCatalog{}, err
	}
	return catalog, nil
}

func (repository *FileRepository) GetImportedCatalog(_ context.Context, environment string) (ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !dnsLabelPattern.MatchString(environment) {
		return ImportedCatalog{}, ErrNotFound
	}
	var catalog ImportedCatalog
	if err := readJSON(repository.importedCatalogPath(environment), &catalog); err != nil {
		return ImportedCatalog{}, err
	}
	return catalog, nil
}

func (repository *FileRepository) ListImportedCatalogs(_ context.Context) ([]ImportedCatalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.importedCatalogDirectory())
	if err != nil {
		return nil, err
	}
	result := make([]ImportedCatalog, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var catalog ImportedCatalog
		if err := readJSON(filepath.Join(repository.importedCatalogDirectory(), entry.Name()), &catalog); err != nil {
			return nil, err
		}
		result = append(result, catalog)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Environment < result[j].Environment })
	return result, nil
}

func (repository *FileRepository) PutBlob(
	ctx context.Context, expectedDigest, mediaType string, source io.Reader,
) (BlobDescriptor, bool, error) {
	if err := validateExpectedDigest(expectedDigest); err != nil {
		return BlobDescriptor{}, false, err
	}
	temporary, err := os.CreateTemp(repository.blobUploadDirectory(), ".upload-*.tmp")
	if err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("create artifact upload: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hasher), contextReader{ctx: ctx, reader: source})
	if copyErr != nil {
		_ = temporary.Close()
		return BlobDescriptor{}, false, fmt.Errorf("store artifact upload: %w", copyErr)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return BlobDescriptor{}, false, fmt.Errorf("sync artifact upload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("close artifact upload: %w", err)
	}
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if expectedDigest != "" && expectedDigest != digest {
		return BlobDescriptor{}, false, fmt.Errorf("%w: expected %s, computed %s", ErrDigestMismatch, expectedDigest, digest)
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, err := repository.readBlobDescriptor(digest); err == nil {
		if existing.Size != size {
			return BlobDescriptor{}, false, fmt.Errorf("stored artifact %s has inconsistent size", digest)
		}
		existing.MediaType = normalizeMediaType(mediaType)
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return BlobDescriptor{}, false, err
	}
	directory := repository.blobDirectory(digest)
	if err := ensureDirectory(directory); err != nil {
		return BlobDescriptor{}, false, err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("protect artifact upload: %w", err)
	}
	if err := os.Rename(temporaryPath, repository.blobDataPath(digest)); err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("commit artifact %s: %w", digest, err)
	}
	descriptor := BlobDescriptor{
		MediaType: normalizeMediaType(mediaType), Digest: digest, Size: size, CreatedAt: time.Now().UTC(),
	}
	if err := atomicWriteJSON(repository.blobDescriptorPath(digest), descriptor); err != nil {
		_ = os.Remove(repository.blobDataPath(digest))
		return BlobDescriptor{}, false, err
	}
	return descriptor, true, nil
}

func (repository *FileRepository) OpenBlob(
	_ context.Context, digest string,
) (BlobDescriptor, io.ReadCloser, error) {
	if !digestPattern.MatchString(digest) {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	descriptor, err := repository.readBlobDescriptor(digest)
	if err != nil {
		return BlobDescriptor{}, nil, err
	}
	file, err := os.Open(repository.blobDataPath(digest))
	if errors.Is(err, os.ErrNotExist) {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	if err != nil {
		return BlobDescriptor{}, nil, fmt.Errorf("open artifact %s: %w", digest, err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return BlobDescriptor{}, nil, fmt.Errorf("inspect artifact %s: %w", digest, err)
	}
	if stat.Size() != descriptor.Size {
		_ = file.Close()
		return BlobDescriptor{}, nil, fmt.Errorf("%w: artifact %s size is %d, expected %d", ErrArtifactCorrupt, digest, stat.Size(), descriptor.Size)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		_ = file.Close()
		return BlobDescriptor{}, nil, fmt.Errorf("verify artifact %s: %w", digest, err)
	}
	computed := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if computed != digest {
		_ = file.Close()
		return BlobDescriptor{}, nil, fmt.Errorf("%w: artifact %s computed as %s", ErrArtifactCorrupt, digest, computed)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return BlobDescriptor{}, nil, fmt.Errorf("rewind artifact %s: %w", digest, err)
	}
	return descriptor, file, nil
}

func (repository *FileRepository) CreatePromotionUnlessStagePaused(
	_ context.Context, record PromotionRecord,
) (PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	control, err := repository.readPromotionStageControl(record.Product, record.FromChannel, record.ToChannel)
	if err != nil {
		return PromotionRecord{}, err
	}
	if control.Status == PromotionStagePaused {
		return PromotionRecord{}, promotionStagePausedError(control)
	}
	if existing, err := repository.readPromotion(record.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return PromotionRecord{}, err
	}
	if err := atomicWriteJSON(repository.promotionPath(record.ID), record); err != nil {
		return PromotionRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) GetPromotionStageControl(
	_ context.Context, product, fromChannel, toChannel string,
) (PromotionStageControl, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readPromotionStageControl(product, fromChannel, toChannel)
}

func (repository *FileRepository) UpdatePromotionStageControl(
	_ context.Context, product, fromChannel, toChannel string, update func(*PromotionStageControl) error,
) (PromotionStageControl, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	control, err := repository.readPromotionStageControl(product, fromChannel, toChannel)
	if err != nil {
		return PromotionStageControl{}, err
	}
	previous := control
	if err := update(&control); err != nil {
		return PromotionStageControl{}, err
	}
	id := promotionStageControlID(product, fromChannel, toChannel)
	if control.ID != id || control.Product != product || control.FromChannel != fromChannel || control.ToChannel != toChannel ||
		(control.Status != PromotionStageActive && control.Status != PromotionStagePaused) {
		return PromotionStageControl{}, ErrConflict
	}
	if control == previous {
		return control, nil
	}
	control.Revision = previous.Revision + 1
	if err := atomicWriteJSON(repository.promotionStageControlPath(id), control); err != nil {
		return PromotionStageControl{}, err
	}
	return control, nil
}

func (repository *FileRepository) CreateModuleRelease(
	_ context.Context, release ModuleRelease,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	retirements, err := repository.listCatalogRetirementsUnlocked("")
	if err != nil {
		return ModuleRelease{}, err
	}
	if moduleEntitiesTargetRetiredProduct(release.Lock.Entities, catalogRetirementIndex(retirements)) {
		return ModuleRelease{}, ErrConflict
	}
	if !moduleNamePattern.MatchString(release.Name) || !moduleReleaseVersionPattern.MatchString(release.Version) {
		return ModuleRelease{}, ErrConflict
	}
	path := repository.moduleReleasePath(release.Name, release.Version)
	if existing, err := repository.readModuleRelease(release.Name, release.Version); err == nil {
		if existing.Digest == release.Digest {
			return existing, nil
		}
		return ModuleRelease{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return ModuleRelease{}, err
	}
	if err := ensureDirectory(filepath.Dir(path)); err != nil {
		return ModuleRelease{}, err
	}
	if err := atomicWriteJSON(path, release); err != nil {
		return ModuleRelease{}, err
	}
	return release, nil
}

func (repository *FileRepository) GetModuleRelease(
	_ context.Context, name, version string,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readModuleRelease(name, version)
}

func (repository *FileRepository) ListModuleReleases(
	_ context.Context, name string,
) ([]ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listModuleReleasesUnlocked(name)
}

func (repository *FileRepository) UpdateModuleRelease(
	_ context.Context, name, version string, update func(*ModuleRelease) error,
) (ModuleRelease, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	release, err := repository.readModuleRelease(name, version)
	if err != nil {
		return ModuleRelease{}, err
	}
	working := cloneJSON(release)
	if err := update(&working); err != nil {
		return ModuleRelease{}, err
	}
	if working.Name != release.Name || working.Version != release.Version || working.Digest != release.Digest ||
		!working.CreatedAt.Equal(release.CreatedAt) ||
		moduleReleaseDigest(working.Definition, working.Lock) != release.Digest {
		return ModuleRelease{}, ErrConflict
	}
	if err := atomicWriteJSON(repository.moduleReleasePath(name, version), working); err != nil {
		return ModuleRelease{}, err
	}
	return working, nil
}

func (repository *FileRepository) CreateModuleInstallation(
	_ context.Context, installation ModuleInstallation,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	retirements, err := repository.listCatalogRetirementsUnlocked("")
	if err != nil {
		return ModuleInstallation{}, err
	}
	if moduleInstallationTargetsRetiredProduct(installation, catalogRetirementIndex(retirements)) {
		return ModuleInstallation{}, ErrConflict
	}
	if !strings.HasPrefix(installation.ID, "modi_") || strings.ContainsAny(installation.ID, `/\\`) {
		return ModuleInstallation{}, ErrConflict
	}
	if existing, err := repository.readModuleInstallation(installation.ID); err == nil {
		return existing, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return ModuleInstallation{}, err
	}
	if err := atomicWriteJSON(repository.moduleInstallationPath(installation.ID), installation); err != nil {
		return ModuleInstallation{}, err
	}
	return installation, nil
}

func (repository *FileRepository) GetModuleInstallation(
	_ context.Context, id string,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readModuleInstallation(id)
}

func (repository *FileRepository) ListModuleInstallations(
	_ context.Context, environment string,
) ([]ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listModuleInstallationsUnlocked(environment)
}

func (repository *FileRepository) listModuleInstallationsUnlocked(environment string) ([]ModuleInstallation, error) {
	entries, err := os.ReadDir(repository.moduleInstallationDirectory())
	if err != nil {
		return nil, fmt.Errorf("list Module installations: %w", err)
	}
	installations := make([]ModuleInstallation, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var installation ModuleInstallation
		if err := readJSON(filepath.Join(repository.moduleInstallationDirectory(), entry.Name()), &installation); err != nil {
			return nil, err
		}
		if environment == "" || installation.Environment == environment {
			installations = append(installations, installation)
		}
	}
	sort.Slice(installations, func(i, j int) bool { return installations[i].ID < installations[j].ID })
	return installations, nil
}

func (repository *FileRepository) UpdateModuleInstallation(
	_ context.Context, id string, update func(*ModuleInstallation) error,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	installation, err := repository.readModuleInstallation(id)
	if err != nil {
		return ModuleInstallation{}, err
	}
	if err := update(&installation); err != nil {
		return ModuleInstallation{}, err
	}
	retirements, err := repository.listCatalogRetirementsUnlocked("")
	if err != nil {
		return ModuleInstallation{}, err
	}
	if moduleInstallationTargetsRetiredProduct(installation, catalogRetirementIndex(retirements)) {
		return ModuleInstallation{}, ErrConflict
	}
	installation.Revision++
	if err := atomicWriteJSON(repository.moduleInstallationPath(id), installation); err != nil {
		return ModuleInstallation{}, err
	}
	return installation, nil
}

func (repository *FileRepository) UpdateModuleInstallationAutomatically(
	_ context.Context, id, module string, policyRevision uint64, update func(*ModuleInstallation) error,
) (ModuleInstallation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !moduleNamePattern.MatchString(module) {
		return ModuleInstallation{}, ErrConflict
	}
	var policy ModuleAutoUpdatePolicy
	if err := readJSON(repository.moduleAutoUpdatePolicyPath(module), &policy); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ModuleInstallation{}, ErrConflict
		}
		return ModuleInstallation{}, err
	}
	if err := validateModuleAutoUpdatePolicy(policy); err != nil || policy.Module != module ||
		!policy.Enabled || policy.Revision != policyRevision {
		return ModuleInstallation{}, ErrConflict
	}
	installation, err := repository.readModuleInstallation(id)
	if err != nil {
		return ModuleInstallation{}, err
	}
	if installation.Module != module {
		return ModuleInstallation{}, ErrConflict
	}
	if err := update(&installation); err != nil {
		return ModuleInstallation{}, err
	}
	retirements, err := repository.listCatalogRetirementsUnlocked("")
	if err != nil {
		return ModuleInstallation{}, err
	}
	if moduleInstallationTargetsRetiredProduct(installation, catalogRetirementIndex(retirements)) {
		return ModuleInstallation{}, ErrConflict
	}
	installation.Revision++
	if err := atomicWriteJSON(repository.moduleInstallationPath(id), installation); err != nil {
		return ModuleInstallation{}, err
	}
	return installation, nil
}

func (repository *FileRepository) PutModuleAutoUpdatePolicy(
	_ context.Context, policy ModuleAutoUpdatePolicy,
) (ModuleAutoUpdatePolicy, error) {
	if err := validateModuleAutoUpdatePolicy(policy); err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	path := repository.moduleAutoUpdatePolicyPath(policy.Module)
	if err := repository.checkRevision(path, policy.Revision); err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	if err := atomicWriteJSON(path, policy); err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	return policy, nil
}

func (repository *FileRepository) GetModuleAutoUpdatePolicy(
	_ context.Context, module string,
) (ModuleAutoUpdatePolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !moduleNamePattern.MatchString(module) {
		return ModuleAutoUpdatePolicy{}, ErrNotFound
	}
	var policy ModuleAutoUpdatePolicy
	if err := readJSON(repository.moduleAutoUpdatePolicyPath(module), &policy); err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	if policy.Module != module {
		return ModuleAutoUpdatePolicy{}, ErrConflict
	}
	if err := validateModuleAutoUpdatePolicy(policy); err != nil {
		return ModuleAutoUpdatePolicy{}, ErrConflict
	}
	return policy, nil
}

func (repository *FileRepository) GetPromotion(_ context.Context, id string) (PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readPromotion(id)
}

func (repository *FileRepository) ListPromotions(
	_ context.Context, product string,
) ([]PromotionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	entries, err := os.ReadDir(repository.promotionDirectory())
	if err != nil {
		return nil, fmt.Errorf("list promotions: %w", err)
	}
	records := make([]PromotionRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record PromotionRecord
		if err := readJSON(filepath.Join(repository.promotionDirectory(), entry.Name()), &record); err != nil {
			return nil, err
		}
		if product == "" || record.Product == product {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (repository *FileRepository) CreateSuppression(
	_ context.Context, record SuppressionRecord,
) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, err := repository.readSuppression(record.ID); err == nil {
		if existing.Environment == record.Environment && existing.PlanID == record.PlanID && existing.Source == record.Source {
			return existing, nil
		}
		return SuppressionRecord{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return SuppressionRecord{}, err
	}
	if err := atomicWriteJSON(repository.suppressionPath(record.ID), record); err != nil {
		return SuppressionRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) GetSuppression(_ context.Context, id string) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readSuppression(id)
}

func (repository *FileRepository) ListSuppressions(
	_ context.Context, environment string,
) ([]SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listSuppressionsUnlocked(environment)
}

func (repository *FileRepository) UpdateSuppression(
	_ context.Context, id string, update func(*SuppressionRecord) error,
) (SuppressionRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, err := repository.readSuppression(id)
	if err != nil {
		return SuppressionRecord{}, err
	}
	if err := update(&record); err != nil {
		return SuppressionRecord{}, err
	}
	record.Revision++
	if err := atomicWriteJSON(repository.suppressionPath(id), record); err != nil {
		return SuppressionRecord{}, err
	}
	return record, nil
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("path exists but is not a directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err = os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path exists but is not a directory")
	}
	return nil
}

func (repository *FileRepository) CreatePlan(_ context.Context, record PlanRecord) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if existing, err := repository.readPlan(record.ID); err == nil {
		if existing.DesiredStateDigest == record.DesiredStateDigest {
			return existing, nil
		}
		return PlanRecord{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return PlanRecord{}, err
	}
	definitions, err := repository.listLabelDefinitionsUnlocked(true)
	if err != nil {
		return PlanRecord{}, err
	}
	if record.DesiredState != nil {
		if err := validateDesiredStateLabelsFromDefinitions(*record.DesiredState, labelDefinitionIndex(definitions)); err != nil {
			return PlanRecord{}, err
		}
	}
	retirements, err := repository.listCatalogRetirementsUnlocked("")
	if err != nil {
		return PlanRecord{}, err
	}
	if planTargetsRetiredCatalog(record, catalogRetirementIndex(retirements)) {
		return PlanRecord{}, ErrConflict
	}
	plans, err := repository.listPlansUnlocked(record.Plan.Environment)
	if err != nil {
		return PlanRecord{}, err
	}
	for _, existing := range plans {
		if existing.Command == nil && record.Command == nil && existing.Plan.Generation == record.Plan.Generation && existing.DesiredStateDigest != record.DesiredStateDigest {
			return PlanRecord{}, fmt.Errorf("%w: generation %s already has different desired state", ErrConflict, record.Plan.Generation)
		}
	}
	if err := atomicWriteJSON(repository.planPath(record.ID), record); err != nil {
		return PlanRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) GetPlan(_ context.Context, id string) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readPlan(id)
}

func (repository *FileRepository) ListPlans(_ context.Context, environment string) ([]PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listPlansUnlocked(environment)
}

func (repository *FileRepository) UpdatePlan(
	_ context.Context, id string, update func(*PlanRecord) error,
) (PlanRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, err := repository.readPlan(id)
	if err != nil {
		return PlanRecord{}, err
	}
	if err := update(&record); err != nil {
		return PlanRecord{}, err
	}
	record.Revision++
	if record.UpdatedAt.Before(record.CreatedAt) {
		record.UpdatedAt = record.CreatedAt
	}
	if err := atomicWriteJSON(repository.planPath(id), record); err != nil {
		return PlanRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) PutEnvironmentReport(
	_ context.Context, report EnvironmentReport,
) (EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	envelope, err := repository.readApolloReportEnvelope(report.Environment)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return EnvironmentReport{}, err
	}
	report.Revision = envelope.Revision + 1
	envelope.EnvironmentReport = report
	envelope.ApolloProjectionPending = false
	if err := atomicWriteJSON(repository.reportPath(report.Environment), envelope); err != nil {
		return EnvironmentReport{}, err
	}
	return report, nil
}

func (repository *FileRepository) GetEnvironmentReport(_ context.Context, environment string) (EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	local, localErr := repository.readReport(environment)
	var catalog ImportedCatalog
	catalogErr := readJSON(repository.importedCatalogPath(environment), &catalog)
	relayed, relayedExists := relayedReportFromCatalog(catalog)
	if localErr != nil && !errors.Is(localErr, ErrNotFound) {
		return EnvironmentReport{}, localErr
	}
	if catalogErr != nil && !errors.Is(catalogErr, ErrNotFound) {
		return EnvironmentReport{}, catalogErr
	}
	if localErr != nil && !relayedExists {
		return EnvironmentReport{}, ErrNotFound
	}
	if relayedExists && (localErr != nil || relayed.ObservedAt.After(local.ObservedAt)) {
		return relayed, nil
	}
	return local, nil
}

func (repository *FileRepository) ListEnvironmentReports(_ context.Context) ([]EnvironmentReport, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listEnvironmentReportsUnlocked()
}

func (repository *FileRepository) listEnvironmentReportsUnlocked() ([]EnvironmentReport, error) {
	entries, err := os.ReadDir(repository.reportDirectory())
	if err != nil {
		return nil, err
	}
	merged := make(map[string]EnvironmentReport, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var report apolloReportEnvelope
		if err := readJSON(filepath.Join(repository.reportDirectory(), entry.Name()), &report); err != nil {
			return nil, err
		}
		if !report.ApolloProjectionPending {
			merged[report.Environment] = report.EnvironmentReport
		}
	}
	catalogEntries, err := os.ReadDir(repository.importedCatalogDirectory())
	if err != nil {
		return nil, err
	}
	for _, entry := range catalogEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var catalog ImportedCatalog
		if err := readJSON(filepath.Join(repository.importedCatalogDirectory(), entry.Name()), &catalog); err != nil {
			return nil, err
		}
		relayed, exists := relayedReportFromCatalog(catalog)
		if !exists {
			continue
		}
		if local, exists := merged[relayed.Environment]; !exists || relayed.ObservedAt.After(local.ObservedAt) {
			merged[relayed.Environment] = relayed
		}
	}
	result := make([]EnvironmentReport, 0, len(merged))
	for _, report := range merged {
		result = append(result, report)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Environment < result[j].Environment })
	return result, nil
}

func (repository *FileRepository) listPlansUnlocked(environment string) ([]PlanRecord, error) {
	entries, err := os.ReadDir(repository.planDirectory())
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	plans := make([]PlanRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record PlanRecord
		if err := readJSON(filepath.Join(repository.planDirectory(), entry.Name()), &record); err != nil {
			return nil, err
		}
		if environment == "" || record.Plan.Environment == environment {
			plans = append(plans, record)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	return plans, nil
}

func (repository *FileRepository) listSuppressionsUnlocked(environment string) ([]SuppressionRecord, error) {
	entries, err := os.ReadDir(repository.suppressionDirectory())
	if err != nil {
		return nil, fmt.Errorf("list suppressions: %w", err)
	}
	records := make([]SuppressionRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record SuppressionRecord
		if err := readJSON(filepath.Join(repository.suppressionDirectory(), entry.Name()), &record); err != nil {
			return nil, err
		}
		if environment == "" || record.Environment == environment {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (repository *FileRepository) readPlan(id string) (PlanRecord, error) {
	if !strings.HasPrefix(id, "pln_") || strings.ContainsAny(id, `/\\`) {
		return PlanRecord{}, ErrNotFound
	}
	var record PlanRecord
	if err := readJSON(repository.planPath(id), &record); err != nil {
		return PlanRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) readReport(environment string) (EnvironmentReport, error) {
	if !dnsLabelPattern.MatchString(environment) {
		return EnvironmentReport{}, ErrNotFound
	}
	envelope, err := repository.readApolloReportEnvelope(environment)
	if err != nil {
		return EnvironmentReport{}, err
	}
	if envelope.ApolloProjectionPending {
		return EnvironmentReport{}, ErrNotFound
	}
	return envelope.EnvironmentReport, nil
}

func (repository *FileRepository) readSuppression(id string) (SuppressionRecord, error) {
	if !strings.HasPrefix(id, "sup_") || strings.ContainsAny(id, `/\\`) {
		return SuppressionRecord{}, ErrNotFound
	}
	var record SuppressionRecord
	if err := readJSON(repository.suppressionPath(id), &record); err != nil {
		return SuppressionRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) readPromotion(id string) (PromotionRecord, error) {
	if !strings.HasPrefix(id, "pro_") || strings.ContainsAny(id, `/\\`) {
		return PromotionRecord{}, ErrNotFound
	}
	var record PromotionRecord
	if err := readJSON(repository.promotionPath(id), &record); err != nil {
		return PromotionRecord{}, err
	}
	return record, nil
}

func (repository *FileRepository) readPromotionStageControl(
	product, fromChannel, toChannel string,
) (PromotionStageControl, error) {
	control := defaultPromotionStageControl(product, fromChannel, toChannel)
	if !coordinatePattern.MatchString(product) || !releaseChannelPattern.MatchString(fromChannel) ||
		!releaseChannelPattern.MatchString(toChannel) || fromChannel == toChannel {
		return PromotionStageControl{}, ErrNotFound
	}
	if err := readJSON(repository.promotionStageControlPath(control.ID), &control); err != nil {
		if errors.Is(err, ErrNotFound) {
			return defaultPromotionStageControl(product, fromChannel, toChannel), nil
		}
		return PromotionStageControl{}, err
	}
	if err := validatePersistedPromotionStageControl(control, product, fromChannel, toChannel); err != nil {
		return PromotionStageControl{}, err
	}
	return control, nil
}

func (repository *FileRepository) readModuleRelease(name, version string) (ModuleRelease, error) {
	if !moduleNamePattern.MatchString(name) || !moduleReleaseVersionPattern.MatchString(version) {
		return ModuleRelease{}, ErrNotFound
	}
	var release ModuleRelease
	if err := readJSON(repository.moduleReleasePath(name, version), &release); err != nil {
		return ModuleRelease{}, err
	}
	return release, nil
}

func (repository *FileRepository) listModuleReleasesUnlocked(name string) ([]ModuleRelease, error) {
	if name != "" && !moduleNamePattern.MatchString(name) {
		return nil, ErrNotFound
	}
	moduleNames := []string{name}
	if name == "" {
		entries, err := os.ReadDir(repository.moduleReleaseDirectory())
		if err != nil {
			return nil, fmt.Errorf("list Module releases: %w", err)
		}
		moduleNames = moduleNames[:0]
		for _, entry := range entries {
			if entry.IsDir() && moduleNamePattern.MatchString(entry.Name()) {
				moduleNames = append(moduleNames, entry.Name())
			}
		}
	}
	releases := make([]ModuleRelease, 0)
	for _, moduleName := range moduleNames {
		directory := filepath.Join(repository.moduleReleaseDirectory(), moduleName)
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list Module %s releases: %w", moduleName, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			var release ModuleRelease
			if err := readJSON(filepath.Join(directory, entry.Name()), &release); err != nil {
				return nil, err
			}
			releases = append(releases, release)
		}
	}
	sortModuleReleases(releases)
	return releases, nil
}

func (repository *FileRepository) readModuleInstallation(id string) (ModuleInstallation, error) {
	if !strings.HasPrefix(id, "modi_") || strings.ContainsAny(id, `/\\`) {
		return ModuleInstallation{}, ErrNotFound
	}
	var installation ModuleInstallation
	if err := readJSON(repository.moduleInstallationPath(id), &installation); err != nil {
		return ModuleInstallation{}, err
	}
	return installation, nil
}

func (repository *FileRepository) readEnvironmentProfile(environment string) (EnvironmentProfile, error) {
	if !identifierPattern.MatchString(environment) {
		return EnvironmentProfile{}, ErrNotFound
	}
	var profile EnvironmentProfile
	if err := readJSON(repository.environmentProfilePath(environment), &profile); err != nil {
		return EnvironmentProfile{}, err
	}
	if profile.EnvironmentID != environment || validateEnvironmentProfile(profile) != nil {
		return EnvironmentProfile{}, ErrConflict
	}
	return profile, nil
}

func (repository *FileRepository) readExportPipeline(id string) (ExportPipeline, error) {
	if !identifierPattern.MatchString(id) {
		return ExportPipeline{}, ErrNotFound
	}
	var pipeline ExportPipeline
	if err := readJSON(repository.exportPipelinePath(id), &pipeline); err != nil {
		return ExportPipeline{}, err
	}
	return pipeline, nil
}

func (repository *FileRepository) readBundle(id string) (BundleEnvelope, error) {
	if !strings.HasPrefix(id, "bnd_") || strings.ContainsAny(id, `/\\`) {
		return BundleEnvelope{}, ErrNotFound
	}
	var bundle BundleEnvelope
	if err := readJSON(repository.bundlePath(id), &bundle); err != nil {
		return BundleEnvelope{}, err
	}
	return bundle, nil
}

func (repository *FileRepository) readBundleImport(id string) (BundleImport, error) {
	if !strings.HasPrefix(id, "bni_") || strings.ContainsAny(id, `/\\`) {
		return BundleImport{}, ErrNotFound
	}
	var record BundleImport
	if err := readJSON(repository.bundleImportPath(id), &record); err != nil {
		return BundleImport{}, err
	}
	return record, nil
}

func (repository *FileRepository) planDirectory() string {
	return filepath.Join(repository.root, "plans")
}
func (repository *FileRepository) reportDirectory() string {
	return filepath.Join(repository.root, "reports")
}
func (repository *FileRepository) suppressionDirectory() string {
	return filepath.Join(repository.root, "suppressions")
}
func (repository *FileRepository) promotionDirectory() string {
	return filepath.Join(repository.root, "promotions")
}
func (repository *FileRepository) promotionStageControlDirectory() string {
	return filepath.Join(repository.root, "promotion-stage-controls")
}
func (repository *FileRepository) moduleReleaseDirectory() string {
	return filepath.Join(repository.root, "module-releases")
}
func (repository *FileRepository) moduleInstallationDirectory() string {
	return filepath.Join(repository.root, "module-installations")
}
func (repository *FileRepository) moduleAutoUpdatePolicyDirectory() string {
	return filepath.Join(repository.root, "module-auto-update-policies")
}
func (repository *FileRepository) exportPipelineDirectory() string {
	return filepath.Join(repository.root, "export-pipelines")
}
func (repository *FileRepository) exportPipelineRevisionDirectory() string {
	return filepath.Join(repository.root, "export-pipeline-revisions")
}
func (repository *FileRepository) exportPipelineScheduleRunDirectory() string {
	return filepath.Join(repository.root, "export-pipeline-schedule-runs")
}
func (repository *FileRepository) environmentProfileDirectory() string {
	return filepath.Join(repository.root, "environment-profiles")
}
func (repository *FileRepository) environmentProfileRevisionDirectory() string {
	return filepath.Join(repository.root, "environment-profile-revisions")
}
func (repository *FileRepository) labelDefinitionDirectory() string {
	return filepath.Join(repository.root, "label-definitions")
}
func (repository *FileRepository) labelDefinitionRevisionDirectory() string {
	return filepath.Join(repository.root, "label-definition-revisions")
}
func (repository *FileRepository) catalogRetirementDirectory() string {
	return filepath.Join(repository.root, "catalog-retirements")
}
func (repository *FileRepository) bundleDirectory() string {
	return filepath.Join(repository.root, "bundles")
}
func (repository *FileRepository) bundleImportDirectory() string {
	return filepath.Join(repository.root, "bundle-imports")
}
func (repository *FileRepository) bundleTransferDirectory() string {
	return filepath.Join(repository.root, "bundle-transfer-confirmations")
}
func (repository *FileRepository) importedCatalogDirectory() string {
	return filepath.Join(repository.root, "imported-catalogs")
}
func (repository *FileRepository) teamDirectory() string {
	return filepath.Join(repository.root, "teams")
}
func (repository *FileRepository) resourceGrantDirectory() string {
	return filepath.Join(repository.root, "resource-grants")
}
func (repository *FileRepository) approvalPolicyDirectory() string {
	return filepath.Join(repository.root, "approval-policies")
}
func (repository *FileRepository) changeRequestDirectory() string {
	return filepath.Join(repository.root, "change-requests")
}
func (repository *FileRepository) authorizationAuditDirectory() string {
	return filepath.Join(repository.root, "authorization-audit")
}
func (repository *FileRepository) blobRootDirectory() string {
	return filepath.Join(repository.root, "blobs", "sha256")
}
func (repository *FileRepository) blobUploadDirectory() string {
	return filepath.Join(repository.root, "blob-uploads")
}
func (repository *FileRepository) blobDirectory(digest string) string {
	encoded := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(repository.blobRootDirectory(), encoded[:2], encoded)
}
func (repository *FileRepository) blobDataPath(digest string) string {
	return filepath.Join(repository.blobDirectory(digest), "data")
}
func (repository *FileRepository) blobDescriptorPath(digest string) string {
	return filepath.Join(repository.blobDirectory(digest), "descriptor.json")
}
func (repository *FileRepository) planPath(id string) string {
	return filepath.Join(repository.planDirectory(), id+".json")
}
func (repository *FileRepository) reportPath(environment string) string {
	return filepath.Join(repository.reportDirectory(), environment+".json")
}
func (repository *FileRepository) suppressionPath(id string) string {
	return filepath.Join(repository.suppressionDirectory(), id+".json")
}
func (repository *FileRepository) promotionPath(id string) string {
	return filepath.Join(repository.promotionDirectory(), id+".json")
}
func (repository *FileRepository) promotionStageControlPath(id string) string {
	return filepath.Join(repository.promotionStageControlDirectory(), id+".json")
}
func (repository *FileRepository) moduleReleasePath(name, version string) string {
	return filepath.Join(repository.moduleReleaseDirectory(), name, version+".json")
}
func (repository *FileRepository) moduleInstallationPath(id string) string {
	return filepath.Join(repository.moduleInstallationDirectory(), id+".json")
}
func (repository *FileRepository) moduleAutoUpdatePolicyPath(module string) string {
	return filepath.Join(repository.moduleAutoUpdatePolicyDirectory(), module+".json")
}
func (repository *FileRepository) exportPipelinePath(id string) string {
	return filepath.Join(repository.exportPipelineDirectory(), id+".json")
}
func (repository *FileRepository) exportPipelineRevisionPath(id string, revision uint64) string {
	return filepath.Join(repository.exportPipelineRevisionDirectory(), id, fmt.Sprintf("%020d.json", revision))
}
func (repository *FileRepository) exportPipelineScheduleRunPath(id string, scheduledFor time.Time) string {
	return filepath.Join(repository.exportPipelineScheduleRunDirectory(), exportPipelineScheduleRunID(id, scheduledFor)+".json")
}
func (repository *FileRepository) environmentProfilePath(environment string) string {
	return filepath.Join(repository.environmentProfileDirectory(), environment+".json")
}
func (repository *FileRepository) environmentProfileRevisionPath(environment string, revision uint64) string {
	return filepath.Join(repository.environmentProfileRevisionDirectory(), environment, fmt.Sprintf("%020d.json", revision))
}
func (repository *FileRepository) labelDefinitionPath(id string) string {
	return filepath.Join(repository.labelDefinitionDirectory(), id+".json")
}
func (repository *FileRepository) labelDefinitionRevisionPath(id string, revision uint64) string {
	return filepath.Join(repository.labelDefinitionRevisionDirectory(), id, fmt.Sprintf("%020d.json", revision))
}
func (repository *FileRepository) catalogRetirementPath(product, version string) string {
	return filepath.Join(repository.catalogRetirementDirectory(), stableShortID(product+"\x00"+version)+".json")
}
func (repository *FileRepository) bundlePath(id string) string {
	return filepath.Join(repository.bundleDirectory(), id+".json")
}
func (repository *FileRepository) bundleImportPath(id string) string {
	return filepath.Join(repository.bundleImportDirectory(), id+".json")
}
func (repository *FileRepository) bundleTransferPath(id string) string {
	return filepath.Join(repository.bundleTransferDirectory(), id+".json")
}
func (repository *FileRepository) importedCatalogPath(environment string) string {
	return filepath.Join(repository.importedCatalogDirectory(), environment+".json")
}

func (repository *FileRepository) readBlobDescriptor(digest string) (BlobDescriptor, error) {
	if !digestPattern.MatchString(digest) {
		return BlobDescriptor{}, ErrNotFound
	}
	var descriptor BlobDescriptor
	if err := readJSON(repository.blobDescriptorPath(digest), &descriptor); err != nil {
		return BlobDescriptor{}, err
	}
	if descriptor.Digest != digest || descriptor.Size < 0 {
		return BlobDescriptor{}, fmt.Errorf("invalid descriptor for artifact %s", digest)
	}
	return descriptor, nil
}

func readJSON(path string, value any) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	contents = append(contents, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".write-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary file: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open repository directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync repository directory: %w", err)
	}
	return nil
}
