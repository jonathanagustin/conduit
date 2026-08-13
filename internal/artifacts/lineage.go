package artifacts

import "sort"

// Baseline replays a Bundle chain into the state it leaves behind, and returns
// that state together with the ID of the last Bundle that contributed to it.
//
// Checkpoints are skipped. A checkpoint is a restatement of what the chain
// already said, so replaying one would apply the same resources twice — the
// second time overwriting nothing, but the returned "last" would then name a
// Bundle that added nothing, and the next incremental would claim to depend on
// it.
func Baseline(bundles []Envelope) (map[string]Resource, string) {
	baseline := make(map[string]Resource)
	last := ""
	for _, bundle := range bundles {
		if bundle.Manifest.Kind == KindCheckpoint {
			continue
		}
		last = bundle.Manifest.ID
		for _, resource := range bundle.Manifest.Resources {
			key := resource.Type + ":" + resource.ID
			if resource.Deleted {
				delete(baseline, key)
			} else {
				baseline[key] = resource
			}
		}
	}
	return baseline, last
}

// ChangedResources is the diff that becomes an incremental Bundle: everything
// added or altered since the baseline, plus a tombstone for everything the
// baseline held that is now gone.
//
// The tombstones are the part worth stating. Without them a resource deleted at
// the source would simply stop being mentioned, and a far end replaying the
// chain would keep it forever.
//
// The result is sorted so that the same inputs always produce the same bytes,
// and therefore the same manifest identity.
func ChangedResources(resources []Resource, baseline map[string]Resource) []Resource {
	changed := make([]Resource, 0)
	current := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		key := resource.Type + ":" + resource.ID
		current[key] = struct{}{}
		if previous, exists := baseline[key]; !exists || previous.Digest != resource.Digest {
			changed = append(changed, resource)
		}
	}
	for key, previous := range baseline {
		if _, exists := current[key]; exists {
			continue
		}
		tombstone := Resource{Type: previous.Type, ID: previous.ID, Deleted: true}
		tombstone.Digest = ResourceDigest(tombstone)
		changed = append(changed, tombstone)
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Type != changed[j].Type {
			return changed[i].Type < changed[j].Type
		}
		return changed[i].ID < changed[j].ID
	})
	return changed
}

// IncrementalsSinceCheckpoint counts how far the chain has run since it was
// last collapsed. It walks backwards and stops at the first checkpoint, because
// what matters is the length of the tail a far end would have to replay, not
// how many incrementals the chain has ever contained.
func IncrementalsSinceCheckpoint(bundles []Envelope) int {
	count := 0
	for index := len(bundles) - 1; index >= 0; index-- {
		if bundles[index].Manifest.Kind == KindCheckpoint {
			break
		}
		if bundles[index].Manifest.Kind == KindIncremental {
			count++
		}
	}
	return count
}
