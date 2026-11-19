package httpapi

import (
	"encoding/json"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// environmentDetailMessage projects raw configured values, without resolving
// defaults, fetching providers or treating recorded reviewer names as authority.
func environmentDetailMessage(record controlplane.EnvironmentCatalogRecord) (*environmentsv1.EnvironmentRecord, error) {
	e := record.Environment
	config, err := json.Marshal(e.Config)
	if err != nil {
		return nil, err
	}
	result := &environmentsv1.EnvironmentRecord{Environment: &environmentsv1.Environment{
		Id: nativeIndexString(e.ID), Generation: nativeIndexString(e.Generation), PackageArchitecture: nativeIndexString(e.PackageArchitecture), ConnectionSetting: nativeIndexString(e.ConnectionSetting), DeclaredStateSource: nativeIndexString(e.DeclaredStateSource), DefaultReleaseChannel: nativeIndexString(e.DefaultReleaseChannel), Labels: nativeDetailLabels(e.Labels), ConfigJson: config, AutomaticSecurityRecall: e.AutomaticSecurityRecall, AutoSuppressionThreshold: int64(e.AutoSuppressionThreshold), Maintenance: &environmentsv1.MaintenancePolicy{},
	}, Spoke: &environmentsv1.SpokeLiveness{AgentId: nativeIndexString(record.Spoke.AgentID), State: nativeIndexString(record.Spoke.State)}}
	result.Spoke.ObservedAt, err = environmentSummaryTimestamp(record.Spoke.ObservedAt)
	if err != nil {
		return nil, err
	}
	if record.Reported != nil {
		result.Reported, err = environmentObservationMessage(*record.Reported)
		if err != nil {
			return nil, err
		}
	}
	out := result.Environment
	for _, n := range e.Namespaces {
		ns := &environmentsv1.NamespaceTarget{Rid: nativeIndexString(n.RID), Name: nativeIndexString(n.Name), Description: nativeIndexString(n.Description), EnrollmentRid: nativeIndexString(n.EnrollmentRID), Trashed: n.Trashed, OntologyCountValid: n.OntologyCountValid}
		if n.DeletionProtected != nil {
			ns.DeletionProtected = new(*n.DeletionProtected)
		}
		for _, org := range n.Organizations {
			ns.Organizations = append(ns.Organizations, &environmentsv1.Organization{DisplayName: nativeIndexString(org.DisplayName), PrimaryHostname: nativeIndexString(org.PrimaryHostname)})
		}
		if n.Installed != nil {
			ns.Installed = make(map[string]*environmentsv1.InstalledTarget, len(n.Installed))
			for k, v := range n.Installed {
				ns.Installed[nativeIndexString(k)] = &environmentsv1.InstalledTarget{Version: nativeIndexString(v.Version), ConfigDigest: nativeIndexString(v.ConfigDigest)}
			}
		}
		out.Namespaces = append(out.Namespaces, ns)
	}
	if p := e.ArtifactSecurityPolicy; p != nil {
		out.ArtifactSecurityPolicy = &environmentsv1.ArtifactSecurityPolicy{RequiredOutcome: nativeIndexString(p.RequiredOutcome), MaximumScanAgeSeconds: int64(p.MaximumScanAgeSeconds)}
	}
	if p := e.ArtifactAvailabilityPolicy; p != nil {
		out.ArtifactAvailabilityPolicy = &environmentsv1.ArtifactAvailabilityPolicy{RequiredOutcome: nativeIndexString(p.RequiredOutcome)}
	}
	for _, r := range e.ArtifactRegistries {
		out.ArtifactRegistries = append(out.ArtifactRegistries, &environmentsv1.ArtifactRegistryAssignment{Id: nativeIndexString(r.ID), Url: nativeIndexString(r.URL), Type: nativeIndexString(r.Type), Revision: int64(r.Revision), Mirrors: nativeDetailStrings(r.Mirrors), Authentication: nativeIndexString(r.Authentication), CredentialRef: nativeIndexString(r.CredentialRef), ImagePullSecretName: nativeIndexString(r.ImagePullSecretName)})
	}
	for _, s := range e.Secrets {
		out.Secrets = append(out.Secrets, &environmentsv1.ExternalSecretAssignment{Name: nativeIndexString(s.Name), Revision: int64(s.Revision), ProviderRef: nativeIndexString(s.ProviderRef), KubernetesSecretName: nativeIndexString(s.KubernetesSecretName), Keys: nativeDetailStrings(s.Keys)})
	}
	if p := e.VulnerabilityUpgrades; p != nil {
		out.VulnerabilityUpgrades = &environmentsv1.VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: p.AllowRecalledForVulnerabilities, RescanSlaSeconds: int64(p.RescanSLASeconds)}
	}
	out.Maintenance.Downtime = nativeDetailWindow(e.Maintenance.Downtime)
	out.Maintenance.NoDowntime = nativeDetailWindow(e.Maintenance.NoDowntime)
	for _, o := range e.Maintenance.Overrides {
		starts, err := environmentSummaryTimestamp(o.StartsAt)
		if err != nil {
			return nil, err
		}
		ends, err := environmentSummaryTimestamp(o.EndsAt)
		if err != nil {
			return nil, err
		}
		out.Maintenance.Overrides = append(out.Maintenance.Overrides, &environmentsv1.MaintenanceOverride{InstallationId: nativeIndexString(o.InstallationID), Type: nativeIndexString(o.Type), StartsAt: starts, EndsAt: ends, Reason: nativeIndexString(o.Reason), ApprovedBy: nativeIndexString(o.ApprovedBy)})
	}
	if h := e.HelmExecution; h != nil {
		out.HelmExecution = &environmentsv1.HelmExecutionDefaults{DefaultRolloutStrategy: nativeIndexString(h.DefaultRolloutStrategy), InstallResourceReadinessTimeoutSeconds: int64(h.InstallResourceReadinessTimeoutSeconds), UpgradeResourceReadinessTimeoutSeconds: int64(h.UpgradeResourceReadinessTimeoutSeconds)}
		if p := h.ImagePrePull; p != nil {
			out.HelmExecution.ImagePrePull = &environmentsv1.ImagePrePullPolicy{Enabled: p.Enabled, TimeoutSeconds: int64(p.TimeoutSeconds)}
		}
	}
	return result, nil
}

func nativeDetailWindow(w *controlplane.WeeklyMaintenanceWindow) *environmentsv1.WeeklyMaintenanceWindow {
	if w == nil {
		return nil
	}
	return &environmentsv1.WeeklyMaintenanceWindow{TimeZone: nativeIndexString(w.TimeZone), Intervals: nativeDetailStrings(w.Intervals)}
}
func nativeDetailStrings(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = nativeIndexString(v)
	}
	return result
}
func nativeDetailLabels(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	// Native JSON replaces invalid UTF-8. Apply the same boundary for ProtoJSON.
	for k, v := range values {
		result[nativeIndexString(k)] = nativeIndexString(v)
	}
	return result
}
