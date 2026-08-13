package optimization

import "testing"

func TestPlatformStateRequirementManifestHasExplicitCoverageForEveryPlane(t *testing.T) {
	entries := PlatformStateRequirementManifest()
	if len(entries) < 12 {
		t.Fatalf("manifest is too small to be a whole-system field taxonomy: %d", len(entries))
	}
	seen := map[string]bool{}
	planes := map[StatePlane]bool{}
	available := map[string]bool{}
	for index, entry := range entries {
		if entry.ID == "" || entry.Authority == "" || entry.RequirementID == "" || entry.MaximumStalenessSecs < 0 {
			t.Fatalf("invalid manifest entry: %#v", entry)
		}
		if seen[entry.ID] || (index > 0 && entries[index-1].ID >= entry.ID) {
			t.Fatalf("manifest IDs are not unique and canonical: %#v", entries)
		}
		seen[entry.ID] = true
		planes[entry.Plane] = true
		switch entry.Status {
		case StateManifestAvailable:
			if entry.CallID == "" || entry.Extractor == "" || entry.Reason != "" {
				t.Fatalf("available entry lacks executable binding or has an unknown reason: %#v", entry)
			}
			available[entry.RequirementID] = true
		case StateManifestUnknown:
			if entry.Reason == "" {
				t.Fatalf("unknown entry lacks an explicit reason: %#v", entry)
			}
		default:
			t.Fatalf("invalid manifest status: %#v", entry)
		}
	}
	for _, plane := range []StatePlane{StateDesired, StateAdmitted, StateExecuting, StateObserved, StateBilled, StateExperienced} {
		if !planes[plane] {
			t.Fatalf("manifest omitted state plane %s", plane)
		}
	}
	requirements, calls := PlatformStateAcquisitionRegistry()
	for _, requirement := range requirements {
		if !available[requirement.ID] {
			t.Fatalf("executable registry requirement %q is not marked available in manifest", requirement.ID)
		}
	}
	if len(available) != 4 || len(calls) != 3 {
		t.Fatalf("manifest/registry availability changed unexpectedly: available=%d calls=%d", len(available), len(calls))
	}
}

func TestPlatformStateRequirementManifestDoesNotPretendCandidateProvidersAreAvailable(t *testing.T) {
	for _, entry := range PlatformStateRequirementManifest() {
		if entry.Authority != "platform-hub" && entry.Status == StateManifestAvailable {
			t.Fatalf("external authority became executable without admission: %#v", entry)
		}
	}
}
