package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

const maxOperatorResponseBytes = 1 << 20

type SPIFFEX509Source interface {
	x509svid.Source
	x509bundle.Source
}

// NewSPIFFEHTTPClient is the production outbound transport adapter. It uses a
// rotating operator X509-SVID and authorizes the Hub by its exact SPIFFE ID.
func NewSPIFFEHTTPClient(source SPIFFEX509Source, expectedHubID string) (*http.Client, error) {
	if source == nil {
		return nil, fmt.Errorf("SPIFFE X.509 source is required")
	}
	hubID, err := spiffeid.FromString(strings.TrimSpace(expectedHubID))
	if err != nil {
		return nil, fmt.Errorf("parse control plane SPIFFE ID: %w", err)
	}
	if hubID.String() != controlplane.HubSPIFFEID {
		return nil, fmt.Errorf("control plane SPIFFE ID must be %s", controlplane.HubSPIFFEID)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.TLSClientConfig = tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(hubID))
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

// HTTPControlPlane is an outbound adapter for the operator application port.
type HTTPControlPlane struct {
	spaceID string
	baseURL *url.URL
	client  *http.Client
}

func (adapter *HTTPControlPlane) ListArtifactRegistries(ctx context.Context) ([]controlplane.ArtifactRegistryRecord, error) {
	var response struct {
		ArtifactRegistries []controlplane.ArtifactRegistryRecord `json:"artifactRegistries"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/artifact-registries", nil, &response)
	return response.ArtifactRegistries, err
}

func (adapter *HTTPControlPlane) InstallModule(
	ctx context.Context, environment, module, version, releaseChannel string, variables map[string]string,
) (controlplane.ModuleInstallation, error) {
	var result controlplane.ModuleInstallation
	body := map[string]any{"module": module, "variables": variables}
	if version != "" {
		body["version"] = version
	}
	if releaseChannel != "" {
		body["releaseChannel"] = releaseChannel
	}
	err := adapter.request(ctx, http.MethodPost, "/v1/environments/"+url.PathEscape(environment)+"/module-installations", body, &result)
	return result, err
}

func (adapter *HTTPControlPlane) PublishProductRelease(
	ctx context.Context, bundle controlplane.HelmProductReleaseBundle,
) (ProductReleasePublication, error) {
	if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
		return ProductReleasePublication{}, err
	}
	manifest := bundle.Manifest
	coordinate := manifest.ProductGroup + ":" + manifest.ProductName + ":" + manifest.ProductVersion
	// The public route carries only what the pinned snapshot describes, so a
	// manifest saying more than that goes to the Conduit-native one, which
	// persists it. A dependency's optional flag and recommended version are
	// exactly that case: ProductDependencyLocator has neither field, and the Hub
	// decodes the public body with DisallowUnknownFields, so sending them earns
	// a 400. Dropping them instead would be worse than the 400 -- an optional
	// dependency silently becoming required changes what the Release means.
	if adapter.spaceID != "" || len(bundle.DefaultConfig) > 0 || len(bundle.ConfigSchema) > 0 || manifestExceedsPublicContract(manifest) {
		var ignored struct{}
		err := adapter.request(ctx, http.MethodPost, adapter.withSpaceSelection("/v1/product-publications/helm-chart"), bundle, &ignored)
		return ProductReleasePublication{MavenCoordinate: coordinate, Created: err == nil}, err
	}
	artifacts := make([]map[string]string, 0, len(manifest.Extensions.Artifacts))
	for _, artifact := range manifest.Extensions.Artifacts {
		artifacts = append(artifacts, map[string]string{"artifactUrl": artifact.URI})
	}
	// Projected field by field, exactly as artifacts above already is. Handing
	// the native struct to the public route is what leaked recommendedVersion
	// and optional into a body the contract does not describe; both are
	// omitempty, so a manifest that happened not to set them hid the leak.
	dependencies := make([]map[string]string, 0, len(manifest.Extensions.ProductDependencies))
	for _, dependency := range manifest.Extensions.ProductDependencies {
		dependencies = append(dependencies, map[string]string{
			"productGroup":   dependency.ProductGroup,
			"productName":    dependency.ProductName,
			"minimumVersion": dependency.MinimumVersion,
			"maximumVersion": dependency.MaximumVersion,
		})
	}
	extensions := map[string]any{
		"helmChart":           manifest.Extensions.HelmChart,
		"artifacts":           artifacts,
		"productDependencies": dependencies,
	}
	body := map[string]any{"extensions": extensions}
	var ignored struct{}
	err := adapter.request(ctx, http.MethodPost, "/api/v0/products/helmChart/"+url.PathEscape(coordinate)+"/publish", body, &ignored)
	return ProductReleasePublication{MavenCoordinate: coordinate, Created: err == nil}, err
}

func (adapter *HTTPControlPlane) PublishTerraformProductRelease(
	ctx context.Context, manifest controlplane.TerraformProductManifest,
) (controlplane.ProductPublication, error) {
	var publication controlplane.ProductPublication
	err := adapter.request(ctx, http.MethodPost, adapter.withSpaceSelection("/v1/product-publications/terraform"), manifest, &publication)
	return publication, err
}

func (adapter *HTTPControlPlane) ListProductPublications(
	ctx context.Context, product string,
) ([]controlplane.ProductPublication, error) {
	var response struct {
		Publications []controlplane.ProductPublication `json:"publications"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(product)+"/publications", nil, &response)
	return response.Publications, err
}

func (adapter *HTTPControlPlane) ListEnvironments(ctx context.Context) ([]controlplane.EnvironmentCatalogRecord, error) {
	return listCatalogPages(ctx, adapter, adapter.withSpaceSelection("/v1/environments"), func(page catalogPageEnvelope[controlplane.EnvironmentCatalogRecord]) []controlplane.EnvironmentCatalogRecord {
		return page.Environments
	})
}

func (adapter *HTTPControlPlane) GetEnvironment(ctx context.Context, environment string) (controlplane.EnvironmentCatalogRecord, error) {
	var result controlplane.EnvironmentCatalogRecord
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment), nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) ListEntities(ctx context.Context, environment string) ([]controlplane.Entity, error) {
	var response struct {
		Entities []controlplane.Entity `json:"entities"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/entities", nil, &response)
	return response.Entities, err
}

func (adapter *HTTPControlPlane) ListProducts(ctx context.Context) ([]controlplane.ProductCatalogRecord, error) {
	return listCatalogPages(ctx, adapter, "/v1/products", func(page catalogPageEnvelope[controlplane.ProductCatalogRecord]) []controlplane.ProductCatalogRecord {
		return page.Products
	})
}

type catalogPageEnvelope[T any] struct {
	Environments []T    `json:"environments"`
	Products     []T    `json:"products"`
	NextCursor   string `json:"nextCursor"`
}

func listCatalogPages[T any](
	ctx context.Context, adapter *HTTPControlPlane, path string, values func(catalogPageEnvelope[T]) []T,
) ([]T, error) {
	result := make([]T, 0)
	cursor := ""
	seen := map[string]struct{}{}
	for {
		parsed, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		pageQueryValues := parsed.Query()
		pageQueryValues.Set("limit", "20")
		pageQueryValues.Set("view", "full")
		parsed.RawQuery = pageQueryValues.Encode()
		query := parsed.String()
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		var page catalogPageEnvelope[T]
		if err := adapter.request(ctx, http.MethodGet, query, nil, &page); err != nil {
			return nil, err
		}
		result = append(result, values(page)...)
		if page.NextCursor == "" {
			return result, nil
		}
		if _, duplicate := seen[page.NextCursor]; duplicate {
			return nil, fmt.Errorf("operator control plane repeated a catalog cursor")
		}
		seen[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func (adapter *HTTPControlPlane) ListProductReleases(ctx context.Context, product string) ([]controlplane.Release, error) {
	var response struct {
		Releases []controlplane.Release `json:"releases"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(product)+"/releases", nil, &response)
	return response.Releases, err
}

