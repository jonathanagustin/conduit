package catalog

import "testing"

func TestApolloOrderableVersionFormats(t *testing.T) {
	t.Parallel()
	valid := []string{
		"0.0.0", "1.2.3", "1.2.3-4-gaaaaaaa", "1.2.3-rc7", "1.2.3-rc7-9-g0abc123",
		"2147483647.2147483647.2147483647", "1.0.0-2147483647-ga", "1.0.0-rc2147483647",
	}
	invalid := []string{
		"1.0", "1.0.0-rc.1", "1.0.0-snapshot.1", "1.0.0-alpha", "1.0.0+build",
		"1.0.0-1-gABC", "1.0.0-rc1-1-g", "2147483648.0.0", "1.0.0-2147483648-ga",
	}
	for _, version := range valid {
		if !VersionPattern.MatchString(version) {
			t.Errorf("valid Apollo version rejected: %s", version)
		}
	}
	for _, version := range invalid {
		if VersionPattern.MatchString(version) {
			t.Errorf("invalid/non-orderable Apollo version accepted: %s", version)
		}
	}
}

func TestApolloVersionOrdering(t *testing.T) {
	t.Parallel()
	ordered := []string{
		"1.0.0-rc1",
		"1.0.0-rc2",
		"1.0.0-rc2-4-gaaaaaaa",
		"1.0.0-rc2-5-gccccccc",
		"1.0.0",
		"1.0.0-3-gaaaaaaa",
		"1.0.0-4-gbbbbbbb",
		"1.1.0-rc1",
		"1.1.0",
	}
	for index := 1; index < len(ordered); index++ {
		if CompareVersions(ordered[index-1], ordered[index]) >= 0 {
			t.Errorf("expected %s < %s", ordered[index-1], ordered[index])
		}
	}
	if CompareVersions("2.0.0-rc1-3-gaaaaaaa", "2.0.0-rc1-3-gbbbbbbb") != 0 {
		t.Fatal("RC snapshot commit hashes must not affect ordering")
	}
	if CompareVersions("2.0.0-5-gbbbbbbb", "2.0.0-5-gaaaaaaa1") != 0 {
		t.Fatal("release snapshot commit hashes must not affect ordering")
	}
}

func TestApolloVersionMatchersAsUpperBounds(t *testing.T) {
	t.Parallel()
	validMatchers := []string{"x.x.x", "1.x.x", "1.2.x", "1.2.3"}
	invalidMatchers := []string{"x.1.1", "1.x.1", "1.2", "1.2.*", "2147483648.x.x"}
	for _, matcher := range validMatchers {
		if !VersionMatcherPattern.MatchString(matcher) {
			t.Errorf("valid matcher rejected: %s", matcher)
		}
	}
	for _, matcher := range invalidMatchers {
		if VersionMatcherPattern.MatchString(matcher) {
			t.Errorf("invalid matcher accepted: %s", matcher)
		}
	}
	cases := []struct {
		version string
		matcher string
		want    bool
	}{
		{"9.3.6", "9.6.x", true},
		{"9.6.1-22-g1a2b3c4", "9.6.x", true},
		{"9.7.0-1-gabcdef", "9.6.x", false},
		{"1.2.3-rc4", "1.2.3", true},
		{"1.2.3", "1.2.3", true},
		{"1.2.3-4-gabcdef", "1.2.3", false},
		{"2.0.0", "1.x.x", false},
		{"999.0.0", "x.x.x", true},
	}
	for _, testCase := range cases {
		if got := VersionWithinMaximum(testCase.version, testCase.matcher); got != testCase.want {
			t.Errorf("VersionWithinMaximum(%q, %q) = %v, want %v", testCase.version, testCase.matcher, got, testCase.want)
		}
	}
}

func TestDefaultChannelsMatchApolloVersionTypes(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"1.0.0":                {"DEV", "RELEASE_CANDIDATE", "RELEASE"},
		"1.0.0-rc1":            {"DEV", "RELEASE_CANDIDATE"},
		"1.0.0-1-gaaaaaaa":     {"DEV"},
		"1.0.0-rc1-1-gaaaaaaa": {"DEV"},
	}
	for version, want := range cases {
		got := DefaultChannelsForVersion(version)
		if !stringSlicesEqual(got, want) {
			t.Errorf("channels for %s = %#v, want %#v", version, got, want)
		}
	}
}

// Restated here rather than shared: a test helper crossing a package boundary
// is a dependency the boundary exists to prevent, and it is eight lines.
func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
