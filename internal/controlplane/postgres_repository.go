package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// PostgresMigrationComponent is this schema's name in the shared ledger. It is
// an identifier in a database, so it changes never.
const PostgresMigrationComponent = "controlplane"

// Applied through the shared ledger so this schema is versioned like the other
// one rather than being the exception. See internal/pgmigrate.
var postgresRepositoryMigrations = []pgmigrate.Migration{{
	Name: "0001-repository-core",
	Statements: `
CREATE TABLE IF NOT EXISTS conduit_repository_state (
    singleton boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    generation bigint NOT NULL,
    state jsonb NOT NULL
);
INSERT INTO conduit_repository_state (singleton, generation, state)
VALUES (TRUE, 0, '{}'::jsonb)
ON CONFLICT (singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS conduit_repository_blobs (
    digest text PRIMARY KEY,
    descriptor jsonb NOT NULL,
    contents bytea NOT NULL,
    CHECK (octet_length(contents) >= 0)
);`,
}, {
	Name: "0002-apollo-agent-reports",
	Statements: `CREATE TABLE IF NOT EXISTS conduit_apollo_agent_reports (
 environment text PRIMARY KEY,
 inputs jsonb NOT NULL
);`,
}, {
	Name: "0003-promotion-lifecycle",
	Statements: `CREATE TABLE IF NOT EXISTS conduit_promotion_lifecycle (
 id text PRIMARY KEY,
 run_id text NOT NULL,
 previous_id text NOT NULL,
 lifecycle text NOT NULL CHECK (lifecycle IN ('RUNNING', 'SUCCEEDED', 'ABORTED', 'CANCELLED')),
 reason text NOT NULL,
 value_tier text NOT NULL
);`,
}}

// PostgresMigrations is this context's schema as the runner sees it, so a
// caller outside the package can ask what a start would apply without starting
// one -- see pgmigrate.Pending, and the restore drill in cmd/platform-delivery
// that uses it to prove a restored database needs no migration. The clone is
// deliberate: a caller that appended to the package's own slice would change
// what every later Apply runs.
func PostgresMigrations() []pgmigrate.Migration { return slices.Clone(postgresRepositoryMigrations) }

// TransactionalRepository can commit several existing repository operations as
// one visibility boundary. Application services use this narrow capability for
// decisions whose audit record must be durable before the mutation is visible.
type TransactionalRepository interface {
	Repository
	// Transaction is limited to metadata operations. Artifact byte ingestion is
	// already atomic by digest and must not be invoked from this callback.
	Transaction(context.Context, func(Repository) error) error
}

// PostgresRepository is the production multi-replica repository. Metadata is
// committed as one versioned JSON snapshot so the legacy Repository contract
// remains unchanged while it is extracted into bounded contexts. Immutable
// artifact bytes are stored separately to keep large blobs out of that row.
//
// The single metadata row deliberately serializes writes. It provides a small,
// auditable correctness boundary for the cutover; future context-owned tables
// can split contention without changing domain behavior.
type PostgresRepository struct {
	database        *sql.DB
	transactionView func(Repository) Repository
}

func NewPostgresRepository(ctx context.Context, dataSourceName string) (*PostgresRepository, error) {
	if dataSourceName == "" {
		return nil, errors.New("PostgreSQL data source name is required")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL repository: %w", err)
	}
	database.SetMaxOpenConns(20)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect PostgreSQL repository: %w", err)
	}
	if err := pgmigrate.Apply(ctx, database, PostgresMigrationComponent, postgresRepositoryMigrations); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize PostgreSQL repository: %w", err)
	}
	if err := auditpostgres.ApplyMigrations(ctx, database); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize authorization audit: %w", err)
	}
	repository := &PostgresRepository{database: database}
	if err := repository.withState(ctx, true, func(*MemoryRepository) error { return nil }); err != nil {
		database.Close()
		return nil, err
	}
	return repository, nil
}

func (repository *PostgresRepository) Close() error {
	if repository == nil || repository.database == nil {
		return nil
	}
	return repository.database.Close()
}

func (repository *PostgresRepository) Transaction(
	ctx context.Context, operation func(Repository) error,
) error {
	if operation == nil {
		return errors.New("repository transaction operation is required")
	}
	return repository.withStateTransaction(ctx, true, func(memory *MemoryRepository, transaction *sql.Tx) error {
		view := Repository(&authorizationAuditView{Repository: memory, audit: auditpostgres.New(transaction)})
		if repository.transactionView != nil {
			view = repository.transactionView(view)
		}
		return operation(view)
	})
}

