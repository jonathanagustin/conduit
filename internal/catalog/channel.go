package catalog

// Promotion is the part of a promotion record this package reads: who moved
// which version, from which Channel to which, and whether it succeeded.
//
// The caller's own promotion type carries far more — approvals, conditions,
// health evidence, the environments involved — and none of it changes which
// Channels a Release has reached. Taking only these five fields is what keeps
// Catalog from depending on Governance, and the conversion is one loop on the
// caller's side.
type Promotion struct {
	Product     string
	Version     string
	FromChannel string
	ToChannel   string
	Passed      bool
}

// EffectiveChannels is every Channel a Release has reached: the ones it declares
// (or, declaring none, the ones its version implies) plus everything reachable
// from those by a passed promotion.
//
// The fixed point matters. Promotions arrive in no particular order, so a single
// pass would apply DEV→RC before RC→RELEASE only by luck; repeating until
// nothing changes gets the same answer whatever the order.
func EffectiveChannels(release Release, promotions []Promotion) []string {
	channels := append([]string(nil), release.Channels...)
	if len(channels) == 0 {
		channels = DefaultChannelsForVersion(release.Version)
	}
	for changed := true; changed; {
		changed = false
		for _, promotion := range promotions {
			if promotion.Passed && promotion.Product == release.Product && promotion.Version == release.Version &&
				contains(channels, promotion.FromChannel) && !contains(channels, promotion.ToChannel) {
				channels = append(channels, promotion.ToChannel)
				changed = true
			}
		}
	}
	return channels
}

// ReleaseOnChannel reports whether one Release has reached a Channel.
func ReleaseOnChannel(release Release, promotions []Promotion, channel string) bool {
	for _, candidate := range EffectiveChannels(release, promotions) {
		if candidate == channel {
			return true
		}
	}
	return false
}

// HasReleaseOnChannel reports whether any Release of a Product has reached a
// Channel — the question asked before promoting something toward it.
func HasReleaseOnChannel(releases []Release, promotions []Promotion, product, channel string) bool {
	for _, release := range releases {
		if release.Product == product && ReleaseOnChannel(release, promotions, channel) {
			return true
		}
	}
	return false
}
