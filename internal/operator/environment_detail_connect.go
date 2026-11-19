package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func invalidEnvironmentDetail() error {
	return errors.New("operator control plane returned invalid Environment detail")
}

func (adapter *HTTPControlPlane) getEnvironmentConnect(ctx context.Context, environment string) (controlplane.EnvironmentCatalogRecord, error) {
	client := environmentsv1connect.NewEnvironmentDetailServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	request := &environmentsv1.GetEnvironmentRequest{Environment: environment}
	if adapter.spaceID != "" {
		space := adapter.spaceID
		request.SpaceId = &space
	}
	response, err := client.GetEnvironment(ctx, request)
	if err != nil {
		return controlplane.EnvironmentCatalogRecord{}, err
	}
	if response == nil {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	return environmentDetailFromWire(response.Record, environment)
}

// environmentDetailFromWire retains raw configuration and existing CLI output
// types. It never resolves policy defaults or replaces unknown observation time.
func environmentDetailFromWire(record *environmentsv1.EnvironmentRecord, environment string) (controlplane.EnvironmentCatalogRecord, error) {
	if record == nil || record.Environment == nil || record.Spoke == nil || environment == "" || record.Environment.Id != environment {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	e := record.Environment
	overflow := false
	integer := func(value int64) int {
		result := int(value)
		if int64(result) != value {
			overflow = true
		}
		return result
	}
	result := controlplane.EnvironmentCatalogRecord{Environment: controlplane.Environment{ID: e.Id, Generation: e.Generation, PackageArchitecture: e.PackageArchitecture, ConnectionSetting: e.ConnectionSetting, DeclaredStateSource: e.DeclaredStateSource, DefaultReleaseChannel: e.DefaultReleaseChannel, Labels: maps.Clone(e.Labels), AutomaticSecurityRecall: e.AutomaticSecurityRecall, AutoSuppressionThreshold: integer(e.AutoSuppressionThreshold)}, Spoke: controlplane.SpokeLiveness{AgentID: record.Spoke.AgentId, State: record.Spoke.State}}
	decoder := json.NewDecoder(bytes.NewReader(e.ConfigJson))
	decoder.UseNumber()
	if decoder.Decode(&result.Environment.Config) != nil {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	observed, err := observationWireTime(record.Spoke.ObservedAt)
	if err != nil {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	result.Spoke.ObservedAt = observed
	if record.Reported != nil {
		report, err := environmentObservationFromWire(record.Reported, environment)
		if err != nil {
			return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
		}
		result.Reported = &report
	}
	out := &result.Environment
	for _, n := range e.Namespaces {
		if n == nil {
			return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
		}
		ns := controlplane.NamespaceTarget{RID: n.Rid, Name: n.Name, Description: n.Description, EnrollmentRID: n.EnrollmentRid, Trashed: n.Trashed, OntologyCountValid: n.OntologyCountValid}
		if n.DeletionProtected != nil {
			ns.DeletionProtected = new(*n.DeletionProtected)
		}
		for _, org := range n.Organizations {
			if org == nil {
				return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
			}
			ns.Organizations = append(ns.Organizations, controlplane.Organization{DisplayName: org.DisplayName, PrimaryHostname: org.PrimaryHostname})
		}
		if n.Installed != nil {
			ns.Installed = make(map[string]controlplane.InstalledTarget, len(n.Installed))
			for key, value := range n.Installed {
				if value == nil {
					return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
				}
				ns.Installed[key] = controlplane.InstalledTarget{Version: value.Version, ConfigDigest: value.ConfigDigest}
			}
		}
		out.Namespaces = append(out.Namespaces, ns)
	}
	if p := e.ArtifactSecurityPolicy; p != nil {
		out.ArtifactSecurityPolicy = &controlplane.ArtifactSecurityPolicy{RequiredOutcome: p.RequiredOutcome, MaximumScanAgeSeconds: integer(p.MaximumScanAgeSeconds)}
	}
	if p := e.ArtifactAvailabilityPolicy; p != nil {
		out.ArtifactAvailabilityPolicy = &controlplane.ArtifactAvailabilityPolicy{RequiredOutcome: p.RequiredOutcome}
	}
	for _, r := range e.ArtifactRegistries {
		if r == nil {
			return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
		}
		out.ArtifactRegistries = append(out.ArtifactRegistries, controlplane.ArtifactRegistryAssignment{ID: r.Id, URL: r.Url, Type: r.Type, Revision: integer(r.Revision), Mirrors: slices.Clone(r.Mirrors), Authentication: r.Authentication, CredentialRef: r.CredentialRef, ImagePullSecretName: r.ImagePullSecretName})
	}
	for _, s := range e.Secrets {
		if s == nil {
			return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
		}
		out.Secrets = append(out.Secrets, controlplane.ExternalSecretAssignment{Name: s.Name, Revision: integer(s.Revision), ProviderRef: s.ProviderRef, KubernetesSecretName: s.KubernetesSecretName, Keys: slices.Clone(s.Keys)})
	}
	if p := e.VulnerabilityUpgrades; p != nil {
		out.VulnerabilityUpgrades = &controlplane.VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: p.AllowRecalledForVulnerabilities, RescanSLASeconds: integer(p.RescanSlaSeconds)}
	}
	if m := e.Maintenance; m != nil {
		out.Maintenance.Downtime = environmentDetailWindowFromWire(m.Downtime)
		out.Maintenance.NoDowntime = environmentDetailWindowFromWire(m.NoDowntime)
		for _, o := range m.Overrides {
			if o == nil {
				return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
			}
			starts, err := observationWireTime(o.StartsAt)
			if err != nil {
				return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
			}
			ends, err := observationWireTime(o.EndsAt)
			if err != nil {
				return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
			}
			out.Maintenance.Overrides = append(out.Maintenance.Overrides, controlplane.MaintenanceOverride{InstallationID: o.InstallationId, Type: o.Type, StartsAt: starts, EndsAt: ends, Reason: o.Reason, ApprovedBy: o.ApprovedBy})
		}
	}
	if h := e.HelmExecution; h != nil {
		out.HelmExecution = &controlplane.HelmExecutionDefaults{DefaultRolloutStrategy: h.DefaultRolloutStrategy, InstallResourceReadinessTimeoutSeconds: integer(h.InstallResourceReadinessTimeoutSeconds), UpgradeResourceReadinessTimeoutSeconds: integer(h.UpgradeResourceReadinessTimeoutSeconds)}
		if p := h.ImagePrePull; p != nil {
			out.HelmExecution.ImagePrePull = &controlplane.ImagePrePullPolicy{Enabled: p.Enabled, TimeoutSeconds: integer(p.TimeoutSeconds)}
		}
	}
	if overflow {
		return controlplane.EnvironmentCatalogRecord{}, invalidEnvironmentDetail()
	}
	return result, nil
}

func environmentDetailWindowFromWire(window *environmentsv1.WeeklyMaintenanceWindow) *controlplane.WeeklyMaintenanceWindow {
	if window == nil {
		return nil
	}
	return &controlplane.WeeklyMaintenanceWindow{TimeZone: window.TimeZone, Intervals: slices.Clone(window.Intervals)}
}
