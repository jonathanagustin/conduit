package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	artifactapp "github.com/jonathanagustin/conduit/internal/artifacts/application"
	artifacts "github.com/jonathanagustin/conduit/internal/artifacts/domain"
	"github.com/robfig/cron/v3"
)

const (

	// VERIFIED against the published contract
	// (https://www.palantir.com/docs/apollo/export-import/create-pipeline/, reviewed 2026-08-07):
	//
	//	INCREMENTAL  "only information that has not been shipped by previous Bundles in the Export
	//	             Pipeline will be included in the new Bundle"
	//	SNAPSHOT     "the selector rules will be fully re-evaluated with each build. All Bundle
	//	             data ... will be included ... regardless of whether the information has been
	//	             shipped by a previous Bundle"
	//
	// Both hold here, and the incremental one only reads that way once both halves are seen
	// together: includeReleaseHistory makes an incremental build SELECT the whole Release history,
	// and changedBundleResources then ships only what the lineage baseline does not already carry.
	// Selecting wide and shipping narrow is what makes "not yet shipped" computable at all.
	//
	// DefaultCheckpointFrequency is ours. The docs establish that the frequency is configurable --
	// "the frequency at which to create checkpoint Bundles" -- and put no number on it.
	BundleStrategySnapshot     = "SNAPSHOT"
	BundleStrategyIncremental  = "INCREMENTAL"
	DefaultCheckpointFrequency = 10

	BundleSelectEnvironment = "ENVIRONMENT"
	BundleSelectModule      = "MODULE"
	BundleSelectProduct     = "PRODUCT"
	BundleSelectRelease     = "RELEASE"
	BundleSelectTeam        = "TEAM"

	// The three published strategies, and the five selection rules below are the five published
	// rule types -- Select Environments, Modules, Products, Releases, Teams
	// (https://www.palantir.com/docs/apollo/export-import/data-selection-rules/, reviewed
	// 2026-08-07). The taxonomy is VERIFIED; one of the three MEANINGS is not.
	//
	// MINIMAL is documented as "only the minimal set of Product Releases needed to satisfy upgrade
	// paths for each Product". It is not implemented: selectReleases never branches on it, so it
	// falls through to the latest-per-channel path and behaves exactly like ALL without history.
	// An operator choosing it for a stepwise upgrade gets the newest Release and none of the
	// intermediate ones the upgrade path needs, and the build reports nothing -- the Bundle is
	// simply unable to complete the upgrade at the far end.
	//
	// Also unenforced: the docs say ALL and MINIMAL require the Incremental build strategy. A
	// Snapshot pipeline may select either here.
	BundleReleasesNone    = "NONE"
	BundleReleasesAll     = "ALL"
	BundleReleasesMinimal = "MINIMAL"

	BundleLabelEnvironment    = "ENVIRONMENT"
	BundleLabelProductRelease = "PRODUCT_RELEASE"

	BundleResourceEnvironmentReport = "ENVIRONMENT_REPORT"

	ExportPipelineActive   = "ACTIVE"
	ExportPipelineArchived = "ARCHIVED"
	ExportPipelineDeleted  = "DELETED"

	ExportScheduleRunning   = "RUNNING"
	ExportScheduleSucceeded = "SUCCEEDED"
	ExportScheduleFailed    = "FAILED"
	DefaultExportCron       = "0 0 * * *"

	BundleImportTaskVerify      = "VERIFY_BUNDLE"
	BundleImportTaskArtifacts   = "STAGE_ARTIFACTS"
	BundleImportTaskCatalog     = "IMPORT_CATALOG"
	BundleImportTaskEnvironment = "APPLY_ENVIRONMENT_CHANGES"
	BundleImportTaskPending     = "PENDING"
	BundleImportTaskSucceeded   = "SUCCEEDED"
	BundleImportTaskFailed      = "FAILED"
	// BundleImportTaskSkipped is a task that never ran because an earlier one
	// failed. PENDING cannot say this: it reads as "queued", and on a terminal
	// import nothing is queued. Upstream's import visibly runs its later tasks
	// after a failure; this one stops, deliberately -- the catalog is applied
	// atomically per Environment -- so the tasks behind the failure need a
	// state that says they were abandoned rather than one that says they wait.
	//
	// INFERRED identifier. The named, ordered, individually-statused task list is capture-grounded
	// (docs/ui/evidence/failed-bundle-import-observation.md records a failed import showing eight
	// named tasks, two of them succeeding AFTER the failure), and the published docs confirm the concept without naming the
	// values -- they speak of viewing "import progress" and the "import stage that failed"
	// (https://www.palantir.com/docs/apollo/export-import/import-bundle/, reviewed 2026-08-07).
	// SKIPPED itself is ours, and so is the situation it describes: upstream's run continues past a
	// failure and has nothing to skip, while this import aborts, which is the divergence recorded
	// above.
	BundleImportTaskSkipped = "SKIPPED"
)

type BundleRepository interface {
	PutExportPipeline(context.Context, ExportPipeline) (ExportPipeline, error)
	GetExportPipeline(context.Context, string) (ExportPipeline, error)
	GetExportPipelineRevision(context.Context, string, uint64) (ExportPipeline, error)
	ListExportPipelines(context.Context) ([]ExportPipeline, error)
	ClaimExportPipelineScheduleRun(context.Context, ExportPipelineScheduleRun) (ExportPipelineScheduleRun, bool, error)
	CompleteExportPipelineScheduleRun(context.Context, string, time.Time, func(*ExportPipelineScheduleRun) error) (ExportPipelineScheduleRun, error)
	ListExportPipelineScheduleRuns(context.Context, string) ([]ExportPipelineScheduleRun, error)
	CreateBundle(context.Context, BundleEnvelope) (BundleEnvelope, error)
	GetBundle(context.Context, string) (BundleEnvelope, error)
	ListBundles(context.Context, string) ([]BundleEnvelope, error)
	CreateBundleImport(context.Context, BundleImport) (BundleImport, error)
	GetBundleImport(context.Context, string) (BundleImport, error)
	ListBundleImports(context.Context) ([]BundleImport, error)
	UpdateBundleImport(context.Context, string, func(*BundleImport) error) (BundleImport, error)
	PutImportedCatalog(context.Context, ImportedCatalog) (ImportedCatalog, error)
	GetImportedCatalog(context.Context, string) (ImportedCatalog, error)
	ListImportedCatalogs(context.Context) ([]ImportedCatalog, error)
	PutBundleTransferConfirmation(context.Context, BundleTransferConfirmation) (BundleTransferConfirmation, error)
	ListBundleTransferConfirmations(context.Context, string, string) ([]BundleTransferConfirmation, error)
}

type ExportPipeline struct {
	ID                             string                   `json:"id"`
	DisplayName                    string                   `json:"displayName"`
	BuildStrategy                  string                   `json:"buildStrategy"`
	CheckpointFrequency            int                      `json:"checkpointFrequency,omitempty"`
	Selections                     []BundleSelectionRule    `json:"selections"`
	ResourceTypes                  []string                 `json:"resourceTypes,omitempty"`
	IncludeFutureTypes             bool                     `json:"includeFutureResourceTypes,omitempty"`
	RequireSecurityPass            bool                     `json:"requireSecurityPass,omitempty"`
	IncludeArtifacts               bool                     `json:"includeArtifacts,omitempty"`
	RequireMalwarePass             bool                     `json:"requireMalwarePass,omitempty"`
	BuildDespiteMissingResources   bool                     `json:"buildDespiteMissingResources,omitempty"`
	ChunkSizeBytes                 int                      `json:"chunkSizeBytes,omitempty"`
	EnableLayerDedup               bool                     `json:"enableLayerDedup,omitempty"`
	ShareLayerDedupAcrossPipelines bool                     `json:"shareLayerDedupAcrossPipelines,omitempty"`
	RequiredLabels                 []BundleLabelRequirement `json:"requiredLabels,omitempty"`
	ExcludedLabels                 []BundleLabelRequirement `json:"excludedLabels,omitempty"`
	// TargetHubVersions records, per target Hub, what that Hub reports running.
	// It supplements the baseline derived from Bundle history so an incremental
	// Bundle stays applicable when the source's history and the target diverge.
	TargetHubVersions map[string]TargetHubVersions `json:"targetHubVersions,omitempty"`
	Schedule          *ExportPipelineSchedule      `json:"schedule,omitempty"`
	Status            string                       `json:"status"`
	ArchivedAt        time.Time                    `json:"archivedAt,omitempty"`
	DeletedAt         time.Time                    `json:"deletedAt,omitempty"`
	CreatedAt         time.Time                    `json:"createdAt"`
	UpdatedAt         time.Time                    `json:"updatedAt"`
	Revision          uint64                       `json:"revision"`
}

type ExportPipelineSchedule struct {
	Enabled                bool   `json:"enabled"`
	Cron                   string `json:"cron,omitempty"`
	TimeZone               string `json:"timeZone,omitempty"`
	DisablePreBuildPreview bool   `json:"disablePreBuildPreview,omitempty"`
}

type ExportPipelineScheduleRun struct {
	PipelineID       string    `json:"pipelineId"`
	PipelineRevision uint64    `json:"pipelineRevision"`
	ScheduledFor     time.Time `json:"scheduledFor"`
	Status           string    `json:"status"`
	ClaimedAt        time.Time `json:"claimedAt"`
	FinishedAt       time.Time `json:"finishedAt,omitempty"`
	BundleIDs        []string  `json:"bundleIds,omitempty"`
	Failure          string    `json:"failure,omitempty"`
	Revision         uint64    `json:"revision"`
}

type ExportPipelinePreview struct {
	PipelineID       string    `json:"pipelineId"`
	PipelineRevision uint64    `json:"pipelineRevision"`
	GeneratedAt      time.Time `json:"generatedAt"`
	Added            []string  `json:"added,omitempty"`
	Updated          []string  `json:"updated,omitempty"`
	Removed          []string  `json:"removed,omitempty"`
	Unchanged        []string  `json:"unchanged,omitempty"`
	SecurityOutcome  string    `json:"securityOutcome"`
	// What the build this previews would advise. The point of a pre-build
	// preview is to find out before building; an omission is exactly the kind
	// of thing worth finding out then.
	Advisories []BundleAdvisory `json:"advisories,omitempty"`
}