// Transaction gives tests and single-process callers the same rollback and
// visibility semantics as the production adapter. The operation runs against
// an isolated copy while the original repository is locked, then replaces the
// original state only after success.
func (repository *MemoryRepository) Transaction(
	_ context.Context, operation func(Repository) error,
) error {
	if operation == nil {
		return errors.New("repository transaction operation is required")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	encoded, err := json.Marshal(repository.stateLocked())
	if err != nil {
		return fmt.Errorf("encode repository transaction state: %w", err)
	}
	working, err := decodeMemoryRepositoryState(encoded)
	if err != nil {
		return err
	}
	working.transactionLocal = true
	working.apolloAgentReports = cloneApolloAgentReports(repository.apolloAgentReports)
	if err := operation(working); err != nil {
		return err
	}
	working.mu.Lock()
	next := working.stateLocked()
	working.mu.Unlock()
	repository.applyStateLocked(next)
	repository.apolloAgentReports = working.apolloAgentReports
	return nil
}

func (repository *PostgresRepository) withState(
	ctx context.Context, write bool, operation func(*MemoryRepository) error,
) error {
	return repository.withStateTransaction(ctx, write, func(memory *MemoryRepository, _ *sql.Tx) error { return operation(memory) })
}

func (repository *PostgresRepository) withStateTransaction(ctx context.Context, write bool, operation func(*MemoryRepository, *sql.Tx) error) error {
	if repository == nil || repository.database == nil {
		return errors.New("PostgreSQL repository is closed")
	}
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: !write})
	if err != nil {
		return fmt.Errorf("begin repository transaction: %w", err)
	}
	defer transaction.Rollback()

	query := "SELECT generation, state FROM conduit_repository_state WHERE singleton = TRUE"
	if write {
		query += " FOR UPDATE"
	}
	var generation int64
	var encoded []byte
	if err := transaction.QueryRowContext(ctx, query).Scan(&generation, &encoded); err != nil {
		return fmt.Errorf("read repository state: %w", err)
	}
	memory, err := decodeMemoryRepositoryState(encoded)
	if err != nil {
		return err
	}
	if err := loadPromotionLifecycles(ctx, transaction, memory); err != nil {
		return err
	}
	if write {
		if err := importLegacyAuthorizationAudit(ctx, transaction, memory); err != nil {
			return err
		}
	}
	if err := operation(memory, transaction); err != nil {
		return err
	}
	if write {
		if err := storePromotionLifecycles(ctx, transaction, memory); err != nil {
			return err
		}
		encoded, err = encodeLegacyPromotionState(memory)
		if err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx,
			"UPDATE conduit_repository_state SET generation = generation + 1, state = $1 WHERE singleton = TRUE AND generation = $2",
			encoded, generation)
		if err != nil {
			return fmt.Errorf("write repository state: %w", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("confirm repository state write: %w", err)
		}
		if updated != 1 {
			return ErrConflict
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit repository transaction: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) PutBlob(
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
	descriptor := BlobDescriptor{
		MediaType: normalizeMediaType(mediaType), Digest: digest, Size: int64(len(contents)), CreatedAt: time.Now().UTC(),
	}
	encodedDescriptor, err := json.Marshal(descriptor)
	if err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("encode artifact descriptor: %w", err)
	}
	result, err := repository.database.ExecContext(ctx, `
INSERT INTO conduit_repository_blobs (digest, descriptor, contents)
VALUES ($1, $2, $3)
ON CONFLICT (digest) DO NOTHING`, digest, encodedDescriptor, contents)
	if err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("store artifact: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return BlobDescriptor{}, false, fmt.Errorf("confirm artifact write: %w", err)
	}
	if created == 1 {
		return descriptor, true, nil
	}
	existing, _, err := repository.openBlob(ctx, digest)
	if err != nil {
		return BlobDescriptor{}, false, err
	}
	existing.MediaType = normalizeMediaType(mediaType)
	return existing, false, nil
}

func (repository *PostgresRepository) OpenBlob(
	ctx context.Context, digest string,
) (BlobDescriptor, io.ReadCloser, error) {
	descriptor, contents, err := repository.openBlob(ctx, digest)
	if err != nil {
		return BlobDescriptor{}, nil, err
	}
	return descriptor, io.NopCloser(bytes.NewReader(contents)), nil
}