func (adapter *HTTPControlPlane) GetProductRelease(ctx context.Context, product, version string) (controlplane.Release, error) {
	var result controlplane.Release
	err := adapter.request(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(product)+"/releases/"+url.PathEscape(version), nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) ListModules(ctx context.Context) ([]controlplane.ModuleRelease, error) {
	var response struct {
		Releases []controlplane.ModuleRelease `json:"releases"`
	}
	err := adapter.request(ctx, http.MethodGet, "/v1/modules", nil, &response)
	return response.Releases, err
}

func (adapter *HTTPControlPlane) GetEnvironmentProfile(ctx context.Context, environment string) (controlplane.EnvironmentProfile, error) {
	var result controlplane.EnvironmentProfile
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/profile", nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) PutEnvironmentProfile(
	ctx context.Context, environment string, profile controlplane.EnvironmentProfile,
) (EnvironmentProfileChangeResult, error) {
	var result EnvironmentProfileChangeResult
	err := adapter.request(ctx, http.MethodPut, "/v1/environments/"+url.PathEscape(environment)+"/profile", profile, &result)
	return result, err
}

func (adapter *HTTPControlPlane) GetEnvironmentVersions(ctx context.Context, environment string) ([]controlplane.TargetHubEntityVersion, error) {
	var result []controlplane.TargetHubEntityVersion
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/target-hub-versions", nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) PublishModules(ctx context.Context, definitions []controlplane.ModuleDefinition) ([]controlplane.ModuleRelease, error) {
	var response struct {
		Releases []controlplane.ModuleRelease `json:"releases"`
	}
	err := adapter.request(ctx, http.MethodPost, "/v1/modules/releases", map[string]any{"definitions": definitions}, &response)
	return response.Releases, err
}

func (adapter *HTTPControlPlane) LockModule(ctx context.Context, definition controlplane.ModuleDefinition) (controlplane.ModuleLock, error) {
	var result controlplane.ModuleLock
	err := adapter.request(ctx, http.MethodPost, "/v1/modules/lock", definition, &result)
	return result, err
}

func (adapter *HTTPControlPlane) ScanRelease(ctx context.Context, release controlplane.Release) (controlplane.ArtifactScanResult, error) {
	var result controlplane.ArtifactScanResult
	err := adapter.request(ctx, http.MethodPost, "/v1/artifact-scans", release, &result)
	return result, err
}

func (adapter *HTTPControlPlane) IssueEntityCommand(ctx context.Context, request controlplane.IssueCommandRequest) (controlplane.PlanRecord, error) {
	var result controlplane.PlanRecord
	err := adapter.request(ctx, http.MethodPost, "/v1/commands", request, &result)
	return result, err
}

func (adapter *HTTPControlPlane) UploadArtifact(ctx context.Context, mediaType string, contents io.Reader) (controlplane.BlobDescriptor, error) {
	target := adapter.baseURL.ResolveReference(&url.URL{Path: "/v1/artifacts/blobs"})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), contents)
	if err != nil {
		return controlplane.BlobDescriptor{}, fmt.Errorf("create artifact upload request: %w", err)
	}
	request.Header.Set("Content-Type", mediaType)
	response, err := adapter.client.Do(request)
	if err != nil {
		return controlplane.BlobDescriptor{}, fmt.Errorf("upload artifact: %w", err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxOperatorResponseBytes+1))
	if err != nil {
		return controlplane.BlobDescriptor{}, fmt.Errorf("read artifact upload response: %w", err)
	}
	if len(encoded) > maxOperatorResponseBytes {
		return controlplane.BlobDescriptor{}, fmt.Errorf("artifact upload response exceeds %d bytes", maxOperatorResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return controlplane.BlobDescriptor{}, fmt.Errorf("artifact upload returned %d: %s", response.StatusCode, strings.TrimSpace(string(encoded)))
	}
	var descriptor controlplane.BlobDescriptor
	if err := json.Unmarshal(encoded, &descriptor); err != nil {
		return controlplane.BlobDescriptor{}, fmt.Errorf("decode artifact descriptor: %w", err)
	}
	return descriptor, nil
}

