package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Multipass attribute keys, verbatim from the recovered principals constants.
//
// THE WIRE SHAPE IS ATTRIBUTE-BASED, and mis-modelling it is the way this
// federation fails without producing a symptom. The recovered
// `principals/User` carries `id`, `username` and an `attributes` map and
// nothing else; given name, family name, email, realm, organization and active
// are `@JsonIgnore` accessors computed over that map on the Java side. A struct
// that declared them as fields would decode a real response into a record of
// empty strings, which is indistinguishable from a directory of users who
// genuinely have no names — the failure would look like sparse data rather than
// a broken client, and nobody would go looking for it.
//
// So they are methods here, not fields, and the derivation is in the domain
// rather than in either adapter. Gitless GitOps keeps its admission decision
// out of its two Convergers for the same reason: a rule that lives in an
// adapter can be lenient in one adapter and strict in the other, and the
// difference then reads as a property of a deployment choice.
const (
	MultipassAttributeGivenName       = "multipass:given-name"
	MultipassAttributeFamilyName      = "multipass:family-name"
	MultipassAttributeEmailPrimary    = "multipass:email:primary"
	MultipassAttributeActive          = "multipass:active"
	MultipassAttributeRealm           = "multipass:realm"
	MultipassAttributeRealmName       = "multipass:realm-name"
	MultipassAttributeOrganization    = "multipass:organization"
	MultipassAttributeOrganizationRID = "multipass:organization-rid"
	MultipassAttributeDescription     = "multipass:description"
)

// MultipassAttributes is the attribute map every Multipass principal carries.
//
// A key maps to a LIST of values, not to one value, and the derived accessors
// read the first element. That is the recovered shape and it is preserved
// exactly, because the alternative — flattening to map[string]string at the
// decode boundary — would silently discard the second and later values of an
// attribute a later read does care about, and the loss would happen before any
// test could see it.
type MultipassAttributes map[string][]string

// MultipassUser is a Multipass user exactly as the wire carries it.
//
// Three fields, no more. See the attribute-key block above for why everything
// people think of as a user's properties lives behind a method instead.
type MultipassUser struct {
	ID         string              `json:"id"`
	Username   string              `json:"username"`
	Attributes MultipassAttributes `json:"attributes"`
}

// MultipassGroup is a Multipass group exactly as the wire carries it. Its
// description, realm and organization set are attribute-derived, as a user's
// are.
type MultipassGroup struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Attributes MultipassAttributes `json:"attributes"`
}

// MultipassGroupPage asks for one page of a user's groups.
//
// The cursor is an integer offset because the current per-user group read takes
// one; the earlier generations of that route took an opaque string and are not
// modelled here. A zero Size means "let the server apply its own default",
// which the recovered contract states is 50 — restating 50 on this side would
// pin a number Conduit did not choose and would not notice changing.
type MultipassGroupPage struct {
	From          int
	Size          int
	IncludeHidden bool
}

// MultipassGroupsPage is one page of groups plus what the server said about the
// page.
//
// OrderedBy echoes the ordering the server actually applied rather than the one
// that was asked for, so a caller that depends on ordering can tell whether it
// got it. Next is the continuation cursor exactly as carried; how it relates to
// MultipassGroupPage.From on the v3 route is not established by any source this
// repository holds, so a caller must pass it back rather than parse it.
type MultipassGroupsPage struct {
	Groups    []MultipassGroup
	Next      string
	OrderedBy string
}

// ErrMultipassPrincipalNotFound is what a directory read reports when Multipass
// says the user or group does not exist.
//
// It is distinct from a transport failure on purpose. "This directory has no
// such user" and "this directory could not be reached" call for opposite
// responses — the first is a stable answer a caller may cache and act on, the
// second is a retry — and an adapter that folded them together would turn an
// outage into a federation that quietly reports everybody as absent.
var ErrMultipassPrincipalNotFound = errors.New("multipass principal not found")

// ErrFederatedUsernameReserved is what federating a user whose minted username
// is already meaningful to this deployment produces. See FederatedClaims.
var ErrFederatedUsernameReserved = errors.New("federated username is reserved by this deployment")

