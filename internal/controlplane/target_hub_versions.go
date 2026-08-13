package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// In a connected topology the source Hub can see what a target already runs. In a
// disconnected one it cannot, so an incremental Bundle has to guess: the only
// baseline available is the source's own record of what it previously built for
// that target. That record is wrong whenever the two diverge — the source Hub was
// rebuilt and lost its Bundle history, a Bundle was produced but never carried
// across, or the target was recovered from a backup.
//
// Guessing wrong is not symmetric. Over-sending is wasted bytes; under-sending
// produces a Bundle the target cannot apply, because it references catalog
// resources the target never received. Target Hub versions close that by letting
// an operator carry the target's installed versions back as JSON, so the source
// baselines against what the target actually has.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/export-import/target-hub-versions/

// TargetHubEntityVersion is one installation the target Hub reports running.
type TargetHubEntityVersion struct {
	Environment    string `json:"environment"`
	InstallationID string `json:"installationId"`
	Product        string `json:"product"`
	Version        string `json:"version"`
}

// TargetHubVersions is the source Hub's recorded knowledge of one target Hub.
// RecordedAt is when the operator supplied it, not when the target observed it:
// the source cannot verify the latter, so it does not claim to.
type TargetHubVersions struct {
	TargetHub  string                   `json:"targetHub"`
	Entities   []TargetHubEntityVersion `json:"entities"`
	RecordedAt time.Time                `json:"recordedAt"`
}

// EnvironmentEntityVersions reports what an Environment on this Hub runs, in the
// form a source Hub records as target Hub versions. This is the export side of the
// exchange, run on the target.
func (service *BundleService) EnvironmentEntityVersions(
	ctx context.Context, environment string,
) ([]TargetHubEntityVersion, error) {
	report, err := service.repository.GetEnvironmentReport(ctx, environment)
	if err != nil {
		return nil, err
	}
	versions := make([]TargetHubEntityVersion, 0, len(report.Entities))
	for _, entity := range report.Entities {
		if strings.TrimSpace(entity.Version) == "" {
			continue
		}
		versions = append(versions, TargetHubEntityVersion{
			Environment: environment, InstallationID: entity.InstallationID,
			Product: entity.Product, Version: entity.Version,
		})
	}
	sortTargetHubVersions(versions)
	return versions, nil
}

// DecodeTargetHubVersions parses the JSON an operator carried from a target Hub.
// It accepts either a bare list of installations or a full record, so the payload
// a target produced can be pasted verbatim.
func DecodeTargetHubVersions(payload []byte) ([]TargetHubEntityVersion, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil, ValidationErrors{{Path: "targetHubVersions", Message: "payload is empty"}}
	}
	var versions []TargetHubEntityVersion
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &versions); err != nil {
			return nil, ValidationErrors{{Path: "targetHubVersions", Message: "decode installations: " + err.Error()}}
		}
	} else {
		var record TargetHubVersions
		if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
			return nil, ValidationErrors{{Path: "targetHubVersions", Message: "decode record: " + err.Error()}}
		}
		versions = record.Entities
	}
	if len(versions) == 0 {
		return nil, ValidationErrors{{Path: "targetHubVersions.entities", Message: "at least one installation is required"}}
	}
	errs := ValidationErrors{}
	seen := make(map[string]struct{}, len(versions))
	for index, version := range versions {
		path := fmt.Sprintf("targetHubVersions.entities[%d]", index)
		if !dnsLabelPattern.MatchString(version.Environment) {
			errs = append(errs, ValidationError{Path: path + ".environment", Message: "must be a DNS label"})
		}
		if strings.TrimSpace(version.InstallationID) == "" {
			errs = append(errs, ValidationError{Path: path + ".installationId", Message: "must not be empty"})
		}
		if !coordinatePattern.MatchString(version.Product) {
			errs = append(errs, ValidationError{Path: path + ".product", Message: "must be a Product coordinate"})
		}
		if !versionPattern.MatchString(version.Version) {
			errs = append(errs, ValidationError{Path: path + ".version", Message: "must be an exact version"})
		}
		key := version.Environment + "/" + version.InstallationID
		if _, duplicate := seen[key]; duplicate {
			errs = append(errs, ValidationError{Path: path, Message: "duplicate installation " + key})
		}
		seen[key] = struct{}{}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	sortTargetHubVersions(versions)
	return versions, nil
}

// RecordTargetHubVersions stores what a target Hub runs, against the Pipeline that
// builds for it. Recording replaces the previous record rather than merging:
// an installation the target no longer reports has genuinely gone, and merging
// would leave the source believing it is still there forever.
func (service *BundleService) RecordTargetHubVersions(
	ctx context.Context, pipelineID, targetHub string, versions []TargetHubEntityVersion,
) (ExportPipeline, error) {
	targetHub = strings.TrimSpace(targetHub)
	if targetHub == "" || len(targetHub) > 255 || strings.ContainsAny(targetHub, "\r\n\x00") {
		return ExportPipeline{}, ValidationErrors{{Path: "targetHub", Message: "must be a non-empty single-line identifier no longer than 255 characters"}}
	}
	payload, err := json.Marshal(versions)
	if err != nil {
		return ExportPipeline{}, fmt.Errorf("encode target Hub versions: %w", err)
	}
	validated, err := DecodeTargetHubVersions(payload)
	if err != nil {
		return ExportPipeline{}, err
	}
	pipeline, err := service.repository.GetExportPipeline(ctx, pipelineID)
	if err != nil {
		return ExportPipeline{}, err
	}
	if !exportPipelineIsActive(pipeline.Status) {
		return ExportPipeline{}, fmt.Errorf("%w: Export Pipeline is not active", ErrConflict)
	}
	if pipeline.TargetHubVersions == nil {
		pipeline.TargetHubVersions = map[string]TargetHubVersions{}
	}
	recorded := append([]TargetHubEntityVersion(nil), validated...)
	pipeline.TargetHubVersions[targetHub] = TargetHubVersions{
		TargetHub: targetHub, Entities: recorded, RecordedAt: service.now().UTC(),
	}
	pipeline.UpdatedAt = service.now().UTC()
	pipeline.Revision++
	return service.repository.PutExportPipeline(ctx, pipeline)
}

// targetHubReleaseBaseline turns recorded target versions into baseline entries
// for the exact Releases the target already holds.
//
// It only ever adds to the baseline computed from Bundle history, and only for
// RELEASE resources. A Release the target runs is one it must already have
// received, so omitting it from an incremental Bundle is safe. Nothing else is
// inferred: that the target runs a version says nothing about which Teams,
// Modules, or Environment settings it holds.
func targetHubReleaseBaseline(
	baseline map[string]BundleResource, releases map[string]Release, recorded TargetHubVersions,
) map[string]BundleResource {
	if len(recorded.Entities) == 0 {
		return baseline
	}
	held := make(map[string]struct{}, len(recorded.Entities))
	for _, entity := range recorded.Entities {
		held[entity.Product+":"+entity.Version] = struct{}{}
	}
	for key, release := range releases {
		if _, runs := held[release.Product+":"+release.Version]; !runs {
			continue
		}
		resourceKey := BundleSelectRelease + ":" + key
		if _, alreadyKnown := baseline[resourceKey]; alreadyKnown {
			continue
		}
		addBundleResource(baseline, BundleSelectRelease, key, release)
	}
	return baseline
}

func sortTargetHubVersions(versions []TargetHubEntityVersion) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Environment != versions[j].Environment {
			return versions[i].Environment < versions[j].Environment
		}
		return versions[i].InstallationID < versions[j].InstallationID
	})
}