func (adapter *HTTPControlPlane) ListChangeRequests(ctx context.Context, environment string) ([]controlplane.ChangeRequest, error) {
	query := url.Values{}
	if strings.TrimSpace(environment) != "" {
		query.Set("environment", environment)
	}
	path := "/v1/change-requests"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response struct {
		ChangeRequests []controlplane.ChangeRequest `json:"changeRequests"`
	}
	err := adapter.request(ctx, http.MethodGet, path, nil, &response)
	return response.ChangeRequests, err
}

func (adapter *HTTPControlPlane) GetChangeRequest(ctx context.Context, id string) (controlplane.ChangeRequest, error) {
	var result controlplane.ChangeRequest
	err := adapter.request(ctx, http.MethodGet, "/v1/change-requests/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (adapter *HTTPControlPlane) ApproveChangeRequest(ctx context.Context, id, rationale string) (ChangeRequestActionResult, error) {
	return adapter.changeRequestAction(ctx, id, "approve", map[string]string{"rationale": rationale})
}

func (adapter *HTTPControlPlane) RejectChangeRequest(ctx context.Context, id, rationale string) (ChangeRequestActionResult, error) {
	return adapter.changeRequestAction(ctx, id, "reject", map[string]string{"rationale": rationale})
}

func (adapter *HTTPControlPlane) ReopenChangeRequest(ctx context.Context, id string) (ChangeRequestActionResult, error) {
	return adapter.changeRequestAction(ctx, id, "reopen", nil)
}

func (adapter *HTTPControlPlane) CancelChangeRequest(ctx context.Context, id, reason string) (ChangeRequestActionResult, error) {
	return adapter.changeRequestAction(ctx, id, "cancel", map[string]string{"reason": reason})
}

func (adapter *HTTPControlPlane) changeRequestAction(
	ctx context.Context, id, action string, body any,
) (ChangeRequestActionResult, error) {
	var result ChangeRequestActionResult
	err := adapter.request(
		ctx, http.MethodPost, "/v1/change-requests/"+url.PathEscape(id)+"/"+action, body, &result,
	)
	return result, err
}

func NewHTTPControlPlane(rawURL string, client *http.Client) (*HTTPControlPlane, error) {
	baseURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("control plane URL must be an absolute http(s) URL")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("control plane URL must use http or https")
	}
	// Every request path is absolute, so ResolveReference would discard a base
	// path rather than prefix it: https://hub.example/platform silently becomes
	// https://hub.example/v1/.... Refuse the URL instead of misrouting quietly.
	if strings.Trim(baseURL.Path, "/") != "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("control plane URL must be a scheme and host without a path, query, or fragment")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPControlPlane{baseURL: baseURL, client: client}, nil
}