func (repository *PostgresRepository) openBlob(ctx context.Context, digest string) (BlobDescriptor, []byte, error) {
	if !digestPattern.MatchString(digest) {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	var encodedDescriptor []byte
	var contents []byte
	err := repository.database.QueryRowContext(ctx,
		"SELECT descriptor, contents FROM conduit_repository_blobs WHERE digest = $1", digest,
	).Scan(&encodedDescriptor, &contents)
	if errors.Is(err, sql.ErrNoRows) {
		return BlobDescriptor{}, nil, ErrNotFound
	}
	if err != nil {
		return BlobDescriptor{}, nil, fmt.Errorf("read artifact: %w", err)
	}
	var descriptor BlobDescriptor
	if err := json.Unmarshal(encodedDescriptor, &descriptor); err != nil {
		return BlobDescriptor{}, nil, fmt.Errorf("decode artifact descriptor: %w", err)
	}
	return descriptor, append([]byte(nil), contents...), nil
}

type memoryRepositoryState struct {
	LiveOutboxes                map[string]nativeLiveOutbox              `json:"liveOutboxes,omitempty"`
	Plans                       map[string]PlanRecord                    `json:"plans"`
	Reports                     map[string]EnvironmentReport             `json:"reports"`
	Suppressions                map[string]SuppressionRecord             `json:"suppressions"`
	Promotions                  map[string]PromotionRecord               `json:"promotions"`
	PromotionStageControls      map[string]PromotionStageControl         `json:"promotionStageControls"`
	ModuleReleases              map[string]ModuleRelease                 `json:"moduleReleases"`
	ModuleInstallations         map[string]ModuleInstallation            `json:"moduleInstallations"`
	ModuleAutoUpdatePolicies    map[string]ModuleAutoUpdatePolicy        `json:"moduleAutoUpdatePolicies"`
	ExportPipelines             map[string]ExportPipeline                `json:"exportPipelines"`
	ExportPipelineRevisions     map[string]map[uint64]ExportPipeline     `json:"exportPipelineRevisions"`
	ExportPipelineScheduleRuns  map[string]ExportPipelineScheduleRun     `json:"exportPipelineScheduleRuns"`
	EnvironmentProfiles         map[string]EnvironmentProfile            `json:"environmentProfiles"`
	EnvironmentProfileRevisions map[string]map[uint64]EnvironmentProfile `json:"environmentProfileRevisions"`
	CatalogRetirements          map[string]CatalogRetirement             `json:"catalogRetirements"`
	LabelDefinitions            map[string]LabelDefinition               `json:"labelDefinitions"`
	LabelDefinitionRevisions    map[string]map[uint64]LabelDefinition    `json:"labelDefinitionRevisions"`
	Bundles                     map[string]BundleEnvelope                `json:"bundles"`
	BundleImports               map[string]BundleImport                  `json:"bundleImports"`
	BundleTransfers             map[string]BundleTransferConfirmation    `json:"bundleTransfers"`
	ImportedCatalogs            map[string]ImportedCatalog               `json:"importedCatalogs"`
	Teams                       map[string]Team                          `json:"teams"`
	ResourceGrants              map[string]ResourceGrant                 `json:"resourceGrants"`
	ApprovalPolicies            map[string]ApprovalPolicy                `json:"approvalPolicies"`
	ChangeRequests              map[string]ChangeRequest                 `json:"changeRequests"`
	AuthorizationAudit          map[string]AuthorizationAuditEvent       `json:"authorizationAudit,omitempty"`
	ProductPublications         map[string]ProductPublication            `json:"productPublications"`
	ComputeExecutions           map[string]ComputeReservation            `json:"computeExecutions,omitempty"`
}

func (repository *MemoryRepository) stateLocked() memoryRepositoryState {
	return memoryRepositoryState{
		LiveOutboxes: repository.liveOutboxes,
		Plans:        repository.plans, Reports: repository.reports, Suppressions: repository.suppressions,
		Promotions: repository.promotions, PromotionStageControls: repository.promotionStageControls,
		ModuleReleases: repository.moduleReleases, ModuleInstallations: repository.moduleInstallations,
		ModuleAutoUpdatePolicies: repository.moduleAutoUpdatePolicies, ExportPipelines: repository.exportPipelines,
		ExportPipelineRevisions:    repository.exportPipelineRevisions,
		ExportPipelineScheduleRuns: repository.exportPipelineScheduleRuns,
		EnvironmentProfiles:        repository.environmentProfiles, EnvironmentProfileRevisions: repository.environmentProfileRevisions,
		CatalogRetirements: repository.catalogRetirements, LabelDefinitions: repository.labelDefinitions,
		LabelDefinitionRevisions: repository.labelDefinitionRevisions, Bundles: repository.bundles,
		BundleImports: repository.bundleImports, BundleTransfers: repository.bundleTransfers,
		ImportedCatalogs: repository.importedCatalogs, Teams: repository.teams, ResourceGrants: repository.resourceGrants,
		ApprovalPolicies: repository.approvalPolicies, ChangeRequests: repository.changeRequests,
		AuthorizationAudit: repository.authorizationAudit, ProductPublications: repository.productPublications,
		ComputeExecutions: repository.computeExecutions,
	}
}

func decodeMemoryRepositoryState(encoded []byte) (*MemoryRepository, error) {
	repository := NewMemoryRepository()
	state := memoryRepositoryState{
		LiveOutboxes: repository.liveOutboxes,
		Plans:        repository.plans, Reports: repository.reports, Suppressions: repository.suppressions,
		Promotions: repository.promotions, PromotionStageControls: repository.promotionStageControls,
		ModuleReleases: repository.moduleReleases, ModuleInstallations: repository.moduleInstallations,
		ModuleAutoUpdatePolicies: repository.moduleAutoUpdatePolicies, ExportPipelines: repository.exportPipelines,
		ExportPipelineRevisions:    repository.exportPipelineRevisions,
		ExportPipelineScheduleRuns: repository.exportPipelineScheduleRuns,
		EnvironmentProfiles:        repository.environmentProfiles, EnvironmentProfileRevisions: repository.environmentProfileRevisions,
		CatalogRetirements: repository.catalogRetirements, LabelDefinitions: repository.labelDefinitions,
		LabelDefinitionRevisions: repository.labelDefinitionRevisions, Bundles: repository.bundles,
		BundleImports: repository.bundleImports, BundleTransfers: repository.bundleTransfers,
		ImportedCatalogs: repository.importedCatalogs, Teams: repository.teams, ResourceGrants: repository.resourceGrants,
		ApprovalPolicies: repository.approvalPolicies, ChangeRequests: repository.changeRequests,
		AuthorizationAudit: repository.authorizationAudit, ProductPublications: repository.productPublications,
		ComputeExecutions: repository.computeExecutions,
	}
	if len(encoded) != 0 {
		if err := json.Unmarshal(encoded, &state); err != nil {
			return nil, fmt.Errorf("decode repository state: %w", err)
		}
	}
	repository.applyStateLocked(state)
	return repository, nil
}

func (repository *MemoryRepository) applyStateLocked(state memoryRepositoryState) {
	repository.liveOutboxes = nonNilMap(state.LiveOutboxes)
	repository.plans = nonNilMap(state.Plans)
	repository.reports = nonNilMap(state.Reports)
	repository.suppressions = nonNilMap(state.Suppressions)
	repository.promotions = nonNilMap(state.Promotions)
	repository.promotionStageControls = nonNilMap(state.PromotionStageControls)
	repository.moduleReleases = nonNilMap(state.ModuleReleases)
	repository.moduleInstallations = nonNilMap(state.ModuleInstallations)
	repository.moduleAutoUpdatePolicies = nonNilMap(state.ModuleAutoUpdatePolicies)
	repository.exportPipelines = nonNilMap(state.ExportPipelines)
	repository.exportPipelineRevisions = nonNilMap(state.ExportPipelineRevisions)
	repository.exportPipelineScheduleRuns = nonNilMap(state.ExportPipelineScheduleRuns)
	repository.environmentProfiles = nonNilMap(state.EnvironmentProfiles)
	repository.environmentProfileRevisions = nonNilMap(state.EnvironmentProfileRevisions)
	repository.catalogRetirements = nonNilMap(state.CatalogRetirements)
	repository.labelDefinitions = nonNilMap(state.LabelDefinitions)
	repository.labelDefinitionRevisions = nonNilMap(state.LabelDefinitionRevisions)
	repository.bundles = nonNilMap(state.Bundles)
	repository.bundleImports = nonNilMap(state.BundleImports)
	repository.bundleTransfers = nonNilMap(state.BundleTransfers)
	repository.importedCatalogs = nonNilMap(state.ImportedCatalogs)
	repository.teams = nonNilMap(state.Teams)
	repository.resourceGrants = nonNilMap(state.ResourceGrants)
	repository.approvalPolicies = nonNilMap(state.ApprovalPolicies)
	repository.changeRequests = nonNilMap(state.ChangeRequests)
	repository.authorizationAudit = nonNilMap(state.AuthorizationAudit)
	repository.productPublications = nonNilMap(state.ProductPublications)
	repository.computeExecutions = nonNilMap(state.ComputeExecutions)
}

func nonNilMap[K comparable, V any](value map[K]V) map[K]V {
	if value == nil {
		return make(map[K]V)
	}
	return value
}
