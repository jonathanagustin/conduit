package domain

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

type releaseVersionKind uint8

const (
	versionReleaseCandidate releaseVersionKind = iota
	versionReleaseCandidateSnapshot
	versionRelease
	versionReleaseSnapshot
)

type releaseVersion struct {
	core     [3]uint32
	kind     releaseVersionKind
	rc       uint32
	snapshot uint32
}

type releaseVersionPattern struct{}
type releaseVersionMatcherPattern struct{}

var (
	VersionPattern        releaseVersionPattern
	VersionMatcherPattern releaseVersionMatcherPattern
	versionPartsPattern   = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)(?:-rc([0-9]+)(?:-([0-9]+)-g([a-f0-9]+))?|-([0-9]+)-g([a-f0-9]+))?$`)
	matcherPartsPattern   = regexp.MustCompile(`^(x\.x\.x|([0-9]+)\.x\.x|([0-9]+)\.([0-9]+)\.x|([0-9]+)\.([0-9]+)\.([0-9]+))$`)
	// Non-orderable versions are a documented second class: a Product Release may
	// carry one, but nothing may order it against another version, which is why
	// this pattern is deliberately not wired into VersionPattern. Dependency
	// ranges, matchers, and upgrade selection all require ordering and keep
	// rejecting these.
	nonOrderableVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9-]+)?(\.dirty)?$`)
)

// IsApolloOrderableVersion reports whether a version can be compared to another.
func IsApolloOrderableVersion(value string) bool {
	return VersionPattern.MatchString(value)
}

// IsApolloProductVersion reports whether a version is one a Product Release may
// declare, orderable or not. It is the conformance test for a manifest's
// product-version; anything that needs to sort versions must ask
// IsApolloOrderableVersion instead.
func IsApolloProductVersion(value string) bool {
	return VersionPattern.MatchString(value) || nonOrderableVersionPattern.MatchString(value)
}

func (releaseVersionPattern) MatchString(value string) bool {
	_, ok := ParseReleaseVersion(value)
	return ok
}

func (releaseVersionMatcherPattern) MatchString(value string) bool {
	parts := matcherPartsPattern.FindStringSubmatch(value)
	if parts == nil {
		return false
	}
	for _, index := range []int{2, 3, 4, 5, 6, 7} {
		if parts[index] != "" {
			if _, ok := parseReleaseVersionComponent(parts[index]); !ok {
				return false
			}
		}
	}
	return true
}

func ParseReleaseVersion(value string) (releaseVersion, bool) {
	parts := versionPartsPattern.FindStringSubmatch(value)
	if parts == nil {
		return releaseVersion{}, false
	}
	parsed := releaseVersion{kind: versionRelease}
	for index := range parsed.core {
		component, ok := parseReleaseVersionComponent(parts[index+1])
		if !ok {
			return releaseVersion{}, false
		}
		parsed.core[index] = component
	}
	switch {
	case parts[4] != "" && parts[5] != "":
		parsed.kind = versionReleaseCandidateSnapshot
		var ok bool
		if parsed.rc, ok = parseReleaseVersionComponent(parts[4]); !ok {
			return releaseVersion{}, false
		}
		if parsed.snapshot, ok = parseReleaseVersionComponent(parts[5]); !ok {
			return releaseVersion{}, false
		}
	case parts[4] != "":
		parsed.kind = versionReleaseCandidate
		var ok bool
		if parsed.rc, ok = parseReleaseVersionComponent(parts[4]); !ok {
			return releaseVersion{}, false
		}
	case parts[7] != "":
		parsed.kind = versionReleaseSnapshot
		var ok bool
		if parsed.snapshot, ok = parseReleaseVersionComponent(parts[7]); !ok {
			return releaseVersion{}, false
		}
	}
	return parsed, true
}

func parseReleaseVersionComponent(value string) (uint32, bool) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed > math.MaxInt32 {
		return 0, false
	}
	return uint32(parsed), true
}

// CompareVersions implements Apollo Product Version ordering. Snapshot commit
// hashes do not participate in ordering, so equal counters compare equal even
// when their hashes differ.
func CompareVersions(left, right string) int {
	leftVersion, leftOK := ParseReleaseVersion(left)
	rightVersion, rightOK := ParseReleaseVersion(right)
	if !leftOK || !rightOK {
		return strings.Compare(left, right)
	}
	for index := range leftVersion.core {
		if comparison := compareUint32(leftVersion.core[index], rightVersion.core[index]); comparison != 0 {
			return comparison
		}
	}
	leftRC := leftVersion.kind == versionReleaseCandidate || leftVersion.kind == versionReleaseCandidateSnapshot
	rightRC := rightVersion.kind == versionReleaseCandidate || rightVersion.kind == versionReleaseCandidateSnapshot
	if leftRC && rightRC {
		if comparison := compareUint32(leftVersion.rc, rightVersion.rc); comparison != 0 {
			return comparison
		}
		if comparison := compareUint32(uint32(leftVersion.kind), uint32(rightVersion.kind)); comparison != 0 {
			return comparison
		}
		return compareUint32(leftVersion.snapshot, rightVersion.snapshot)
	}
	if leftRC {
		return -1
	}
	if rightRC {
		return 1
	}
	if comparison := compareUint32(uint32(leftVersion.kind), uint32(rightVersion.kind)); comparison != 0 {
		return comparison
	}
	return compareUint32(leftVersion.snapshot, rightVersion.snapshot)
}

func compareUint32(left, right uint32) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func VersionWithinMaximum(version, matcher string) bool {
	parsed, ok := ParseReleaseVersion(version)
	if !ok || !VersionMatcherPattern.MatchString(matcher) {
		return false
	}
	if VersionPattern.MatchString(matcher) {
		return CompareVersions(version, matcher) <= 0
	}
	parts := strings.Split(matcher, ".")
	if parts[0] == "x" {
		return true
	}
	major, _ := parseReleaseVersionComponent(parts[0])
	if parsed.core[0] != major {
		return parsed.core[0] < major
	}
	if parts[1] == "x" {
		return true
	}
	minor, _ := parseReleaseVersionComponent(parts[1])
	if parsed.core[1] != minor {
		return parsed.core[1] < minor
	}
	return true
}

func DefaultChannelsForVersion(version string) []string {
	parsed, ok := ParseReleaseVersion(version)
	if !ok {
		return nil
	}
	switch parsed.kind {
	case versionRelease:
		return []string{"DEV", "RELEASE_CANDIDATE", "RELEASE"}
	case versionReleaseCandidate:
		return []string{"DEV", "RELEASE_CANDIDATE"}
	default:
		return []string{"DEV"}
	}
}