func (adapter *HTTPControlPlane) PutTeam(ctx context.Context, name string, team controlplane.Team) (controlplane.Team, error) {
	var stored controlplane.Team
	err := adapter.request(ctx, http.MethodPut, "/v1/teams/"+url.PathEscape(name), team, &stored)
	return stored, err
}

func (adapter *HTTPControlPlane) CreateResourceGrant(ctx context.Context, grant controlplane.ResourceGrant) (controlplane.ResourceGrant, error) {
	var stored controlplane.ResourceGrant
	err := adapter.request(ctx, http.MethodPost, "/v1/resource-grants", grant, &stored)
	return stored, err
}

func (adapter *HTTPControlPlane) PutApprovalPolicy(ctx context.Context, id string, policy controlplane.ApprovalPolicy) (controlplane.ApprovalPolicy, error) {
	var stored controlplane.ApprovalPolicy
	err := adapter.request(ctx, http.MethodPut, "/v1/approval-policies/"+url.PathEscape(id), policy, &stored)
	return stored, err
}

func (adapter *HTTPControlPlane) ProposePlan(ctx context.Context, state controlplane.DesiredState) (controlplane.PlanRecord, error) {
	var record controlplane.PlanRecord
	err := adapter.request(ctx, http.MethodPost, "/v1/plans", state, &record)
	return record, err
}