// ErrFederatedUserInactive is what federating a user Multipass reports as
// deactivated produces.
var ErrFederatedUserInactive = errors.New("federated user is not active in the identity source")

// localDeveloperUsername is the identity the control plane's development header
// path invents when no subject is presented, and it is an administrator in a
// deployment that has named no administrators. It is reserved here
// unconditionally so that turning federation on cannot make a Multipass account
// called "local-developer" into one. The name is restated rather than imported
// because this package may not import another internal package.
const localDeveloperUsername = "local-developer"

// AttributeValue returns the first value recorded under key.
//
// Absent, and present-but-empty, are both reported as not present. The empty
// case is the one worth stating: `{"multipass:given-name": []}` is a wire shape
// the server can send, the reference client's `attributes[key]?.[0]` yields
// undefined for it, and the obvious Go transcription — `values[0]` — panics.
//
// A present key whose first value is the empty string is reported as PRESENT
// with an empty value, because that is what the reference client does and
// because "recorded as blank" and "not recorded" are different facts about a
// directory entry.
func (attributes MultipassAttributes) AttributeValue(key string) (string, bool) {
	values, present := attributes[key]
	if !present || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// GivenName is the user's given name, or empty when the attribute is absent.
func (user MultipassUser) GivenName() string {
	value, _ := user.Attributes.AttributeValue(MultipassAttributeGivenName)
	return value
}

// FamilyName is the user's family name, or empty when the attribute is absent.
func (user MultipassUser) FamilyName() string {
	value, _ := user.Attributes.AttributeValue(MultipassAttributeFamilyName)
	return value
}

// EmailPrimary is the user's primary email address, or empty when the attribute
// is absent. Multipass models email as an attribute family and this reads only
// the primary member of it.
func (user MultipassUser) EmailPrimary() string {
	value, _ := user.Attributes.AttributeValue(MultipassAttributeEmailPrimary)
	return value
}

// OrganizationRID is the user's organization RID, or empty when absent. Only
// the first is returned; a principal may carry several, and a caller that needs
// all of them should read Attributes directly rather than have this method
// pretend the set is one value.
func (user MultipassUser) OrganizationRID() string {
	value, _ := user.Attributes.AttributeValue(MultipassAttributeOrganizationRID)
	return value
}

// Realm is the realm the user was minted in, or empty when absent.
func (user MultipassUser) Realm() string {
	value, _ := user.Attributes.AttributeValue(MultipassAttributeRealm)
	return value
}

// Description is the group's description, or empty when absent.
func (group MultipassGroup) Description() string {
	value, _ := group.Attributes.AttributeValue(MultipassAttributeDescription)
	return value
}

// Realm is the realm the group was minted in, or empty when absent.
func (group MultipassGroup) Realm() string {
	value, _ := group.Attributes.AttributeValue(MultipassAttributeRealm)
	return value
}

// Active reports whether Multipass considers this account live.
//
// An ABSENT multipass:active attribute means active. That is the recovered
// accessor's own default and the reference client restates it, so it is
// reproduced rather than tightened — but it is a fail-open default and it is
// worth naming as one. The mitigation is not to second-guess it here; it is
// that this value never reaches an authorization decision. Governance decides,
// and it decides from grants, not from an attribute an identity source may
// simply have omitted.
//
// The comparison is exact. The reference client tests `value === "true"`, so
// "TRUE", "1" and "yes" are all reported inactive here. Whether the server's
// own accessor parses the string case-insensitively is not established by any
// source this repository holds; if it does, this is stricter than the server,
// which is the safe direction for a value whose only Conduit use is to refuse
// federation.
func (user MultipassUser) Active() bool {
	value, present := user.Attributes.AttributeValue(MultipassAttributeActive)
	if !present {
		return true
	}
	return value == "true"
}

// FullName is the given name, then the family name, separated by one space.
//
// An absent GIVEN name yields an empty full name even when a family name is
// present. That is the reference client's behaviour and it is deliberate here:
// rendering a bare surname as somebody's full name is worse than rendering
// nothing, because nothing is visibly missing data and a bare surname is not.
func (user MultipassUser) FullName() string {
	given, present := user.Attributes.AttributeValue(MultipassAttributeGivenName)
	if !present {
		return ""
	}
	family, familyPresent := user.Attributes.AttributeValue(MultipassAttributeFamilyName)
	if !familyPresent {
		return given
	}
	return given + " " + family
}

// FederationPolicy is what a deployment must state before a Multipass identity
// is allowed to become claims Governance will decide about.
//
// It is required rather than optional, and its fields have no defaults, because
// the mapping it governs is where a federation can hand out authority nobody
// granted. Every field below exists because of a specific way that happens.
type FederationPolicy struct {
	// UsernamePrefix is prepended to the Multipass username to produce the
	// username Governance sees. It exists because Governance compares usernames
	// with exact string equality everywhere — grants, separation of duties, and
	// the bootstrap administrator list — so an unprefixed federation puts the
	// naming of Conduit's own administrators inside somebody else's directory.
	//
	// Empty is permitted, because a deployment whose operators ALREADY are
	// Multipass users wants exactly the flat namespace, and refusing it would
	// push that deployment into a prefix it then has to strip. It is permitted,
	// not defaulted: ReservedUsernames below is what makes it survivable.
	UsernamePrefix string

	// GroupPrefix is prepended to each group name for the same reason, on the
	// other half of the same comparison: a Team's MemberGroups are matched
	// against a principal's groups by exact name, so a Multipass group called
	// "platform-admins" joins any Team that named that group, whoever created it
	// and in whichever directory.
	GroupPrefix string

	// ReservedUsernames names the identities this deployment already means
	// something by. Its BootstrapAdministrators list belongs here first: that
	// list is matched against Principal.Username with plain string equality and
	// grants administrator authority before any grant exists, so a Multipass
	// user whose minted username lands on one of those names becomes an
	// administrator of Conduit by having been created in Multipass.
	//
	// A collision is refused, not silently qualified. Qualifying it would change
	// an identity that grants, change requests and audit records may already
	// refer to, and would do so at the moment of a collision — the worst
	// available moment to rename somebody.
	ReservedUsernames []string
}

// FederatedClaims turns one Multipass user, and the groups that user belongs
// to, into the Claims the control plane's ResolvePrincipal already consumes.
//
// This is the whole of the mapping onto the Principal Governance decides about,
// and it is deliberately a pure function with no port behind it. Nothing calls
// it: federation is inert until a composition root deliberately assembles a
// directory, a policy and a caller for it. What it does NOT do is as load
// bearing as what it does — it grants nothing, it consults no grant, it reads
// no Team, and it returns claims, which are an input to authorization and never
// an outcome of one.
//
// ServiceAccount is left false. Multipass's recovered user shape says nothing
// about whether a principal is an automation, and inferring it from a username
// pattern would let a directory decide which identities may act on behalf of
// others, which is a delegation power Conduit hands out in its own
// configuration.
func FederatedClaims(user MultipassUser, groups []MultipassGroup, policy FederationPolicy) (Claims, error) {
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return Claims{}, fmt.Errorf("%w: Multipass user %q carries no username", ErrUnauthorized, user.ID)
	}
	if !user.Active() {
		return Claims{}, fmt.Errorf("%w: %s", ErrFederatedUserInactive, username)
	}
	minted := strings.TrimSpace(policy.UsernamePrefix) + username
	// The development fallback identity is appended by value rather than with
	// append(policy.ReservedUsernames, ...), which would write into the caller's
	// backing array whenever it had spare capacity.
	reserved := make([]string, 0, len(policy.ReservedUsernames)+1)
	reserved = append(reserved, policy.ReservedUsernames...)
	reserved = append(reserved, localDeveloperUsername)
	for _, name := range reserved {
		if strings.TrimSpace(name) == minted {
			return Claims{}, fmt.Errorf("%w: %s", ErrFederatedUsernameReserved, minted)
		}
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		if name := strings.TrimSpace(group.Name); name != "" {
			names = append(names, strings.TrimSpace(policy.GroupPrefix)+name)
		}
	}
	return Claims{Username: minted, Groups: normalizedStrings(names)}, nil
}