func validateExportPipelineScheduleRun(run ExportPipelineScheduleRun) error {
	if !identifierPattern.MatchString(run.PipelineID) || run.PipelineRevision == 0 || run.Revision == 0 ||
		run.ScheduledFor.IsZero() || !run.ScheduledFor.Equal(run.ScheduledFor.UTC().Truncate(time.Minute)) ||
		run.ClaimedAt.IsZero() {
		return ErrConflict
	}
	switch run.Status {
	case ExportScheduleRunning:
		if !run.FinishedAt.IsZero() || len(run.BundleIDs) != 0 || run.Failure != "" {
			return ErrConflict
		}
	case ExportScheduleSucceeded:
		if run.FinishedAt.IsZero() || len(run.BundleIDs) == 0 || run.Failure != "" {
			return ErrConflict
		}
	case ExportScheduleFailed:
		if run.FinishedAt.IsZero() || len(run.BundleIDs) != 0 || strings.TrimSpace(run.Failure) == "" {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}

type BundleLabelRequirement struct {
	ResourceTypes []string `json:"resourceTypes"`
	Name          string   `json:"name"`
	Values        []string `json:"values,omitempty"`
}

type BundleSelectionRule struct {
	Type            string   `json:"type"`
	IDs             []string `json:"ids"`
	ReleaseStrategy string   `json:"releaseStrategy,omitempty"`
	ReleaseChannels []string `json:"releaseChannels,omitempty"`
}

// BundleAdvisory records something a Bundle build did not do, on a build that
// otherwise succeeded. A build therefore has three outcomes rather than two:
// it succeeded, it succeeded with advisories, or it failed. Kind is a closed
// set so a reader can group and act on advisories; Message alone is prose, and
// prose has to be parsed back apart to be useful.

type BundlePreview struct {
	BundleID        string   `json:"bundleId"`
	PipelineID      string   `json:"pipelineId"`
	Kind            string   `json:"kind"`
	DependsOn       string   `json:"dependsOn,omitempty"`
	Added           []string `json:"added,omitempty"`
	Updated         []string `json:"updated,omitempty"`
	Removed         []string `json:"removed,omitempty"`
	Unchanged       []string `json:"unchanged,omitempty"`
	SecurityOutcome string   `json:"securityOutcome"`
}

type BundleImport struct {
	ID              string             `json:"id"`
	BundleID        string             `json:"bundleId"`
	ManifestDigest  string             `json:"manifestDigest"`
	Environment     string             `json:"environment"`
	ChangeRequestID string             `json:"changeRequestId,omitempty"`
	RequestedBy     string             `json:"requestedBy"`
	Status          string             `json:"status"`
	Preview         BundlePreview      `json:"preview"`
	Error           string             `json:"error,omitempty"`
	Tasks           []BundleImportTask `json:"tasks"`
	RequestedAt     time.Time          `json:"requestedAt"`
	ImportedAt      time.Time          `json:"importedAt,omitempty"`
	Revision        uint64             `json:"revision"`
}

// BundleImportTask is a bounded stage result. It contains only resource
// coordinates and redacted diagnostics, never artifact bodies or credentials.
// Public task presentation reviewed 2026-08-02:
// https://www.palantir.com/docs/apollo/export-import/import-bundle/
type BundleImportTask struct {
	Sequence   int       `json:"sequence"`
	Stage      string    `json:"stage"`
	Status     string    `json:"status"`
	Resources  []string  `json:"resources,omitempty"`
	Failure    string    `json:"failure,omitempty"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

type ImportedCatalog struct {
	Environment             string                    `json:"environment"`
	Resources               map[string]BundleResource `json:"resources"`
	BundleIDs               []string                  `json:"bundleIds"`
	RelayedReport           *EnvironmentReport        `json:"relayedReport,omitempty"`
	RelayedReportBundleID   string                    `json:"relayedReportBundleId,omitempty"`
	RelayedReportSigningKey string                    `json:"relayedReportSigningKeyId,omitempty"`
	UpdatedAt               time.Time                 `json:"updatedAt"`
	Revision                uint64                    `json:"revision"`
}

type BundleService struct {
	repository Repository
	privateKey ed25519.PrivateKey
	trusted    map[string]ed25519.PublicKey
	now        func() time.Time
}

func NewBundleService(repository Repository, privateKey ed25519.PrivateKey, trusted map[string]ed25519.PublicKey) (*BundleService, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("an Ed25519 Bundle signing key is required")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keys := make(map[string]ed25519.PublicKey, len(trusted)+1)
	for id, key := range trusted {
		if len(key) != ed25519.PublicKeySize || id != bundleSigningKeyID(key) {
			return nil, fmt.Errorf("invalid trusted Bundle signing key %q", id)
		}
		keys[id] = append(ed25519.PublicKey(nil), key...)
	}
	keys[bundleSigningKeyID(publicKey)] = append(ed25519.PublicKey(nil), publicKey...)
	return &BundleService{repository: repository, privateKey: privateKey, trusted: keys, now: time.Now}, nil
}

func (service *BundleService) PublicSigningKey() (string, string) {
	key := service.privateKey.Public().(ed25519.PublicKey)
	return bundleSigningKeyID(key), base64.RawStdEncoding.EncodeToString(key)
}

func (service *BundleService) PutPipeline(ctx context.Context, pipeline ExportPipeline) (ExportPipeline, error) {
	if pipeline.BuildStrategy == BundleStrategyIncremental && pipeline.CheckpointFrequency == 0 {
		pipeline.CheckpointFrequency = DefaultCheckpointFrequency
	}
	if pipeline.Schedule != nil {
		pipeline.Schedule.Cron = strings.TrimSpace(pipeline.Schedule.Cron)
		if pipeline.Schedule.Cron == "" {
			pipeline.Schedule.Cron = DefaultExportCron
		}
		pipeline.Schedule.TimeZone = strings.TrimSpace(pipeline.Schedule.TimeZone)
		if pipeline.Schedule.TimeZone == "" {
			pipeline.Schedule.TimeZone = "UTC"
		}
	}
	if errs := validateExportPipeline(pipeline); len(errs) > 0 {
		return ExportPipeline{}, ValidationErrors(errs)
	}
	now := service.now().UTC()
	existing, err := service.repository.GetExportPipeline(ctx, pipeline.ID)
	switch {
	case err == nil:
		if !exportPipelineIsActive(existing.Status) {
			return ExportPipeline{}, fmt.Errorf("%w: archived or deleted Export Pipeline cannot be modified", ErrConflict)
		}
		if pipeline.Revision != existing.Revision {
			return ExportPipeline{}, fmt.Errorf("%w: Export Pipeline revision is stale", ErrConflict)
		}
		pipeline.CreatedAt = existing.CreatedAt
		pipeline.Status = ExportPipelineActive
		pipeline.Revision++
	case errors.Is(err, ErrNotFound):
		pipeline.CreatedAt = now
		pipeline.Status = ExportPipelineActive
		pipeline.Revision = 1
	case err != nil:
		return ExportPipeline{}, err
	}
	pipeline.UpdatedAt = now
	return service.repository.PutExportPipeline(ctx, pipeline)
}

// Empty status is the on-disk representation written before Pipeline lifecycle
// was introduced. Treat it as ACTIVE so an existing repository can be upgraded
// without rewriting history; every subsequent write persists the explicit state.
func exportPipelineIsActive(status string) bool {
	return status == "" || status == ExportPipelineActive
}

// SetPipelineArchived records a reversible lifecycle transition as a new,
// immutable Pipeline revision. Deleted Pipelines remain tombstoned so an ID and
// its historical settings can never be silently reused.
// Public source reviewed 2026-08-02:
// https://www.palantir.com/docs/apollo/export-import/create-pipeline/
func (service *BundleService) SetPipelineArchived(ctx context.Context, id string, archived bool) (ExportPipeline, error) {
	pipeline, err := service.repository.GetExportPipeline(ctx, id)
	if err != nil {
		return ExportPipeline{}, err
	}
	if pipeline.Status == ExportPipelineDeleted {
		return ExportPipeline{}, ErrInvalidTransition
	}
	now := service.now().UTC()
	target := ExportPipelineActive
	if archived {
		target = ExportPipelineArchived
	}
	if pipeline.Status == target || (!archived && exportPipelineIsActive(pipeline.Status)) {
		pipeline.Status = ExportPipelineActive
		return pipeline, nil
	}
	pipeline.Status = target
	pipeline.ArchivedAt = time.Time{}
	if archived {
		pipeline.ArchivedAt = now
	}
	pipeline.UpdatedAt = now
	pipeline.Revision++
	return service.repository.PutExportPipeline(ctx, pipeline)
}

// exportPipelineProposalID names a lifecycle proposal by the pipeline AND the
// state it was proposed against.
//
// A Change Request id is a digest of TargetKind and TargetID alone (see
// evaluateChangeRequest), and the create guard returns whatever record already
// occupies that id regardless of its status. With a bare pipeline id as the
// target, the first archive of a pipeline owns that id for good: once it
// reaches a terminal status -- cancelled, rejected, or errored because no
// approval policy was enabled yet -- every later archive of that pipeline is
// answered with the stale terminal record, nothing is applied, and the pipeline
// can never be archived again. Reopen is author-gated, so a departed author
// leaves no recourse to any principal. Folding the observed revision and a
// digest of the lifecycle state in gives a proposal against a pipeline that has
// since moved its own record, the way a label definition proposal carries its
// revision and content digest.
//
// Proposals against an UNMOVED pipeline still collapse onto one record, which is
// what the guard is for: SetPipelineArchived and DeletePipeline are no-ops when
// the pipeline already holds the target state, so a repeat with nothing to do is
// a retry, not a second decision.
func exportPipelineProposalID(pipelineID string, pipeline ExportPipeline) string {
	state, _ := json.Marshal(struct {
		Status     string    `json:"status"`
		Revision   uint64    `json:"revision"`
		ArchivedAt time.Time `json:"archivedAt"`
		DeletedAt  time.Time `json:"deletedAt"`
	}{pipeline.Status, pipeline.Revision, pipeline.ArchivedAt, pipeline.DeletedAt})
	return pipelineID + ":" + strconv.FormatUint(pipeline.Revision, 10) + ":" + stableShortID(string(state))
}

// exportPipelineLifecycleTarget recovers the pipeline a lifecycle Change Request
// acts on. TargetID carries the proposal identity above, not a bare pipeline id,
// so the pipeline comes from the change payload's pipelineRid -- the field
// change_types.go requires of every pipeline lifecycle change. Records written
// before the state was folded in have TargetID == the pipeline id, and that is
// the fallback, so an approval arriving today against a request stored yesterday
// still archives the right pipeline instead of none.
func exportPipelineLifecycleTarget(request ChangeRequest) string {
	for _, change := range request.Changes {
		if rid, ok := change.Request["pipelineRid"].(string); ok && strings.TrimSpace(rid) != "" {
			return rid
		}
	}
	return request.TargetID
}

func (service *BundleService) DeletePipeline(ctx context.Context, id string) (ExportPipeline, error) {
	pipeline, err := service.repository.GetExportPipeline(ctx, id)
	if err != nil {
		return ExportPipeline{}, err
	}
	if pipeline.Status == ExportPipelineDeleted {
		return pipeline, nil
	}
	if pipeline.Status != ExportPipelineArchived {
		return ExportPipeline{}, fmt.Errorf("%w: Export Pipeline must be archived before deletion", ErrInvalidTransition)
	}
	now := service.now().UTC()
	pipeline.Status, pipeline.DeletedAt, pipeline.UpdatedAt = ExportPipelineDeleted, now, now
	pipeline.Revision++
	return service.repository.PutExportPipeline(ctx, pipeline)
}

func validateExportPipeline(pipeline ExportPipeline) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	if !identifierPattern.MatchString(pipeline.ID) {
		add("id", "must be a stable identifier")
	}
	if strings.TrimSpace(pipeline.DisplayName) == "" {
		add("displayName", "must not be empty")
	}
	if pipeline.BuildStrategy != BundleStrategySnapshot && pipeline.BuildStrategy != BundleStrategyIncremental {
		add("buildStrategy", "must be SNAPSHOT or INCREMENTAL")
	}
	if pipeline.CheckpointFrequency < 0 || pipeline.CheckpointFrequency > 10000 {
		add("checkpointFrequency", "must be between 0 and 10000")
	}
	if pipeline.ChunkSizeBytes < 0 || pipeline.ChunkSizeBytes > 64*1024*1024 {
		add("chunkSizeBytes", "must be between 0 and 67108864")
	}
	if pipeline.RequireMalwarePass && !pipeline.IncludeArtifacts {
		add("requireMalwarePass", "requires includeArtifacts")
	}
	if pipeline.EnableLayerDedup && !pipeline.IncludeArtifacts {
		add("enableLayerDedup", "requires includeArtifacts")
	}
	if pipeline.ShareLayerDedupAcrossPipelines && !pipeline.EnableLayerDedup {
		add("shareLayerDedupAcrossPipelines", "requires enableLayerDedup")
	}
	if pipeline.BuildStrategy == BundleStrategySnapshot && pipeline.CheckpointFrequency != 0 {
		add("checkpointFrequency", "is only valid for INCREMENTAL Pipelines")
	}
	if pipeline.Schedule != nil {
		if _, err := time.LoadLocation(pipeline.Schedule.TimeZone); err != nil {
			add("schedule.timeZone", "must be an IANA timezone")
		} else if _, err := parseExportPipelineSchedule(*pipeline.Schedule); err != nil {
			add("schedule.cron", "must be a five-field cron expression")
		}
	}
	if len(pipeline.Selections) == 0 {
		add("selections", "must contain at least one data selection rule")
	}
	for index, rule := range pipeline.Selections {
		path := fmt.Sprintf("selections[%d]", index)
		if rule.Type != BundleSelectEnvironment && rule.Type != BundleSelectModule &&
			rule.Type != BundleSelectProduct && rule.Type != BundleSelectRelease && rule.Type != BundleSelectTeam {
			add(path+".type", "must be ENVIRONMENT, MODULE, PRODUCT, RELEASE, or TEAM")
		}
		if len(rule.IDs) == 0 {
			add(path+".ids", "must not be empty")
		}
		if rule.Type != BundleSelectRelease && rule.Type != BundleSelectTeam && rule.ReleaseStrategy != BundleReleasesNone &&
			rule.ReleaseStrategy != BundleReleasesAll && rule.ReleaseStrategy != BundleReleasesMinimal {
			add(path+".releaseStrategy", "must be NONE, ALL, or MINIMAL")
		}
		if (rule.Type == BundleSelectRelease || rule.Type == BundleSelectTeam) &&
			(rule.ReleaseStrategy != "" || len(rule.ReleaseChannels) > 0) {
			add(path+".releaseStrategy", "is not applicable to exact Release or Team selection")
		}
		if (rule.Type == BundleSelectProduct || rule.Type == BundleSelectModule) &&
			rule.ReleaseStrategy != BundleReleasesNone && len(rule.ReleaseChannels) == 0 {
			add(path+".releaseChannels", "must select at least one Release Channel")
		}
	}
	validateBundleLabelRequirements("requiredLabels", pipeline.RequiredLabels, &errs)
	validateBundleLabelRequirements("excludedLabels", pipeline.ExcludedLabels, &errs)
	return errs
}

func parseExportPipelineSchedule(configuration ExportPipelineSchedule) (cron.Schedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return parser.Parse("CRON_TZ=" + configuration.TimeZone + " " + configuration.Cron)
}

// PreviewExportPipeline refreshes the unsigned projection of resources that a
// build would add, update, remove, or retain. It deliberately does not package
// artifacts, advance Bundle lineage, or create approval/execution evidence.
func (service *BundleService) PreviewExportPipeline(ctx context.Context, pipelineID string) (ExportPipelinePreview, error) {
	pipeline, err := service.repository.GetExportPipeline(ctx, pipelineID)
	if err != nil {
		return ExportPipelinePreview{}, err
	}
	if !exportPipelineIsActive(pipeline.Status) {
		return ExportPipelinePreview{}, fmt.Errorf("%w: Export Pipeline is not active", ErrConflict)
	}
	if pipeline.Schedule != nil && pipeline.Schedule.DisablePreBuildPreview {
		return ExportPipelinePreview{}, fmt.Errorf("%w: pre-build preview is disabled", ErrConflict)
	}
	selection, err := service.selectResources(ctx, pipeline, false)
	if err != nil {
		return ExportPipelinePreview{}, err
	}
	selected, security, advisories := selection.Resources, selection.Security, selection.Advisories
	bundles, err := service.repository.ListBundles(ctx, pipeline.ID)
	if err != nil {
		return ExportPipelinePreview{}, err
	}
	lineage := make([]BundleEnvelope, 0, len(bundles))
	for _, bundle := range bundles {
		if bundle.Manifest.TargetHub == "" && !bundle.Manifest.LineageIndependent {
			lineage = append(lineage, bundle)
		}
	}
	baseline, _ := bundleBaseline(lineage)
	preview := ExportPipelinePreview{
		PipelineID: pipeline.ID, PipelineRevision: pipeline.Revision, GeneratedAt: service.now().UTC(),
		SecurityOutcome: security.Outcome, Advisories: advisories,
	}
	current := make(map[string]BundleResource, len(selected))
	for _, resource := range selected {
		key := resource.Type + ":" + resource.ID
		current[key] = resource
		previous, exists := baseline[key]
		switch {
		case !exists:
			preview.Added = append(preview.Added, key)
		case previous.Digest != resource.Digest:
			preview.Updated = append(preview.Updated, key)
		default:
			preview.Unchanged = append(preview.Unchanged, key)
		}
	}
	for key := range baseline {
		if _, exists := current[key]; !exists {
			preview.Removed = append(preview.Removed, key)
		}
	}
	sort.Strings(preview.Added)
	sort.Strings(preview.Updated)
	sort.Strings(preview.Removed)
	sort.Strings(preview.Unchanged)
	return preview, nil
}

func exportPipelineScheduleDue(pipeline ExportPipeline, scheduledFor time.Time) bool {
	if pipeline.Schedule == nil || !pipeline.Schedule.Enabled || !exportPipelineIsActive(pipeline.Status) {
		return false
	}
	schedule, err := parseExportPipelineSchedule(*pipeline.Schedule)
	if err != nil {
		return false
	}
	slot := scheduledFor.UTC().Truncate(time.Minute)
	return schedule.Next(slot.Add(-time.Minute)).Equal(slot)
}

// RunDueExportPipelines durably claims each due Pipeline minute before building.
// A restarted or concurrent scheduler observes the same claim and cannot create
// a second build for that slot.
func (service *BundleService) RunDueExportPipelines(
	ctx context.Context, scheduledFor time.Time,
) ([]ExportPipelineScheduleRun, error) {
	pipelines, err := service.repository.ListExportPipelines(ctx)
	if err != nil {
		return nil, err
	}
	slot := scheduledFor.UTC().Truncate(time.Minute)
	runs := make([]ExportPipelineScheduleRun, 0)
	errs := make([]error, 0)
	for _, pipeline := range pipelines {
		if !exportPipelineScheduleDue(pipeline, slot) {
			continue
		}
		run := ExportPipelineScheduleRun{
			PipelineID: pipeline.ID, PipelineRevision: pipeline.Revision, ScheduledFor: slot,
			Status: ExportScheduleRunning, ClaimedAt: service.now().UTC(),
		}
		// The claimed record is discarded deliberately: CompleteExportPipelineScheduleRun
		// below returns the same row with the claim fields already durable.
		_, claimed, claimErr := service.repository.ClaimExportPipelineScheduleRun(ctx, run)
		if claimErr != nil {
			errs = append(errs, fmt.Errorf("claim scheduled Export Pipeline %s: %w", pipeline.ID, claimErr))
			continue
		}
		if !claimed {
			continue
		}
		built, buildErr := service.BuildRevisionForTarget(ctx, pipeline.ID, pipeline.Revision, "", false)
		run, completeErr := service.repository.CompleteExportPipelineScheduleRun(ctx, pipeline.ID, slot, func(record *ExportPipelineScheduleRun) error {
			record.FinishedAt = service.now().UTC()
			if buildErr != nil {
				record.Status = ExportScheduleFailed
				record.Failure = boundedExportScheduleFailure(buildErr)
				return nil
			}
			record.Status = ExportScheduleSucceeded
			for _, bundle := range built {
				record.BundleIDs = append(record.BundleIDs, bundle.Manifest.ID)
			}
			return nil
		})
		if completeErr != nil {
			errs = append(errs, fmt.Errorf("complete scheduled Export Pipeline %s: %w", pipeline.ID, completeErr))
			continue
		}
		runs = append(runs, run)
		if buildErr != nil {
			errs = append(errs, fmt.Errorf("build scheduled Export Pipeline %s: %w", pipeline.ID, buildErr))
		}
	}
	return runs, errors.Join(errs...)
}

func boundedExportScheduleFailure(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

func (service *BundleService) RunExportScheduler(ctx context.Context, reportError func(error)) {
	if _, err := service.RunDueExportPipelines(ctx, service.now()); err != nil && reportError != nil {
		reportError(err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := service.RunDueExportPipelines(ctx, now); err != nil && reportError != nil {
				reportError(err)
			}
		}
	}
}

func validateBundleLabelRequirements(path string, requirements []BundleLabelRequirement, errs *[]ValidationError) {
	for index, requirement := range requirements {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if strings.TrimSpace(requirement.Name) == "" {
			*errs = append(*errs, ValidationError{Path: prefix + ".name", Message: "must not be empty"})
		}
		if len(requirement.ResourceTypes) == 0 {
			*errs = append(*errs, ValidationError{Path: prefix + ".resourceTypes", Message: "must not be empty"})
		}
		seenTypes := map[string]struct{}{}
		for _, resourceType := range requirement.ResourceTypes {
			if resourceType != BundleLabelEnvironment && resourceType != BundleLabelProductRelease {
				*errs = append(*errs, ValidationError{Path: prefix + ".resourceTypes", Message: "must contain ENVIRONMENT or PRODUCT_RELEASE"})
			}
			if _, exists := seenTypes[resourceType]; exists {
				*errs = append(*errs, ValidationError{Path: prefix + ".resourceTypes", Message: "must be unique"})
			}
			seenTypes[resourceType] = struct{}{}
		}
		seenValues := map[string]struct{}{}
		for _, value := range requirement.Values {
			if strings.TrimSpace(value) == "" {
				*errs = append(*errs, ValidationError{Path: prefix + ".values", Message: "must not contain empty values"})
			}
			if _, exists := seenValues[value]; exists {
				*errs = append(*errs, ValidationError{Path: prefix + ".values", Message: "must be unique"})
			}
			seenValues[value] = struct{}{}
		}
	}
}

func (service *BundleService) Build(ctx context.Context, pipelineID string) ([]BundleEnvelope, error) {
	return service.BuildForTarget(ctx, pipelineID, "")
}

// BuildForTarget creates a target-specific Bundle. Layer omission is permitted
// only for explicitly confirmed transfers to this exact target Hub.
func (service *BundleService) BuildForTarget(ctx context.Context, pipelineID, targetHub string) ([]BundleEnvelope, error) {
	return service.BuildRevisionForTarget(ctx, pipelineID, 0, targetHub, false)
}

// BuildRevisionForTarget evaluates an immutable historical Pipeline definition.
// Historical and standalone builds are snapshots and never mutate or depend on
// the current incremental lineage.
// Public source reviewed 2026-08-02:
// https://www.palantir.com/docs/apollo/export-import/create-pipeline/
func (service *BundleService) BuildRevisionForTarget(
	ctx context.Context, pipelineID string, revision uint64, targetHub string, standalone bool,
) ([]BundleEnvelope, error) {
	current, err := service.repository.GetExportPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	if !exportPipelineIsActive(current.Status) {
		return nil, fmt.Errorf("%w: Export Pipeline is not active", ErrConflict)
	}
	pipeline := current
	if revision != 0 && revision != current.Revision {
		pipeline, err = service.repository.GetExportPipelineRevision(ctx, pipelineID, revision)
		if err != nil {
			return nil, err
		}
	}
	alternate := standalone || pipeline.Revision != current.Revision
	allExisting, err := service.repository.ListBundles(ctx, pipeline.ID)
	if err != nil {
		return nil, err
	}
	series := make([]BundleEnvelope, 0, len(allExisting))
	for _, candidate := range allExisting {
		if candidate.Manifest.TargetHub == targetHub {
			series = append(series, candidate)
		}
	}
	allExisting = series
	existing := make([]BundleEnvelope, 0, len(allExisting))
	for _, candidate := range allExisting {
		if !candidate.Manifest.LineageIndependent {
			existing = append(existing, candidate)
		}
	}
	includeReleaseHistory := !alternate && pipeline.BuildStrategy == BundleStrategyIncremental && len(existing) > 0
	selection, err := service.selectResources(ctx, pipeline, includeReleaseHistory)
	if err != nil {
		return nil, err
	}
	selected, releases, security := selection.Resources, selection.Releases, selection.Security
	if pipeline.RequireSecurityPass && security.Outcome != "PASS" {
		return nil, fmt.Errorf("%w: Bundle security report outcome is %s", ErrConflict, security.Outcome)
	}
	if pipeline.RequireMalwarePass {
		for _, assessment := range security.ArtifactAssessments {
			if assessment.VirusStatus != "PASS" {
				return nil, fmt.Errorf("%w: malware evidence is incomplete for %s", ErrConflict, assessment.ArtifactDigest)
			}
		}
	}
	layers, chunks, err := service.packageArtifacts(ctx, pipeline, targetHub, releases)
	if err != nil {
		return nil, err
	}
	sequence := uint64(1)
	if len(allExisting) > 0 {
		sequence = allExisting[len(allExisting)-1].Manifest.Sequence + 1
	}
	kind := BundleSnapshot
	dependsOn := ""
	resources := selected
	if !alternate && pipeline.BuildStrategy == BundleStrategyIncremental && len(existing) > 0 {
		kind = BundleIncremental
		baseline, last := bundleBaseline(existing)
		baseline = targetHubReleaseBaseline(baseline, releases, pipeline.TargetHubVersions[targetHub])
		dependsOn = last
		resources = changedBundleResources(selected, baseline)
	}
	bundle, err := service.signBundle(pipeline, sequence, kind, dependsOn, "", targetHub, standalone, alternate, resources, layers, chunks, security, selection.Advisories)
	if err != nil {
		return nil, err
	}
	bundle, err = service.repository.CreateBundle(ctx, bundle)
	if err != nil {
		return nil, err
	}
	result := []BundleEnvelope{bundle}
	if kind == BundleIncremental && pipeline.CheckpointFrequency > 0 &&
		incrementalsSinceCheckpoint(existing)+1 >= pipeline.CheckpointFrequency {
		checkpoint, err := service.signBundle(
			pipeline, sequence+1, BundleCheckpoint, "", bundle.Manifest.ID, targetHub, false, false, selected, layers, chunks, security, selection.Advisories,
		)
		if err != nil {
			return nil, err
		}
		checkpoint, err = service.repository.CreateBundle(ctx, checkpoint)
		if err != nil {
			return nil, err
		}
		result = append(result, checkpoint)
	}
	return result, nil
}

// signBundle translates this package's vocabulary into Artifacts' and hands
// over. The pipeline is the only thing Artifacts must not see: it belongs to
// Delivery, and a Bundle format that knew about pipelines could not be reused
// by anything that does not have one.
func (service *BundleService) signBundle(
	pipeline ExportPipeline, sequence uint64, kind, dependsOn, checkpointOf, targetHub string, standalone, lineageIndependent bool,
	resources []BundleResource, layers []BundleArtifactLayer, chunks []BundleChunk, security BundleSecurityReport,
	advisories []BundleAdvisory,
) (BundleEnvelope, error) {
	return artifacts.Sign(
		service.privateKey,
		artifacts.Origin{
			PipelineID:         pipeline.ID,
			PipelineRevision:   pipeline.Revision,
			Sequence:           sequence,
			Kind:               kind,
			DependsOn:          dependsOn,
			CheckpointOf:       checkpointOf,
			TargetHub:          targetHub,
			Standalone:         standalone,
			LineageIndependent: lineageIndependent,
			ResourceTypes:      pipeline.ResourceTypes,
		},
		service.now(),
		resources, layers, chunks, security, advisories,
	)
}

// bundleSelection is what a Pipeline's selection rules resolved to.
type bundleSelection struct {
	Resources  []BundleResource
	Releases   map[string]Release
	Security   BundleSecurityReport
	Advisories []BundleAdvisory
}

func (service *BundleService) selectResources(
	ctx context.Context, pipeline ExportPipeline, includeReleaseHistory bool,
) (bundleSelection, error) {
	plans, err := service.repository.ListPlans(ctx, "")
	if err != nil {
		return bundleSelection{}, err
	}
	states := latestDesiredStates(plans)
	catalog := make(map[string]Release)
	// Recalls travel with the Environments that declared them, and the same
	// recall can appear in several desired states, so they are gathered by ID.
	// releaseRecalled does not scope by Environment -- a recall stops a Release
	// being selected for a new installation anywhere -- so which state a recall
	// came from does not change what it excludes here.
	recallsByID := make(map[string]Recall)
	for _, state := range states {
		for _, release := range state.Releases {
			catalog[release.Product+":"+release.Version] = release
		}
		for _, recall := range state.Recalls {
			recallsByID[recall.ID] = recall
		}
	}
	recalls := make([]Recall, 0, len(recallsByID))
	for _, recall := range recallsByID {
		recalls = append(recalls, recall)
	}
	resources := make(map[string]BundleResource)
	selectedReleases := make(map[string]Release)
	// Candidates for an advisory: Products a rule asked for and came up empty
	// on. Whether one is really missing from the Bundle is not decided here,
	// because a later rule may still supply it.
	var unmet []string
	for _, rule := range pipeline.Selections {
		switch rule.Type {
		case BundleSelectEnvironment:
			for _, id := range rule.IDs {
				state, exists := states[id]
				if !exists {
					if pipeline.BuildDespiteMissingResources {
						continue
					}
					return bundleSelection{}, fmt.Errorf("%w: Environment %s is absent from the catalog", ErrNotFound, id)
				}
				if !bundleLabelsAllow(BundleLabelEnvironment, state.Environment.Labels, pipeline) {
					continue
				}
				addBundleResource(resources, "ENVIRONMENT", id, state.Environment)
				if report, reportErr := service.repository.GetEnvironmentReport(ctx, id); reportErr == nil {
					addBundleResource(resources, BundleResourceEnvironmentReport, id, report)
				} else if !errors.Is(reportErr, ErrNotFound) {
					return bundleSelection{}, reportErr
				}
				products := make([]string, 0, len(state.Entities))
				for _, entity := range state.Entities {
					products = append(products, entity.Product)
				}
				unmet = append(unmet, selectReleases(catalog, recalls, selectedReleases, products, rule.ReleaseChannels, rule.ReleaseStrategy, includeReleaseHistory)...)
			}
		case BundleSelectProduct:
			unmet = append(unmet, selectReleases(catalog, recalls, selectedReleases, rule.IDs, rule.ReleaseChannels, rule.ReleaseStrategy, includeReleaseHistory)...)
		case BundleSelectRelease:
			for _, coordinate := range rule.IDs {
				release, exists := catalog[coordinate]
				if !exists {
					if pipeline.BuildDespiteMissingResources {
						continue
					}
					return bundleSelection{}, fmt.Errorf("%w: Product Release %s is absent from the catalog", ErrNotFound, coordinate)
				}
				selectedReleases[coordinate] = release
			}
		case BundleSelectModule:
			for _, name := range rule.IDs {
				releases, err := service.repository.ListModuleReleases(ctx, name)
				if err != nil {
					return bundleSelection{}, err
				}
				if len(releases) == 0 {
					return bundleSelection{}, fmt.Errorf("%w: Module %s is absent from the catalog", ErrNotFound, name)
				}
				latest := releases[len(releases)-1]
				addBundleResource(resources, "MODULE_RELEASE", latest.Name+":"+latest.Version, latest)
				products := make([]string, 0, len(latest.Lock.Entities))
				for _, entity := range latest.Lock.Entities {
					products = append(products, entity.ProductID)
				}
				unmet = append(unmet, selectReleases(catalog, recalls, selectedReleases, products, rule.ReleaseChannels, rule.ReleaseStrategy, includeReleaseHistory)...)
			}
		case BundleSelectTeam:
			for _, name := range rule.IDs {
				team, err := service.repository.GetTeam(ctx, name)
				if err != nil {
					return bundleSelection{}, err
				}
				addBundleResource(resources, "TEAM", name, team)
			}
		}
	}
	for coordinate, release := range selectedReleases {
		if !bundleLabelsAllow(BundleLabelProductRelease, effectiveReleaseLabels(release), pipeline) {
			delete(selectedReleases, coordinate)
		}
	}
	if err := closeBundleReleaseDependencies(catalog, selectedReleases, recalls, pipeline.BuildDespiteMissingResources); err != nil {
		return bundleSelection{}, err
	}
	for coordinate, release := range selectedReleases {
		if !bundleLabelsAllow(BundleLabelProductRelease, effectiveReleaseLabels(release), pipeline) {
			return bundleSelection{}, fmt.Errorf(
				"%w: dependency Product Release %s does not satisfy Pipeline label requirements", ErrConflict, coordinate,
			)
		}
	}
	security := buildBundleSecurityReport(selectedReleases, service.now().UTC())
	for coordinate, release := range selectedReleases {
		addBundleResource(resources, "PRODUCT_RELEASE", coordinate, release)
	}
	result := make([]BundleResource, 0, len(resources))
	for _, resource := range resources {
		result = append(result, resource)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].ID < result[j].ID
	})
	return bundleSelection{
		Resources: result, Releases: selectedReleases, Security: security,
		Advisories: append(unimplementedStrategyAdvisories(pipeline), noReleaseAdvisories(unmet, selectedReleases)...),
	}, nil
}

// unimplementedStrategyAdvisories names the selection rules that asked for a
// Release strategy this Hub validates but does not honour. It is deliberately
// derived from the Pipeline rather than from what was selected: the whole
// problem is that the selection looks the same either way.
func unimplementedStrategyAdvisories(pipeline ExportPipeline) []BundleAdvisory {
	seen := map[string]struct{}{}
	subjects := make([]string, 0, len(pipeline.Selections))
	add := func(subject string) {
		if _, duplicate := seen[subject]; duplicate {
			return
		}
		seen[subject] = struct{}{}
		subjects = append(subjects, subject)
	}
	for _, rule := range pipeline.Selections {
		if rule.ReleaseStrategy != BundleReleasesMinimal {
			continue
		}
		if len(rule.IDs) == 0 {
			add(rule.Type)
			continue
		}
		for _, id := range rule.IDs {
			add(rule.Type + ":" + id)
		}
	}
	if len(subjects) == 0 {
		return nil
	}
	sort.Strings(subjects)
	return []BundleAdvisory{{
		Kind: BundleAdvisoryUnimplementedStrategy,
		Message: "the following selection rules asked for the MINIMAL Release strategy, which this Hub " +
			"accepts but does not implement; they were built as if they had asked for the latest Release " +
			"on each Release Channel, so intermediate Releases an upgrade path needs are not in this Bundle",
		Subjects: subjects,
	}}
}

// noReleaseAdvisories keeps the candidates the finished Bundle does not in fact
// contain. Only a Product some rule came up empty on is a candidate at all --
// which is what keeps a Release the Pipeline's own label filters dropped from
// being reported as though its Release Channels were the problem.
func noReleaseAdvisories(unmet []string, selected map[string]Release) []BundleAdvisory {
	if len(unmet) == 0 {
		return nil
	}
	present := make(map[string]struct{}, len(selected))
	for _, release := range selected {
		present[release.Product] = struct{}{}
	}
	missing := unsatisfiedProducts(unmet, present)
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return []BundleAdvisory{{
		Kind: BundleAdvisoryNoProductReleases,
		Message: "the following Products have no unrecalled Product Release on their specified Release Channels; " +
			"this Bundle can be transferred, but it does not contain them",
		Subjects: missing,
	}}
}

func bundleLabelsAllow(resourceType string, labels map[string]string, pipeline ExportPipeline) bool {
	for _, requirement := range pipeline.ExcludedLabels {
		if bundleLabelRequirementMatches(resourceType, labels, requirement) {
			return false
		}
	}
	for _, requirement := range pipeline.RequiredLabels {
		if contains(requirement.ResourceTypes, resourceType) && !bundleLabelRequirementMatches(resourceType, labels, requirement) {
			return false
		}
	}
	return true
}

func bundleLabelRequirementMatches(resourceType string, labels map[string]string, requirement BundleLabelRequirement) bool {
	if !contains(requirement.ResourceTypes, resourceType) {
		return false
	}
	value, exists := labels[requirement.Name]
	if !exists {
		return false
	}
	return len(requirement.Values) == 0 || contains(requirement.Values, value)
}

func (service *BundleService) packageArtifacts(
	ctx context.Context, pipeline ExportPipeline, targetHub string, releases map[string]Release,
) ([]BundleArtifactLayer, []BundleChunk, error) {
	if !pipeline.IncludeArtifacts {
		return nil, nil, nil
	}
	known := map[string]struct{}{}
	if pipeline.EnableLayerDedup && targetHub != "" {
		pipelineID := pipeline.ID
		if pipeline.ShareLayerDedupAcrossPipelines {
			pipelineID = ""
		}
		confirmations, err := service.repository.ListBundleTransferConfirmations(ctx, pipelineID, targetHub)
		if err != nil {
			return nil, nil, err
		}
		for _, confirmation := range confirmations {
			if !confirmation.RescindedAt.IsZero() {
				continue
			}
			for _, digest := range confirmation.LayerDigests {
				known[digest] = struct{}{}
			}
		}
	}
	digests := map[string]struct{}{}
	for _, release := range releases {
		for _, artifact := range append([]Artifact{release.Chart}, release.Images...) {
			digests[artifact.Digest] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(digests))
	for digest := range digests {
		ordered = append(ordered, digest)
	}
	sort.Strings(ordered)
	chunkSize := pipeline.ChunkSizeBytes
	if chunkSize == 0 {
		chunkSize = 4 * 1024 * 1024
	}
	layers := make([]BundleArtifactLayer, 0, len(ordered))
	chunkPayloads := map[string]BundleChunk{}
	for _, digest := range ordered {
		descriptor, reader, err := service.repository.OpenBlob(ctx, digest)
		if err != nil {
			if pipeline.BuildDespiteMissingResources && errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("package artifact %s: %w", digest, err)
		}
		contents, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read artifact %s: %w", digest, readErr)
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close artifact %s: %w", digest, closeErr)
		}
		layer := BundleArtifactLayer{Digest: digest, MediaType: descriptor.MediaType, Size: descriptor.Size}
		_, layer.Transferred = known[digest]
		for offset := 0; offset < len(contents); offset += chunkSize {
			end := offset + chunkSize
			if end > len(contents) {
				end = len(contents)
			}
			data := contents[offset:end]
			sum := sha256.Sum256(data)
			chunkDigest := sha256Digest(sum)
			layer.ChunkDigests = append(layer.ChunkDigests, chunkDigest)
			if !layer.Transferred {
				chunkPayloads[chunkDigest] = BundleChunk{Digest: chunkDigest, Size: len(data), Data: append([]byte(nil), data...)}
			}
		}
		layers = append(layers, layer)
	}
	chunks := make([]BundleChunk, 0, len(chunkPayloads))
	for _, chunk := range chunkPayloads {
		chunks = append(chunks, chunk)
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Digest < chunks[j].Digest })
	return layers, chunks, nil
}

func (service *BundleService) ConfirmTransfer(
	ctx context.Context, bundleID, targetHub string,
) (BundleTransferConfirmation, error) {
	if strings.TrimSpace(targetHub) == "" {
		return BundleTransferConfirmation{}, ValidationErrors{{Path: "targetHub", Message: "must not be empty"}}
	}
	bundle, err := service.repository.GetBundle(ctx, bundleID)
	if err != nil {
		return BundleTransferConfirmation{}, err
	}
	if bundle.Manifest.TargetHub != targetHub {
		return BundleTransferConfirmation{}, fmt.Errorf("%w: Bundle targets Hub %s", ErrConflict, bundle.Manifest.TargetHub)
	}
	pipeline, err := service.repository.GetExportPipeline(ctx, bundle.Manifest.PipelineID)
	if err != nil {
		return BundleTransferConfirmation{}, err
	}
	if !pipeline.EnableLayerDedup {
		return BundleTransferConfirmation{}, fmt.Errorf("%w: Bundle Pipeline does not enable layer deduplication", ErrConflict)
	}
	now := service.now().UTC()
	if bundle.Manifest.CreatedAt.After(now) || now.Sub(bundle.Manifest.CreatedAt) > 7*24*time.Hour {
		return BundleTransferConfirmation{}, fmt.Errorf("%w: target-specific Bundle confirmation window has expired", ErrConflict)
	}
	digests := make([]string, 0, len(bundle.Manifest.ArtifactLayers))
	for _, layer := range bundle.Manifest.ArtifactLayers {
		digests = append(digests, layer.Digest)
	}
	confirmation := BundleTransferConfirmation{
		ID:         "btc_" + stableShortID(bundle.Manifest.PipelineID+"\x00"+targetHub+"\x00"+bundleID),
		PipelineID: bundle.Manifest.PipelineID, TargetHub: targetHub, BundleID: bundleID,
		LayerDigests: normalizedStrings(digests), ConfirmedAt: now, Revision: 1,
	}
	return service.repository.PutBundleTransferConfirmation(ctx, confirmation)
}

func (service *BundleService) RescindTransfer(
	ctx context.Context, confirmation BundleTransferConfirmation,
) (BundleTransferConfirmation, error) {
	confirmation.RescindedAt = service.now().UTC()
	confirmation.Revision++
	return service.repository.PutBundleTransferConfirmation(ctx, confirmation)
}

func (service *BundleService) RescindTransferByID(
	ctx context.Context, id string,
) (BundleTransferConfirmation, error) {
	confirmations, err := service.repository.ListBundleTransferConfirmations(ctx, "", "")
	if err != nil {
		return BundleTransferConfirmation{}, err
	}
	for _, confirmation := range confirmations {
		if confirmation.ID == id {
			if !confirmation.RescindedAt.IsZero() {
				return confirmation, nil
			}
			return service.RescindTransfer(ctx, confirmation)
		}
	}
	return BundleTransferConfirmation{}, ErrNotFound
}

// closeBundleReleaseDependencies pulls in what the selected Releases need. It
// is the other way into a Bundle, so it has to honour recalls too: without
// that, a recalled Release excluded from selection walks back in as somebody
// else's dependency.
func closeBundleReleaseDependencies(
	catalog, selected map[string]Release, recalls []Recall, allowMissing bool,
) error {
	for changed := true; changed; {
		changed = false
		coordinates := make([]string, 0, len(selected))
		for coordinate := range selected {
			coordinates = append(coordinates, coordinate)
		}
		sort.Strings(coordinates)
		for _, coordinate := range coordinates {
			for _, dependency := range selected[coordinate].Dependencies {
				if bundleHasDependency(selected, dependency) {
					continue
				}
				candidate, found := latestBundleDependency(catalog, recalls, dependency)
				if !found {
					if dependency.Optional || allowMissing {
						continue
					}
					return fmt.Errorf("%w: Product Release %s requires unavailable dependency %s [%s, %s]",
						ErrConflict, coordinate, dependency.Product, dependency.MinimumVersion, dependency.MaximumVersion)
				}
				// Only a candidate that is genuinely new counts as a change.
				// Re-adding one already selected is a no-op on the map, so
				// treating it as progress leaves this loop turning forever: it
				// terminates because `selected` grows and the catalog is finite,
				// and that argument only holds if `changed` means "grew".
				//
				// Found by mutation testing. Inverting the version comparison in
				// bundleHasDependency made it report a selected Release as
				// absent, and the loop then re-selected the same candidate
				// without end -- the mutant did not fail, it hung. Termination
				// should not depend on a predicate elsewhere being right.
				key := candidate.Product + ":" + candidate.Version
				if _, already := selected[key]; already {
					continue
				}
				selected[key] = candidate
				changed = true
			}
		}
	}
	return nil
}

func bundleHasDependency(selected map[string]Release, dependency Dependency) bool {
	for _, release := range selected {
		if release.Product == dependency.Product && compareVersions(release.Version, dependency.MinimumVersion) >= 0 &&
			versionWithinMaximum(release.Version, dependency.MaximumVersion) {
			return true
		}
	}
	return false
}

// latestBundleDependency takes the newest Release satisfying a dependency, and
// a recalled one does not satisfy it. Skipping rather than failing matters: an
// older version in the same range is usually still there, and taking it is a
// better answer than either shipping the recalled Release or refusing to build.
// Only when the whole range is recalled does the caller see nothing found, and
// report the dependency as unavailable -- which it is.
func latestBundleDependency(catalog map[string]Release, recalls []Recall, dependency Dependency) (Release, bool) {
	candidates := make([]Release, 0)
	for _, release := range catalog {
		if release.Product == dependency.Product && !releaseRecalled(release, recalls, "") &&
			compareVersions(release.Version, dependency.MinimumVersion) >= 0 &&
			versionWithinMaximum(release.Version, dependency.MaximumVersion) {
			candidates = append(candidates, release)
		}
	}
	if len(candidates) == 0 {
		return Release{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return compareVersions(candidates[i].Version, candidates[j].Version) > 0
	})
	return candidates[0], true
}

func latestDesiredStates(plans []PlanRecord) map[string]DesiredState {
	latest := latestPlanRecords(plans)
	result := make(map[string]DesiredState, len(latest))
	for environment, plan := range latest {
		if plan.DesiredState == nil {
			continue
		}
		result[environment] = cloneJSON(*plan.DesiredState)
	}
	return result
}

// selectReleases adds the Releases a rule asks for and returns the Products it
// could not satisfy. A Product whose Release Channels hold no Release
// contributes nothing to the Bundle, and the build otherwise succeeds without
// saying so -- which ships a Bundle that an operator believes contains a
// Product it does not.
func selectReleases(
	catalog map[string]Release, recalls []Recall, selected map[string]Release, products, channels []string,
	strategy string, includeHistory bool,
) []string {
	if strategy == BundleReleasesNone {
		return nil
	}
	productSet := make(map[string]struct{}, len(products))
	for _, product := range products {
		productSet[product] = struct{}{}
	}
	channelSet := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		channelSet[channel] = struct{}{}
	}
	satisfied := map[string]struct{}{}
	if strategy == BundleReleasesAll && includeHistory {
		for coordinate, release := range catalog {
			if _, ok := productSet[release.Product]; !ok {
				continue
			}
			if releaseRecalled(release, recalls, "") {
				continue
			}
			if len(channelSet) == 0 || releaseMatchesAnyChannel(release, channelSet) {
				selected[coordinate] = release
				satisfied[release.Product] = struct{}{}
			}
		}
		return unsatisfiedProducts(products, satisfied)
	}
	for product := range productSet {
		for channel := range channelSet {
			var candidates []Release
			for _, release := range catalog {
				if release.Product == product && !releaseRecalled(release, recalls, "") &&
					contains(effectiveReleaseChannels(release, nil), channel) {
					candidates = append(candidates, release)
				}
			}
			sort.Slice(candidates, func(i, j int) bool { return compareVersions(candidates[i].Version, candidates[j].Version) > 0 })
			if len(candidates) > 0 {
				selected[product+":"+candidates[0].Version] = candidates[0]
				satisfied[product] = struct{}{}
			}
		}
	}
	return unsatisfiedProducts(products, satisfied)
}

// unsatisfiedProducts preserves the order the rule named its Products in, and
// reports each at most once however many Channels it was asked for on.
func unsatisfiedProducts(products []string, satisfied map[string]struct{}) []string {
	var missing []string
	seen := map[string]struct{}{}
	for _, product := range products {
		if _, ok := satisfied[product]; ok {
			continue
		}
		if _, duplicate := seen[product]; duplicate {
			continue
		}
		seen[product] = struct{}{}
		missing = append(missing, product)
	}
	return missing
}

func releaseMatchesAnyChannel(release Release, channels map[string]struct{}) bool {
	for _, channel := range effectiveReleaseChannels(release, nil) {
		if _, exists := channels[channel]; exists {
			return true
		}
	}
	return false
}

func addBundleResource(resources map[string]BundleResource, resourceType, id string, value any) {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	resource := BundleResource{Type: resourceType, ID: id, Digest: sha256Digest(digest), Payload: payload}
	resources[resourceType+":"+id] = resource
}

func buildBundleSecurityReport(releases map[string]Release, now time.Time) BundleSecurityReport {
	report := BundleSecurityReport{GeneratedAt: now, Outcome: "PASS"}
	coordinates := make([]string, 0, len(releases))
	for coordinate := range releases {
		coordinates = append(coordinates, coordinate)
	}
	sort.Strings(coordinates)
	for _, coordinate := range coordinates {
		release := releases[coordinate]
		assessment := release.VulnerabilityAssessment
		malware := release.MalwareAssessment
		status := "UNSCANNED"
		virusStatus := "UNSCANNED"
		if assessment != nil && assessment.Status == "COMPLETE" && assessment.Outcome == "PASS" &&
			assessmentCoversReleaseImages(assessment.ArtifactDigests, release) {
			status = "PASS"
		}
		if status != "PASS" {
			report.Outcome = "FAIL"
		}
		if malware != nil && malware.Status == "COMPLETE" && malware.Outcome == "PASS" &&
			malware.Findings == 0 && artifactDigestsCoverRelease(malware.ArtifactDigests, release) {
			virusStatus = "PASS"
		}
		report.ArtifactAssessments = append(report.ArtifactAssessments, BundleArtifactSecurity{
			Release: coordinate, ArtifactDigest: release.Chart.Digest,
			VulnerabilityStatus: "NOT_APPLICABLE", VirusStatus: virusStatus, AssessedAt: malwareAssessmentTime(malware),
		})
		for _, artifact := range release.Images {
			report.ArtifactAssessments = append(report.ArtifactAssessments, BundleArtifactSecurity{
				Release: coordinate, ArtifactDigest: artifact.Digest,
				VulnerabilityStatus: status, VirusStatus: virusStatus, AssessedAt: malwareAssessmentTime(malware),
			})
		}
	}
	return report
}

func artifactDigestsCoverRelease(digests []string, release Release) bool {
	want := make([]string, 0, len(release.Images)+1)
	for _, artifact := range append([]Artifact{release.Chart}, release.Images...) {
		want = append(want, artifact.Digest)
	}
	want, got := normalizedStrings(want), normalizedStrings(digests)
	return strings.Join(want, "\x00") == strings.Join(got, "\x00")
}

func malwareAssessmentTime(assessment *MalwareAssessment) time.Time {
	if assessment == nil {
		return time.Time{}
	}
	return assessment.AssessedAt
}

func (service *BundleService) Preview(ctx context.Context, envelope BundleEnvelope) (BundlePreview, error) {
	return service.PreviewForEnvironment(ctx, envelope, "")
}

func (service *BundleService) PreviewForEnvironment(
	ctx context.Context, envelope BundleEnvelope, environment string,
) (BundlePreview, error) {
	if err := service.verifyEnvelope(envelope); err != nil {
		return BundlePreview{}, err
	}
	importedBundles := map[string]struct{}{}
	state := make(map[string]string)
	if environment != "" {
		catalog, err := service.repository.GetImportedCatalog(ctx, environment)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return BundlePreview{}, err
		}
		if err == nil {
			for key, resource := range catalog.Resources {
				state[key] = resource.Digest
			}
			for _, id := range catalog.BundleIDs {
				importedBundles[id] = struct{}{}
			}
		}
	} else {
		imports, err := service.repository.ListBundleImports(ctx)
		if err != nil {
			return BundlePreview{}, err
		}
		for _, imported := range imports {
			if imported.Status != "ADMITTED" {
				continue
			}
			importedBundles[imported.BundleID] = struct{}{}
			bundle, err := service.repository.GetBundle(ctx, imported.BundleID)
			if err != nil {
				continue
			}
			for _, resource := range bundle.Manifest.Resources {
				key := resource.Type + ":" + resource.ID
				if resource.Deleted {
					delete(state, key)
				} else {
					state[key] = resource.Digest
				}
			}
		}
	}
	if envelope.Manifest.DependsOn != "" {
		if _, exists := importedBundles[envelope.Manifest.DependsOn]; !exists {
			return BundlePreview{}, fmt.Errorf("%w: dependency Bundle %s has not been admitted", ErrConflict, envelope.Manifest.DependsOn)
		}
	}
	preview := BundlePreview{
		BundleID: envelope.Manifest.ID, PipelineID: envelope.Manifest.PipelineID,
		Kind: envelope.Manifest.Kind, DependsOn: envelope.Manifest.DependsOn,
		SecurityOutcome: envelope.Manifest.SecurityReport.Outcome,
	}
	for _, resource := range envelope.Manifest.Resources {
		key := resource.Type + ":" + resource.ID
		if resource.Deleted {
			if _, exists := state[key]; exists {
				preview.Removed = append(preview.Removed, key)
			} else {
				preview.Unchanged = append(preview.Unchanged, key)
			}
			continue
		}
		switch current, exists := state[key]; {
		case !exists:
			preview.Added = append(preview.Added, key)
		case current == resource.Digest:
			preview.Unchanged = append(preview.Unchanged, key)
		default:
			preview.Updated = append(preview.Updated, key)
		}
	}
	return preview, nil
}

func (service *BundleService) Import(
	ctx context.Context, envelope BundleEnvelope, requireSecurityPass bool,
) (BundleImport, error) {
	environment := artifacts.FirstEnvironment(envelope)
	if environment == "" {
		environment = "catalog"
	}
	record, err := service.StageImport(ctx, envelope, environment, "legacy-import", requireSecurityPass)
	if err != nil {
		return BundleImport{}, err
	}
	if record.Status == "ADMITTED" {
		return record, nil
	}
	now := service.now().UTC()
	changeRequest := ChangeRequest{
		ID: "crq_" + stableShortID("legacy-bundle-import\x00"+record.ID), TargetKind: "BUNDLE_IMPORT", TargetID: record.ID,
		Environment: environment, Title: "Trusted internal Bundle import", Author: Principal{Username: "legacy-import"},
		Status: ChangeApproved, RequirementsSatisfied: true, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	changeRequest, err = service.repository.CreateChangeRequest(ctx, changeRequest)
	if err != nil {
		return BundleImport{}, err
	}
	record, err = service.LinkImportChangeRequest(ctx, record.ID, changeRequest.ID)
	if err != nil {
		return BundleImport{}, err
	}
	return service.AdmitImport(ctx, record.ID)
}

func (service *BundleService) StageImport(
	ctx context.Context, envelope BundleEnvelope, environment, requestedBy string, requireSecurityPass bool,
) (BundleImport, error) {
	if !dnsLabelPattern.MatchString(environment) || strings.TrimSpace(requestedBy) == "" {
		return BundleImport{}, ValidationErrors{{Path: "environment", Message: "a target Environment and requester are required"}}
	}
	preview, err := service.PreviewForEnvironment(ctx, envelope, environment)
	if err != nil {
		return BundleImport{}, err
	}
	if requireSecurityPass && preview.SecurityOutcome != "PASS" {
		return BundleImport{}, fmt.Errorf("%w: Bundle security report outcome is %s", ErrConflict, preview.SecurityOutcome)
	}
	if err := service.validateRelayedReportResources(ctx, envelope, environment); err != nil {
		return BundleImport{}, err
	}
	// A Bundle carrying the target Environment's own settings would overwrite
	// them. Refuse unless the target has deliberately handed settings ownership
	// to an upstream Hub.
	if artifacts.CarriesEnvironmentSettings(envelope, environment) {
		federation, err := environmentFederation(ctx, service.repository, environment)
		if err != nil {
			return BundleImport{}, err
		}
		if err := admitSettingsImport(federation); err != nil {
			return BundleImport{}, err
		}
	}
	if err := service.ensureBundleArtifacts(ctx, envelope); err != nil {
		return BundleImport{}, err
	}
	identity := sha256.Sum256([]byte(envelope.ManifestDigest + "\x00" + environment))
	id := "bni_" + hex.EncodeToString(identity[:16])
	if existing, err := service.repository.GetBundleImport(ctx, id); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return BundleImport{}, err
	}
	imports, err := service.repository.ListBundleImports(ctx)
	if err != nil {
		return BundleImport{}, err
	}
	for _, existing := range imports {
		if existing.Environment == environment && existing.Status == "PENDING_APPROVAL" {
			return BundleImport{}, fmt.Errorf("%w: Environment %s already has a pending Bundle import", ErrConflict, environment)
		}
	}
	if _, err := service.repository.CreateBundle(ctx, envelope); err != nil {
		return BundleImport{}, err
	}
	now := service.now().UTC()
	record := BundleImport{
		ID:       id,
		BundleID: envelope.Manifest.ID, ManifestDigest: envelope.ManifestDigest,
		Environment: environment, RequestedBy: requestedBy, Status: "PENDING_APPROVAL",
		Preview: preview, Tasks: newBundleImportTasks(envelope, environment, now), RequestedAt: now, Revision: 1,
	}
	return service.repository.CreateBundleImport(ctx, record)
}

func newBundleImportTasks(envelope BundleEnvelope, environment string, now time.Time) []BundleImportTask {
	artifacts := make([]string, 0, len(envelope.Manifest.ArtifactLayers))
	resources := make([]string, 0, len(envelope.Manifest.Resources))
	for _, layer := range envelope.Manifest.ArtifactLayers {
		artifacts = append(artifacts, layer.Digest)
	}
	for _, resource := range envelope.Manifest.Resources {
		resources = append(resources, resource.Type+":"+resource.ID)
	}
	artifacts, resources = normalizedStrings(artifacts), normalizedStrings(resources)
	return []BundleImportTask{
		{Sequence: 1, Stage: BundleImportTaskVerify, Status: BundleImportTaskSucceeded, Resources: []string{envelope.Manifest.ID}, StartedAt: now, FinishedAt: now},
		{Sequence: 2, Stage: BundleImportTaskArtifacts, Status: BundleImportTaskSucceeded, Resources: artifacts, StartedAt: now, FinishedAt: now},
		{Sequence: 3, Stage: BundleImportTaskCatalog, Status: BundleImportTaskPending, Resources: resources},
		{Sequence: 4, Stage: BundleImportTaskEnvironment, Status: BundleImportTaskPending, Resources: []string{environment}},
	}
}

func (service *BundleService) LinkImportChangeRequest(ctx context.Context, id, changeRequestID string) (BundleImport, error) {
	return service.repository.UpdateBundleImport(ctx, id, func(record *BundleImport) error {
		if record.Status != "PENDING_APPROVAL" || (record.ChangeRequestID != "" && record.ChangeRequestID != changeRequestID) {
			return ErrConflict
		}
		record.ChangeRequestID = changeRequestID
		return nil
	})
}

func (service *BundleService) SyncImportReviewStatus(ctx context.Context, id, changeStatus string) (BundleImport, error) {
	if changeStatus == ChangeApproved {
		return service.AdmitImport(ctx, id)
	}
	target := map[string]string{
		ChangePendingApprovals: "PENDING_APPROVAL", ChangeRejected: "REJECTED", ChangeCancelled: "CANCELLED", ChangeError: "ERROR",
	}[changeStatus]
	if target == "" {
		return BundleImport{}, ErrInvalidTransition
	}
	return service.repository.UpdateBundleImport(ctx, id, func(record *BundleImport) error {
		if record.Status == "ADMITTED" {
			return ErrInvalidTransition
		}
		record.Status = target
		return nil
	})
}

func (service *BundleService) AdmitImport(ctx context.Context, id string) (result BundleImport, resultErr error) {
	record, err := service.repository.GetBundleImport(ctx, id)
	if err != nil {
		return BundleImport{}, err
	}
	if record.Status == "ADMITTED" {
		return record, nil
	}
	if record.Status != "PENDING_APPROVAL" || record.ChangeRequestID == "" {
		return BundleImport{}, ErrInvalidTransition
	}
	changeRequest, err := service.repository.GetChangeRequest(ctx, record.ChangeRequestID)
	if err != nil {
		return BundleImport{}, err
	}
	if changeRequest.Status != ChangeApproved {
		return BundleImport{}, ErrInvalidTransition
	}
	envelope, err := service.repository.GetBundle(ctx, record.BundleID)
	if err != nil {
		return BundleImport{}, err
	}
	activeStage := BundleImportTaskCatalog
	stageStarted := service.now().UTC()
	defer func() {
		if resultErr == nil {
			return
		}
		_, _ = service.repository.UpdateBundleImport(ctx, id, func(current *BundleImport) error {
			current.Status = "ERROR"
			current.Error = RedactDiagnosticText(resultErr.Error(), maximumDiagnosticFailure)
			finishBundleImportTask(current, activeStage, BundleImportTaskFailed, stageStarted, service.now().UTC(), current.Error)
			skipUnrunBundleImportTasks(current)
			return nil
		})
	}()
	catalog, err := service.repository.GetImportedCatalog(ctx, record.Environment)
	if errors.Is(err, ErrNotFound) {
		catalog = ImportedCatalog{Environment: record.Environment, Resources: map[string]BundleResource{}, Revision: 1}
	} else if err != nil {
		return BundleImport{}, err
	} else {
		catalog.Revision++
	}
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type == "ENVIRONMENT" && resource.ID != record.Environment {
			continue
		}
		if resource.Type == BundleResourceEnvironmentReport {
			if resource.ID != record.Environment {
				continue
			}
			if resource.Deleted {
				catalog.RelayedReport = nil
				catalog.RelayedReportBundleID = ""
				catalog.RelayedReportSigningKey = ""
				delete(catalog.Resources, resource.Type+":"+resource.ID)
				continue
			}
			var report EnvironmentReport
			if err := json.Unmarshal(resource.Payload, &report); err != nil {
				return BundleImport{}, fmt.Errorf("%w: invalid relayed Environment report", ErrConflict)
			}
			catalog.RelayedReport = &report
			catalog.RelayedReportBundleID = envelope.Manifest.ID
			catalog.RelayedReportSigningKey = envelope.SigningKeyID
			catalog.Resources[resource.Type+":"+resource.ID] = cloneJSON(resource)
			continue
		}
		key := resource.Type + ":" + resource.ID
		if resource.Deleted {
			delete(catalog.Resources, key)
		} else {
			catalog.Resources[key] = cloneJSON(resource)
		}
	}
	activeStage = BundleImportTaskEnvironment
	catalog.BundleIDs = normalizedStrings(append(catalog.BundleIDs, envelope.Manifest.ID))
	catalog.UpdatedAt = service.now().UTC()
	if _, err := service.repository.PutImportedCatalog(ctx, catalog); err != nil {
		return BundleImport{}, err
	}
	return service.repository.UpdateBundleImport(ctx, id, func(current *BundleImport) error {
		if current.Status == "ADMITTED" {
			return nil
		}
		if current.Status != "PENDING_APPROVAL" {
			return ErrInvalidTransition
		}
		now := service.now().UTC()
		current.Status, current.ImportedAt = "ADMITTED", now
		finishBundleImportTask(current, BundleImportTaskCatalog, BundleImportTaskSucceeded, stageStarted, now, "")
		finishBundleImportTask(current, BundleImportTaskEnvironment, BundleImportTaskSucceeded, stageStarted, now, "")
		return nil
	})
}

// skipUnrunBundleImportTasks marks what an aborted import never got to. The
// import is terminal by the time this runs, so a task still PENDING is not
// waiting for anything -- and an operator reading the task list has no other
// way to tell an abandoned task from a queued one.
func skipUnrunBundleImportTasks(record *BundleImport) {
	for index := range record.Tasks {
		if record.Tasks[index].Status == BundleImportTaskPending {
			record.Tasks[index].Status = BundleImportTaskSkipped
		}
	}
}

func finishBundleImportTask(record *BundleImport, stage, status string, started, finished time.Time, failure string) {
	for index := range record.Tasks {
		if record.Tasks[index].Stage != stage {
			continue
		}
		record.Tasks[index].Status = status
		record.Tasks[index].StartedAt = started.UTC()
		record.Tasks[index].FinishedAt = finished.UTC()
		record.Tasks[index].Failure = RedactDiagnosticText(failure, maximumDiagnosticFailure)
		return
	}
}

// validateRelayedReportResources makes the topology predicate executable at
// the Bundle boundary. A signed report is still rejected unless the target has
// explicitly selected relayed state, and the original Agent attribution and
// observation timestamps must remain intact.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/managing-environments/environment-connection-settings/
func (service *BundleService) validateRelayedReportResources(
	ctx context.Context, envelope BundleEnvelope, environment string,
) error {
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type != BundleResourceEnvironmentReport {
			continue
		}
		if resource.ID != environment {
			return fmt.Errorf("%w: relayed report %s does not match target Environment %s", ErrConflict, resource.ID, environment)
		}
		federation, err := environmentFederation(ctx, service.repository, environment)
		if err != nil {
			return err
		}
		if !federation.AcceptsRelayedState() {
			return &FederationError{Environment: environment, Message: "connection setting does not accept reported state from Bundles"}
		}
		if resource.Deleted {
			continue
		}
		var report EnvironmentReport
		if err := json.Unmarshal(resource.Payload, &report); err != nil {
			return fmt.Errorf("%w: invalid relayed Environment report", ErrConflict)
		}
		if err := validateRelayedEnvironmentReport(report, environment, service.now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func validateRelayedEnvironmentReport(report EnvironmentReport, environment string, now time.Time) error {
	if report.Environment != environment || strings.TrimSpace(report.AgentID) == "" || report.ObservedAt.IsZero() {
		return fmt.Errorf("%w: relayed report has invalid Environment, Agent, or observation attribution", ErrConflict)
	}
	if report.ObservedAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("%w: relayed report observation time is in the future", ErrConflict)
	}
	if len(report.Entities) > 10000 || len(report.Secrets) > 10000 {
		return fmt.Errorf("%w: relayed report exceeds the 10000-item bound", ErrConflict)
	}
	seenEntities := make(map[string]struct{}, len(report.Entities))
	for _, entity := range report.Entities {
		if strings.TrimSpace(entity.InstallationID) == "" || entity.ObservedAt.IsZero() {
			return fmt.Errorf("%w: relayed report contains an unattributed Entity observation", ErrConflict)
		}
		if _, duplicate := seenEntities[entity.InstallationID]; duplicate {
			return fmt.Errorf("%w: relayed report duplicates installation %s", ErrConflict, entity.InstallationID)
		}
		seenEntities[entity.InstallationID] = struct{}{}
	}
	seenSecrets := make(map[string]struct{}, len(report.Secrets))
	for _, secret := range report.Secrets {
		key := secret.Assignment + "\x00" + secret.Namespace
		if strings.TrimSpace(secret.Assignment) == "" || strings.TrimSpace(secret.Namespace) == "" {
			return fmt.Errorf("%w: relayed report contains an unattributed secret observation", ErrConflict)
		}
		if _, duplicate := seenSecrets[key]; duplicate {
			return fmt.Errorf("%w: relayed report duplicates secret %s in %s", ErrConflict, secret.Assignment, secret.Namespace)
		}
		seenSecrets[key] = struct{}{}
	}
	return nil
}

// bundleCarriesEnvironmentSettings reports whether the Bundle would replace the
// named Environment's own settings, as opposed to only delivering catalog
// resources such as Products, Releases, and Teams.
// ensureBundleArtifacts hands the envelope to Artifacts, which makes every
// layer it describes present in the store. The one translation left here is the
// vocabulary: Artifacts calls an absent transferred layer ErrLayerMissing,
// which is the right name inside a package that knows nothing about HTTP, and
// out here that condition has always been ErrNotFound and a 404.
func (service *BundleService) ensureBundleArtifacts(ctx context.Context, envelope BundleEnvelope) error {
	err := artifactapp.NewStaging(service.repository).EnsureLayers(ctx, envelope)
	if errors.Is(err, artifacts.ErrLayerMissing) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}