func (adapter *HTTPControlPlane) ApprovePlan(ctx context.Context, id, rationale string) (controlplane.PlanRecord, error) {
	var record controlplane.PlanRecord
	err := adapter.request(ctx, http.MethodPost, "/v1/plans/"+url.PathEscape(id)+"/approvals", map[string]string{"rationale": rationale}, &record)
	return record, err
}

func (adapter *HTTPControlPlane) CancelPlan(ctx context.Context, id, reason string) (controlplane.PlanRecord, error) {
	var record controlplane.PlanRecord
	err := adapter.request(ctx, http.MethodPost, "/v1/plans/"+url.PathEscape(id)+"/cancel", map[string]string{"reason": reason}, &record)
	return record, err
}

func (adapter *HTTPControlPlane) GetPlan(ctx context.Context, id string) (controlplane.PlanRecord, error) {
	var record controlplane.PlanRecord
	err := adapter.request(ctx, http.MethodGet, "/v1/plans/"+url.PathEscape(id), nil, &record)
	return record, err
}

func (adapter *HTTPControlPlane) GetReportedState(ctx context.Context, environment string) (controlplane.EnvironmentReport, error) {
	var report controlplane.EnvironmentReport
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/reported-state", nil, &report)
	return report, err
}

func (adapter *HTTPControlPlane) ListSuppressions(ctx context.Context, environment string) ([]controlplane.SuppressionRecord, error) {
	var records []controlplane.SuppressionRecord
	err := adapter.request(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(environment)+"/suppressions", nil, &records)
	return records, err
}

func (adapter *HTTPControlPlane) RemoveSuppression(ctx context.Context, id string) (controlplane.SuppressionRecord, error) {
	var record controlplane.SuppressionRecord
	err := adapter.request(ctx, http.MethodDelete, "/v1/suppressions/"+url.PathEscape(id), nil, &record)
	return record, err
}

func (adapter *HTTPControlPlane) request(ctx context.Context, method, path string, body, output any) error {
	return adapter.requestWithMediaType(ctx, method, path, body, output, "application/json")
}

func (adapter *HTTPControlPlane) requestWithMediaType(ctx context.Context, method, path string, body, output any, mediaType string) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode operator request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	parsedPath, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse operator request path: %w", err)
	}
	target := adapter.baseURL.ResolveReference(parsedPath)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return fmt.Errorf("create operator request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", mediaType)
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		return fmt.Errorf("operator control plane request: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxOperatorResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read operator control plane response: %w", err)
	}
	if len(contents) > maxOperatorResponseBytes {
		return fmt.Errorf("operator control plane response exceeds %d bytes", maxOperatorResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(contents, &problem) == nil && problem.Error != "" {
			return fmt.Errorf("operator control plane returned %d: %s", response.StatusCode, problem.Error)
		}
		return fmt.Errorf("operator control plane returned %d", response.StatusCode)
	}
	if output != nil {
		if err := json.Unmarshal(contents, output); err != nil {
			return fmt.Errorf("decode operator control plane response: %w", err)
		}
	}
	return nil
}

// manifestExceedsPublicContract reports whether the manifest says something the
// pinned public publish contract has no field for. Today that is a dependency's
// optional flag or recommended version: ProductDependencyLocator carries only
// group, name and the version bounds. Such a Release is published through the
// Conduit-native route, which persists the whole ApolloManifestDependency,
// rather than through /api/v0 with the extra fields dropped.
func manifestExceedsPublicContract(manifest controlplane.ApolloProductManifest) bool {
	for _, dependency := range manifest.Extensions.ProductDependencies {
		if dependency.Optional || dependency.RecommendedVersion != "" {
			return true
		}
	}
	return false
}
