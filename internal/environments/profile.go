// Package environments owns how an Environment describes itself: the profile an
// operator authors — who owns it, what accreditation it holds, which namespaces
// are registered — and the desired configuration that governs what may be
// installed into it.
//
// It owns the rules for those, not the workflow around them. Whether a profile
// change needs an approval, who may make it, and how it reaches the Environment
// are Governance and Delivery questions; whether the profile is well formed, and
// what it looks like to a policy engine, are answered here.
//
// This package imports nothing.
package environments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SourceOperatorAPI is the only provenance a profile may claim. A profile that
// arrived any other way is not one this package will accept.
const SourceOperatorAPI = "OPERATOR_API"

// ErrInvalid means a profile is not well formed. The caller decides what that
// means over the wire; here it means only that the rules below were not met.
var ErrInvalid = errors.New("invalid Environment profile")

var (
	identifierPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
	accreditationPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
)

// Profile is what an operator authors about an Environment.
type Profile struct {
	EnvironmentID                    string    `json:"environmentId"`
	DisplayName                      string    `json:"displayName,omitempty"`
	Description                      string    `json:"description,omitempty"`
	README                           string    `json:"readme,omitempty"`
	Accreditation                    string    `json:"accreditation"`
	OwnerTeam                        string    `json:"ownerTeam,omitempty"`
	CoOwnerTeams                     []string  `json:"coOwnerTeams,omitempty"`
	OperationalResponsibilityEnabled bool      `json:"operationalResponsibilityEnabled"`
	RegisteredNamespaces             []string  `json:"registeredNamespaces,omitempty"`
	SupportOverride                  string    `json:"supportOverride,omitempty"`
	Source                           string    `json:"source"`
	UpdatedBy                        string    `json:"updatedBy"`
	Rationale                        string    `json:"rationale"`
	CreatedAt                        time.Time `json:"createdAt"`
	UpdatedAt                        time.Time `json:"updatedAt"`
	Revision                         uint64    `json:"revision"`
}

// ProfilesEqual compares two profiles by their serialized form, so a field
// added later is compared without this function having to learn about it.
func ProfilesEqual(left, right Profile) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

// ProposalIdentity names a proposed change by its content, with the timestamps
// zeroed. Two operators proposing the same change therefore produce the same
// identity, which is what makes a resubmission recognisable as the same
// proposal rather than a second one.
func ProposalIdentity(profile Profile) string {
	profile.CreatedAt, profile.UpdatedAt = time.Time{}, time.Time{}
	encoded, _ := json.Marshal(profile)
	digest := sha256.Sum256([]byte(string(encoded)))
	return hex.EncodeToString(digest[:16])
}

// ValidateProfile reports whether a profile is well formed.
func ValidateProfile(profile Profile) error {
	if !identifierPattern.MatchString(profile.EnvironmentID) || !accreditationPattern.MatchString(profile.Accreditation) ||
		len(profile.DisplayName) > 256 || len(profile.Description) > 4096 || len(profile.README) > 64*1024 ||
		len(profile.SupportOverride) > 128 || profile.Source != SourceOperatorAPI ||
		strings.TrimSpace(profile.UpdatedBy) == "" || strings.TrimSpace(profile.Rationale) == "" ||
		profile.CreatedAt.IsZero() || profile.UpdatedAt.IsZero() || profile.UpdatedAt.Before(profile.CreatedAt) || profile.Revision == 0 {
		return fmt.Errorf("%w: invalid Environment profile", ErrInvalid)
	}
	if profile.OwnerTeam != "" && !identifierPattern.MatchString(profile.OwnerTeam) {
		return fmt.Errorf("%w: invalid Environment owner team", ErrInvalid)
	}
	for _, team := range profile.CoOwnerTeams {
		if !identifierPattern.MatchString(team) || team == profile.OwnerTeam {
			return fmt.Errorf("%w: invalid Environment co-owner team", ErrInvalid)
		}
	}
	for _, namespace := range profile.RegisteredNamespaces {
		if strings.TrimSpace(namespace) == "" || len(namespace) > 256 || strings.ContainsAny(namespace, "\r\n\x00") {
			return fmt.Errorf("%w: invalid registered namespace", ErrInvalid)
		}
	}
	if profile.SupportOverride != "" && !accreditationPattern.MatchString(profile.SupportOverride) {
		return fmt.Errorf("%w: invalid support override", ErrInvalid)
	}
	return nil
}

// PolicyResource is the profile as a policy engine sees it. The slices are
// copied rather than shared: a policy engine that mutated one would otherwise
// be editing the profile it was asked to judge.
func PolicyResource(profile Profile) map[string]any {
	return map[string]any{
		"displayName": profile.DisplayName, "description": profile.Description,
		"accreditation": profile.Accreditation, "ownerTeam": profile.OwnerTeam,
		"coOwnerTeams":                     append([]string(nil), profile.CoOwnerTeams...),
		"operationalResponsibilityEnabled": profile.OperationalResponsibilityEnabled,
		"registeredNamespaces":             append([]string(nil), profile.RegisteredNamespaces...),
		"supportOverride":                  profile.SupportOverride, "profileRevision": profile.Revision,
	}
}

// SortProfiles orders profiles oldest revision first.
func SortProfiles(profiles []Profile) {
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Revision < profiles[j].Revision })
}

// ContactTeams is every team a profile names, normalized and deduplicated —
// the set a caller must check exists before accepting the profile.
func ContactTeams(profile Profile) []string {
	teams := append([]string(nil), profile.CoOwnerTeams...)
	if profile.OwnerTeam != "" {
		teams = append(teams, profile.OwnerTeam)
	}
	seen := make(map[string]struct{}, len(teams))
	normalized := make([]string, 0, len(teams))
	for _, team := range teams {
		trimmed := strings.TrimSpace(team)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

// ValidContactTeam reports whether a team name is well formed. A caller still
// has to check the team exists; this only rejects names that could not name one.
func ValidContactTeam(team string) bool { return identifierPattern.MatchString(team) }
