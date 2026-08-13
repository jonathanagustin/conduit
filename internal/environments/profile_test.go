package environments

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validProfile() Profile {
	created := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	return Profile{
		EnvironmentID: "dev-use2",
		Accreditation: "IL4",
		Source:        SourceOperatorAPI,
		UpdatedBy:     "operator",
		Rationale:     "because",
		CreatedAt:     created,
		UpdatedAt:     created,
		Revision:      1,
	}
}

func TestValidProfileIsAccepted(t *testing.T) {
	t.Parallel()
	if err := ValidateProfile(validProfile()); err != nil {
		t.Fatalf("a well-formed profile must validate: %v", err)
	}
}

func TestProfileRejections(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		break_ func(*Profile)
	}{
		{"no environment id", func(p *Profile) { p.EnvironmentID = "" }},
		{"environment id with a space", func(p *Profile) { p.EnvironmentID = "dev use2" }},
		{"lowercase accreditation", func(p *Profile) { p.Accreditation = "il4" }},
		{"no accreditation", func(p *Profile) { p.Accreditation = "" }},
		{"source is not the operator API", func(p *Profile) { p.Source = "IMPORTED" }},
		{"no author", func(p *Profile) { p.UpdatedBy = "  " }},
		{"no rationale", func(p *Profile) { p.Rationale = "" }},
		{"revision zero", func(p *Profile) { p.Revision = 0 }},
		{"never created", func(p *Profile) { p.CreatedAt = time.Time{} }},
		{"updated before created", func(p *Profile) { p.UpdatedAt = p.CreatedAt.Add(-time.Hour) }},
		{"display name too long", func(p *Profile) { p.DisplayName = strings.Repeat("x", 257) }},
		{"description too long", func(p *Profile) { p.Description = strings.Repeat("x", 4097) }},
		{"readme too long", func(p *Profile) { p.README = strings.Repeat("x", 64*1024+1) }},
		{"malformed owner team", func(p *Profile) { p.OwnerTeam = "owner team" }},
		{"co-owner repeats the owner", func(p *Profile) { p.OwnerTeam = "sre"; p.CoOwnerTeams = []string{"sre"} }},
		{"malformed co-owner", func(p *Profile) { p.CoOwnerTeams = []string{"co owner"} }},
		{"blank namespace", func(p *Profile) { p.RegisteredNamespaces = []string{"   "} }},
		{"namespace with a newline", func(p *Profile) { p.RegisteredNamespaces = []string{"ns\nother"} }},
		{"namespace too long", func(p *Profile) { p.RegisteredNamespaces = []string{strings.Repeat("n", 257)} }},
		{"malformed support override", func(p *Profile) { p.SupportOverride = "il4" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			profile := validProfile()
			testCase.break_(&profile)
			err := ValidateProfile(profile)
			if err == nil {
				t.Fatal("must be rejected")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("must be rejected as ErrInvalid, got %v", err)
			}
		})
	}
}

// The identity names a change by its content, so the same change proposed twice
// is recognisable as one proposal rather than two.
func TestProposalIdentityIgnoresTimestamps(t *testing.T) {
	t.Parallel()
	first := validProfile()
	second := validProfile()
	second.CreatedAt = second.CreatedAt.Add(72 * time.Hour)
	second.UpdatedAt = second.UpdatedAt.Add(72 * time.Hour)
	if ProposalIdentity(first) != ProposalIdentity(second) {
		t.Fatal("the same change proposed at a different time must have the same identity")
	}
	third := validProfile()
	third.DisplayName = "different"
	if ProposalIdentity(first) == ProposalIdentity(third) {
		t.Fatal("a different change must have a different identity")
	}
}

func TestProfilesEqualComparesEveryField(t *testing.T) {
	t.Parallel()
	if !ProfilesEqual(validProfile(), validProfile()) {
		t.Fatal("identical profiles must compare equal")
	}
	other := validProfile()
	other.OperationalResponsibilityEnabled = true
	if ProfilesEqual(validProfile(), other) {
		t.Fatal("a differing field must make profiles unequal")
	}
}

// A policy engine handed the profile's own slices could edit what it was asked
// to judge. It gets copies.
func TestPolicyResourceDoesNotShareItsSlices(t *testing.T) {
	t.Parallel()
	profile := validProfile()
	profile.CoOwnerTeams = []string{"sre"}
	profile.RegisteredNamespaces = []string{"ns"}
	resource := PolicyResource(profile)
	resource["coOwnerTeams"].([]string)[0] = "tampered"
	resource["registeredNamespaces"].([]string)[0] = "tampered"
	if profile.CoOwnerTeams[0] != "sre" || profile.RegisteredNamespaces[0] != "ns" {
		t.Fatal("PolicyResource shared its slices with the profile")
	}
}

func TestSortProfilesOrdersByRevision(t *testing.T) {
	t.Parallel()
	profiles := []Profile{{Revision: 3}, {Revision: 1}, {Revision: 2}}
	SortProfiles(profiles)
	for index, want := range []uint64{1, 2, 3} {
		if profiles[index].Revision != want {
			t.Fatalf("sorted = %v, want ascending revisions", profiles)
		}
	}
}

// The teams a caller must check exist: owner and co-owners, trimmed,
// deduplicated, and ordered so the check is the same every time.
func TestContactTeamsAreNormalized(t *testing.T) {
	t.Parallel()
	profile := validProfile()
	profile.OwnerTeam = "sre"
	profile.CoOwnerTeams = []string{" platform ", "", "sre", "platform"}
	got := ContactTeams(profile)
	if len(got) != 2 || got[0] != "platform" || got[1] != "sre" {
		t.Fatalf("want [platform sre], got %v", got)
	}
}

// Separate from the dedup case above, where the owner is also a co-owner and
// so would still appear if ContactTeams forgot to add it.
func TestContactTeamsIncludeTheOwner(t *testing.T) {
	t.Parallel()
	profile := validProfile()
	profile.OwnerTeam = "sre"
	profile.CoOwnerTeams = []string{"platform"}
	got := ContactTeams(profile)
	if len(got) != 2 || got[0] != "platform" || got[1] != "sre" {
		t.Fatalf("the owner must be among the contact teams: got %v", got)
	}
}

func TestContactTeamsOmitsAnAbsentOwner(t *testing.T) {
	t.Parallel()
	profile := validProfile()
	profile.OwnerTeam = ""
	profile.CoOwnerTeams = []string{"platform"}
	if got := ContactTeams(profile); len(got) != 1 || got[0] != "platform" {
		t.Fatalf("want [platform], got %v", got)
	}
}

func TestValidContactTeamRejectsWhatCouldNotNameATeam(t *testing.T) {
	t.Parallel()
	if !ValidContactTeam("sre-platform.2") {
		t.Error("a well-formed team name must be accepted")
	}
	for _, bad := range []string{"", "with space", "new\nline"} {
		if ValidContactTeam(bad) {
			t.Errorf("%q must not be accepted as a team name", bad)
		}
	}
}
