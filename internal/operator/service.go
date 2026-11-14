// Package operator is the client side of the Hub's operator API: the semantic
// operations an operator performs -- proposing and approving Plans, managing
// grants and approval policies, reading reported state -- expressed as Go
// calls over a SPIFFE-authenticated transport.
package operator

import (
	"context"
	"io"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// ControlPlane is the outbound port used by operator workflows. Transports
// implement this interface; operator use cases do not depend on HTTP, SPIFFE,
// or a particular deployment environment.
type ControlPlane interface {
	ListArtifactRegistries(context.Context) ([]controlplane.ArtifactRegistryRecord, error)
	InstallModule(context.Context, string, string, string, string, map[string]string) (controlplane.ModuleInstallation, error)
	PublishProductRelease(context.Context, controlplane.HelmProductReleaseBundle) (ProductReleasePublication, error)
	PublishTerraformProductRelease(context.Context, controlplane.TerraformProductManifest) (controlplane.ProductPublication, error)
	ListProductPublications(context.Context, string) ([]controlplane.ProductPublication, error)
	ListEnvironments(context.Context) ([]controlplane.EnvironmentCatalogRecord, error)
	GetEnvironment(context.Context, string) (controlplane.EnvironmentCatalogRecord, error)
	ListEntities(context.Context, string) ([]controlplane.Entity, error)
	ListProducts(context.Context) ([]controlplane.ProductCatalogRecord, error)
	ListProductReleases(context.Context, string) ([]controlplane.Release, error)
	GetProductRelease(context.Context, string, string) (controlplane.Release, error)
	ListModules(context.Context) ([]controlplane.ModuleRelease, error)
	GetEnvironmentProfile(context.Context, string) (controlplane.EnvironmentProfile, error)
	PutEnvironmentProfile(context.Context, string, controlplane.EnvironmentProfile) (EnvironmentProfileChangeResult, error)
	GetEnvironmentVersions(context.Context, string) ([]controlplane.TargetHubEntityVersion, error)
	PublishModules(context.Context, []controlplane.ModuleDefinition) ([]controlplane.ModuleRelease, error)
	LockModule(context.Context, controlplane.ModuleDefinition) (controlplane.ModuleLock, error)
	ScanRelease(context.Context, controlplane.Release) (controlplane.ArtifactScanResult, error)
	IssueEntityCommand(context.Context, controlplane.IssueCommandRequest) (controlplane.PlanRecord, error)
	UploadArtifact(context.Context, string, io.Reader) (controlplane.BlobDescriptor, error)
	ListChangeRequests(context.Context, string) ([]controlplane.ChangeRequest, error)
	GetChangeRequest(context.Context, string) (controlplane.ChangeRequest, error)
	ApproveChangeRequest(context.Context, string, string) (ChangeRequestActionResult, error)
	RejectChangeRequest(context.Context, string, string) (ChangeRequestActionResult, error)
	ReopenChangeRequest(context.Context, string) (ChangeRequestActionResult, error)
	CancelChangeRequest(context.Context, string, string) (ChangeRequestActionResult, error)
	PutTeam(context.Context, string, controlplane.Team) (controlplane.Team, error)
	CreateResourceGrant(context.Context, controlplane.ResourceGrant) (controlplane.ResourceGrant, error)
	PutApprovalPolicy(context.Context, string, controlplane.ApprovalPolicy) (controlplane.ApprovalPolicy, error)
	ProposePlan(context.Context, controlplane.DesiredState) (controlplane.PlanRecord, error)
	ApprovePlan(context.Context, string, string) (controlplane.PlanRecord, error)
	CancelPlan(context.Context, string, string) (controlplane.PlanRecord, error)
	GetPlan(context.Context, string) (controlplane.PlanRecord, error)
	ListPlans(context.Context, PlanListQuery) (PlanListResult, error)
	ListPackagingRuns(context.Context, PackagingRunQuery) (PackagingRunList, error)
	GetReportedState(context.Context, string) (controlplane.EnvironmentReport, error)
	ListSuppressions(context.Context, string) ([]controlplane.SuppressionRecord, error)
	RemoveSuppression(context.Context, string) (controlplane.SuppressionRecord, error)
}

type ChangeRequestActionResult struct {
	ChangeRequest controlplane.ChangeRequest `json:"changeRequest"`
	Plan          *controlplane.PlanRecord   `json:"plan,omitempty"`
	BundleImport  *controlplane.BundleImport `json:"bundleImport,omitempty"`
}

type ProductReleasePublication struct {
	MavenCoordinate string `json:"mavenCoordinate"`
	Created         bool   `json:"created"`
}

type EnvironmentProfileChangeResult struct {
	ChangeRequest controlplane.ChangeRequest       `json:"changeRequest"`
	Profile       *controlplane.EnvironmentProfile `json:"profile,omitempty"`
}

// Service is the application boundary consumed by inbound operator adapters.
// It deliberately exposes semantic control-plane operations rather than a
// generic arbitrary HTTP request primitive.
type Service struct {
	controlPlane ControlPlane
}

func NewService(controlPlane ControlPlane) Service { return Service{controlPlane: controlPlane} }

func (service Service) ListArtifactRegistries(ctx context.Context) ([]controlplane.ArtifactRegistryRecord, error) {
	return service.controlPlane.ListArtifactRegistries(ctx)
}

func (service Service) InstallModule(
	ctx context.Context, environment, module, version, releaseChannel string, variables map[string]string,
) (controlplane.ModuleInstallation, error) {
	return service.controlPlane.InstallModule(ctx, environment, module, version, releaseChannel, variables)
}

func (service Service) PublishProductRelease(
	ctx context.Context, bundle controlplane.HelmProductReleaseBundle,
) (ProductReleasePublication, error) {
	return service.controlPlane.PublishProductRelease(ctx, bundle)
}

func (service Service) PublishTerraformProductRelease(
	ctx context.Context, manifest controlplane.TerraformProductManifest,
) (controlplane.ProductPublication, error) {
	return service.controlPlane.PublishTerraformProductRelease(ctx, manifest)
}

func (service Service) ListProductPublications(ctx context.Context, product string) ([]controlplane.ProductPublication, error) {
	return service.controlPlane.ListProductPublications(ctx, product)
}

func (service Service) ListEnvironments(ctx context.Context) ([]controlplane.EnvironmentCatalogRecord, error) {
	return service.controlPlane.ListEnvironments(ctx)
}

func (service Service) GetEnvironment(ctx context.Context, environment string) (controlplane.EnvironmentCatalogRecord, error) {
	return service.controlPlane.GetEnvironment(ctx, environment)
}

func (service Service) ListEntities(ctx context.Context, environment string) ([]controlplane.Entity, error) {
	return service.controlPlane.ListEntities(ctx, environment)
}

func (service Service) ListProducts(ctx context.Context) ([]controlplane.ProductCatalogRecord, error) {
	return service.controlPlane.ListProducts(ctx)
}

func (service Service) ListProductReleases(ctx context.Context, product string) ([]controlplane.Release, error) {
	return service.controlPlane.ListProductReleases(ctx, product)
}

func (service Service) GetProductRelease(ctx context.Context, product, version string) (controlplane.Release, error) {
	return service.controlPlane.GetProductRelease(ctx, product, version)
}

func (service Service) ListModules(ctx context.Context) ([]controlplane.ModuleRelease, error) {
	return service.controlPlane.ListModules(ctx)
}

func (service Service) GetEnvironmentProfile(ctx context.Context, environment string) (controlplane.EnvironmentProfile, error) {
	return service.controlPlane.GetEnvironmentProfile(ctx, environment)
}

func (service Service) PutEnvironmentProfile(
	ctx context.Context, environment string, profile controlplane.EnvironmentProfile,
) (EnvironmentProfileChangeResult, error) {
	return service.controlPlane.PutEnvironmentProfile(ctx, environment, profile)
}

func (service Service) GetEnvironmentVersions(ctx context.Context, environment string) ([]controlplane.TargetHubEntityVersion, error) {
	return service.controlPlane.GetEnvironmentVersions(ctx, environment)
}

func (service Service) PublishModules(ctx context.Context, definitions []controlplane.ModuleDefinition) ([]controlplane.ModuleRelease, error) {
	return service.controlPlane.PublishModules(ctx, definitions)
}

func (service Service) LockModule(ctx context.Context, definition controlplane.ModuleDefinition) (controlplane.ModuleLock, error) {
	return service.controlPlane.LockModule(ctx, definition)
}

func (service Service) ScanRelease(ctx context.Context, release controlplane.Release) (controlplane.ArtifactScanResult, error) {
	return service.controlPlane.ScanRelease(ctx, release)
}

func (service Service) IssueEntityCommand(ctx context.Context, request controlplane.IssueCommandRequest) (controlplane.PlanRecord, error) {
	return service.controlPlane.IssueEntityCommand(ctx, request)
}

func (service Service) UploadArtifact(ctx context.Context, mediaType string, contents io.Reader) (controlplane.BlobDescriptor, error) {
	return service.controlPlane.UploadArtifact(ctx, mediaType, contents)
}

func (service Service) ListChangeRequests(ctx context.Context, environment string) ([]controlplane.ChangeRequest, error) {
	return service.controlPlane.ListChangeRequests(ctx, environment)
}

func (service Service) GetChangeRequest(ctx context.Context, id string) (controlplane.ChangeRequest, error) {
	return service.controlPlane.GetChangeRequest(ctx, id)
}

func (service Service) ApproveChangeRequest(ctx context.Context, id, rationale string) (ChangeRequestActionResult, error) {
	return service.controlPlane.ApproveChangeRequest(ctx, id, rationale)
}

func (service Service) RejectChangeRequest(ctx context.Context, id, rationale string) (ChangeRequestActionResult, error) {
	return service.controlPlane.RejectChangeRequest(ctx, id, rationale)
}

func (service Service) ReopenChangeRequest(ctx context.Context, id string) (ChangeRequestActionResult, error) {
	return service.controlPlane.ReopenChangeRequest(ctx, id)
}

func (service Service) CancelChangeRequest(ctx context.Context, id, reason string) (ChangeRequestActionResult, error) {
	return service.controlPlane.CancelChangeRequest(ctx, id, reason)
}

func (service Service) PutTeam(ctx context.Context, name string, team controlplane.Team) (controlplane.Team, error) {
	return service.controlPlane.PutTeam(ctx, name, team)
}

func (service Service) CreateResourceGrant(ctx context.Context, grant controlplane.ResourceGrant) (controlplane.ResourceGrant, error) {
	return service.controlPlane.CreateResourceGrant(ctx, grant)
}

func (service Service) PutApprovalPolicy(ctx context.Context, id string, policy controlplane.ApprovalPolicy) (controlplane.ApprovalPolicy, error) {
	return service.controlPlane.PutApprovalPolicy(ctx, id, policy)
}

func (service Service) ProposePlan(ctx context.Context, state controlplane.DesiredState) (controlplane.PlanRecord, error) {
	return service.controlPlane.ProposePlan(ctx, state)
}

func (service Service) ApprovePlan(ctx context.Context, id, rationale string) (controlplane.PlanRecord, error) {
	return service.controlPlane.ApprovePlan(ctx, id, rationale)
}

func (service Service) CancelPlan(ctx context.Context, id, reason string) (controlplane.PlanRecord, error) {
	return service.controlPlane.CancelPlan(ctx, id, reason)
}

func (service Service) GetPlan(ctx context.Context, id string) (controlplane.PlanRecord, error) {
	return service.controlPlane.GetPlan(ctx, id)
}

func (service Service) ListPlans(ctx context.Context, query PlanListQuery) (PlanListResult, error) {
	return service.controlPlane.ListPlans(ctx, query)
}

func (service Service) ListPackagingRuns(ctx context.Context, query PackagingRunQuery) (PackagingRunList, error) {
	return service.controlPlane.ListPackagingRuns(ctx, query)
}

func (service Service) GetReportedState(ctx context.Context, environment string) (controlplane.EnvironmentReport, error) {
	return service.controlPlane.GetReportedState(ctx, environment)
}

func (service Service) ListSuppressions(ctx context.Context, environment string) ([]controlplane.SuppressionRecord, error) {
	return service.controlPlane.ListSuppressions(ctx, environment)
}

func (service Service) RemoveSuppression(ctx context.Context, id string) (controlplane.SuppressionRecord, error) {
	return service.controlPlane.RemoveSuppression(ctx, id)
}
