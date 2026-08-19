package domain

import (
	"errors"
	"strings"
	"testing"
)

// The derivation table. Every row is a shape the recovered wire format can
// actually produce, and the three that matter most are the ones a
// reimplementation gets wrong: a missing attributes map entirely, a key present
// with an EMPTY list, and multipass:active absent.
func TestMultipassUserAttributeDerivation(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name         string
		user         MultipassUser
		givenName    string
		familyName   string
		email        string
		realm        string
		organization string
		fullName     string
		active       bool
	}{
		{
			name: "every attribute present",
			user: MultipassUser{ID: "u1", Username: "operator", Attributes: MultipassAttributes{
				MultipassAttributeGivenName:       {"Ops"},
				MultipassAttributeFamilyName:      {"Operator"},
				MultipassAttributeEmailPrimary:    {"operator@example.test"},
				MultipassAttributeRealm:           {"realm-a"},
				MultipassAttributeOrganizationRID: {"ri.organization.a"},
				MultipassAttributeActive:          {"true"},
			}},
			givenName: "Ops", familyName: "Operator", email: "operator@example.test",
			realm: "realm-a", organization: "ri.organization.a", fullName: "Ops Operator", active: true,
		},
		{
			// A nil attributes map is what a response omitting the field decodes
			// to, and nothing here may panic on it.
			name:   "the attributes map is absent entirely",
			user:   MultipassUser{ID: "u2", Username: "sparse"},
			active: true,
		},
		{
			name:   "the attributes map is present but empty",
			user:   MultipassUser{ID: "u3", Username: "sparse", Attributes: MultipassAttributes{}},
			active: true,
		},
		{
			// The case the reference client handles with `?.[0]` and the obvious
			// Go transcription handles with a panic.
			name: "every attribute is present with an empty list",
			user: MultipassUser{ID: "u4", Username: "empty", Attributes: MultipassAttributes{
				MultipassAttributeGivenName:       {},
				MultipassAttributeFamilyName:      {},
				MultipassAttributeEmailPrimary:    {},
				MultipassAttributeRealm:           {},
				MultipassAttributeOrganizationRID: {},
				MultipassAttributeActive:          {},
			}},
			active: true,
		},
		{
			name: "a family name with no given name yields no full name",
			user: MultipassUser{ID: "u5", Username: "surname", Attributes: MultipassAttributes{
				MultipassAttributeFamilyName: {"Operator"},
			}},
			familyName: "Operator", active: true,
		},
		{
			name: "a given name with no family name is the whole full name",
			user: MultipassUser{ID: "u6", Username: "mononym", Attributes: MultipassAttributes{
				MultipassAttributeGivenName: {"Ops"},
			}},
			givenName: "Ops", fullName: "Ops", active: true,
		},
		{
			// A key recorded blank is a different fact from a key not recorded,
			// and the derivation preserves the difference.
			name: "an attribute recorded as the empty string is present and blank",
			user: MultipassUser{ID: "u7", Username: "blank", Attributes: MultipassAttributes{
				MultipassAttributeGivenName:  {""},
				MultipassAttributeFamilyName: {"Operator"},
			}},
			familyName: "Operator", fullName: " Operator", active: true,
		},
		{
			// Multi-valued attributes read the first element, and the ordering is
			// the server's rather than anything this side sorts.
			name: "a multi-valued attribute reads its first value",
			user: MultipassUser{ID: "u8", Username: "multi", Attributes: MultipassAttributes{
				MultipassAttributeOrganizationRID: {"ri.organization.a", "ri.organization.b"},
			}},
			organization: "ri.organization.a", active: true,
		},
		{
			name: "multipass:active recorded false",
			user: MultipassUser{ID: "u9", Username: "retired", Attributes: MultipassAttributes{
				MultipassAttributeActive: {"false"},
			}},
			active: false,
		},
		{
			// The comparison is exact, matching the reference client's
			// `value === "true"`. If the server parses case-insensitively this is
			// stricter than the server, which is the safe direction.
			name: "multipass:active recorded TRUE in upper case is not active",
			user: MultipassUser{ID: "u10", Username: "shouty", Attributes: MultipassAttributes{
				MultipassAttributeActive: {"TRUE"},
			}},
			active: false,
		},
		{
			name: "multipass:active recorded as an unrelated string is not active",
			user: MultipassUser{ID: "u11", Username: "odd", Attributes: MultipassAttributes{
				MultipassAttributeActive: {"yes"},
			}},
			active: false,
		},
		{
			// The fail-open default the recovered accessor states. It is
			// reproduced deliberately; see Active's comment.
			name: "multipass:active absent means active",
			user: MultipassUser{ID: "u12", Username: "implicit", Attributes: MultipassAttributes{
				MultipassAttributeGivenName: {"Ops"},
			}},
			givenName: "Ops", fullName: "Ops", active: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := testCase.user.GivenName(); got != testCase.givenName {
				t.Errorf("GivenName = %q, want %q", got, testCase.givenName)
			}
			if got := testCase.user.FamilyName(); got != testCase.familyName {
				t.Errorf("FamilyName = %q, want %q", got, testCase.familyName)
			}
			if got := testCase.user.EmailPrimary(); got != testCase.email {
				t.Errorf("EmailPrimary = %q, want %q", got, testCase.email)
			}
			if got := testCase.user.Realm(); got != testCase.realm {
				t.Errorf("Realm = %q, want %q", got, testCase.realm)
			}
			if got := testCase.user.OrganizationRID(); got != testCase.organization {
				t.Errorf("OrganizationRID = %q, want %q", got, testCase.organization)
			}
			if got := testCase.user.FullName(); got != testCase.fullName {
				t.Errorf("FullName = %q, want %q", got, testCase.fullName)
			}
			if got := testCase.user.Active(); got != testCase.active {
				t.Errorf("Active = %v, want %v", got, testCase.active)
			}
		})
	}
}

func TestMultipassGroupAttributeDerivation(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name        string
		group       MultipassGroup
		description string
		realm       string
	}{
		{
			name: "both attributes present",
			group: MultipassGroup{ID: "g1", Name: "Users", Attributes: MultipassAttributes{
				MultipassAttributeDescription: {"All platform users"},
				MultipassAttributeRealm:       {"realm-a"},
			}},
			description: "All platform users", realm: "realm-a",
		},
		{name: "no attributes map", group: MultipassGroup{ID: "g2", Name: "Users"}},
		{
			name: "attributes present with empty lists",
			group: MultipassGroup{ID: "g3", Name: "Users", Attributes: MultipassAttributes{
				MultipassAttributeDescription: {},
				MultipassAttributeRealm:       {},
			}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := testCase.group.Description(); got != testCase.description {
				t.Errorf("Description = %q, want %q", got, testCase.description)
			}
			if got := testCase.group.Realm(); got != testCase.realm {
				t.Errorf("Realm = %q, want %q", got, testCase.realm)
			}
		})
	}
}

func TestAttributeValueDistinguishesAbsentFromBlank(t *testing.T) {
	t.Parallel()
	attributes := MultipassAttributes{"present": {""}, "empty": {}}
	if value, present := attributes.AttributeValue("present"); value != "" || !present {
		t.Errorf("blank attribute: value=%q present=%v, want \"\" true", value, present)
	}
	if value, present := attributes.AttributeValue("empty"); value != "" || present {
		t.Errorf("empty list: value=%q present=%v, want \"\" false", value, present)
	}
	if value, present := attributes.AttributeValue("missing"); value != "" || present {
		t.Errorf("absent key: value=%q present=%v, want \"\" false", value, present)
	}
	var nilAttributes MultipassAttributes
	if value, present := nilAttributes.AttributeValue("anything"); value != "" || present {
		t.Errorf("nil map: value=%q present=%v, want \"\" false", value, present)
	}
}

// The mapping onto the Principal Governance decides about. The rows that matter
// are the refusals: each one is a way a directory entry could otherwise acquire
// authority this deployment never granted.
func TestFederatedClaims(t *testing.T) {
	t.Parallel()
	active := func(username string) MultipassUser {
		return MultipassUser{ID: "u-" + username, Username: username,
			Attributes: MultipassAttributes{MultipassAttributeActive: {"true"}}}
	}
	group := func(name string) MultipassGroup {
		return MultipassGroup{ID: "g-" + name, Name: name}
	}
	for _, testCase := range []struct {
		name         string
		user         MultipassUser
		groups       []MultipassGroup
		policy       FederationPolicy
		wantUsername string
		wantGroups   []string
		wantErr      error
	}{
		{
			name: "a prefixed user with prefixed groups",
			user: active("alice"), groups: []MultipassGroup{group("Users"), group("Administrators")},
			policy:       FederationPolicy{UsernamePrefix: "multipass:", GroupPrefix: "multipass:"},
			wantUsername: "multipass:alice",
			wantGroups:   []string{"multipass:Administrators", "multipass:Users"},
		},
		{
			name: "an unprefixed deployment is permitted",
			user: active("alice"), groups: []MultipassGroup{group("Users")},
			wantUsername: "alice", wantGroups: []string{"Users"},
		},
		{
			// The collision this whole policy exists for. Without the refusal,
			// creating a Multipass account called "root-admin" makes its holder
			// an administrator of Conduit, because the bootstrap list is matched
			// against Principal.Username with plain string equality.
			name: "a username colliding with a bootstrap administrator is refused",
			user: active("root-admin"),
			policy: FederationPolicy{
				ReservedUsernames: []string{"root-admin", "second-admin"},
			},
			wantErr: ErrFederatedUsernameReserved,
		},
		{
			// A prefix moves the collision rather than removing it: the check has
			// to run on the MINTED name, not the raw one.
			name: "the collision is judged on the minted name, not the raw one",
			user: active("admin"),
			policy: FederationPolicy{
				UsernamePrefix: "multipass:", ReservedUsernames: []string{"multipass:admin"},
			},
			wantErr: ErrFederatedUsernameReserved,
		},
		{
			// And a prefix that avoids the reserved name is fine, which is the
			// point of allowing one.
			name: "a prefix that clears the reserved name federates",
			user: active("admin"),
			policy: FederationPolicy{
				UsernamePrefix: "multipass:", ReservedUsernames: []string{"admin"},
			},
			wantUsername: "multipass:admin",
		},
		{
			// Reserved unconditionally: this deployment's development fallback
			// identity is an administrator when no administrator is named.
			name:    "the local-developer fallback identity is reserved without being listed",
			user:    active("local-developer"),
			wantErr: ErrFederatedUsernameReserved,
		},
		{
			name:    "a deactivated user is not federated",
			user:    MultipassUser{ID: "u", Username: "retired", Attributes: MultipassAttributes{MultipassAttributeActive: {"false"}}},
			wantErr: ErrFederatedUserInactive,
		},
		{
			// The fail-open contract default, stated as a test so that changing
			// it is a deliberate act rather than a silent one.
			name:         "a user with no multipass:active attribute is federated",
			user:         MultipassUser{ID: "u", Username: "implicit"},
			wantUsername: "implicit",
		},
		{
			name:    "a user with no username is not an identity",
			user:    MultipassUser{ID: "u", Attributes: MultipassAttributes{}},
			wantErr: ErrUnauthorized,
		},
		{
			name:    "a username of only whitespace is not an identity",
			user:    MultipassUser{ID: "u", Username: "   "},
			wantErr: ErrUnauthorized,
		},
		{
			// Groups arrive from a directory Conduit does not control, so blanks
			// and duplicates are dropped rather than passed to a Team match.
			name: "blank and duplicate group names are dropped and the rest ordered",
			user: active("alice"),
			groups: []MultipassGroup{
				group("Users"), {ID: "g-blank", Name: "   "}, group("Users"), group("Auditors"),
			},
			wantUsername: "alice", wantGroups: []string{"Auditors", "Users"},
		},
		{
			name: "a user in no groups federates with no groups",
			user: active("alice"), wantUsername: "alice",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			claims, err := FederatedClaims(testCase.user, testCase.groups, testCase.policy)
			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("err = %v, want %v", err, testCase.wantErr)
				}
				if claims.Username != "" {
					t.Fatalf("a refused federation returned claims: %#v", claims)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if claims.Username != testCase.wantUsername {
				t.Errorf("Username = %q, want %q", claims.Username, testCase.wantUsername)
			}
			if strings.Join(claims.Groups, ",") != strings.Join(testCase.wantGroups, ",") {
				t.Errorf("Groups = %v, want %v", claims.Groups, testCase.wantGroups)
			}
			if claims.ServiceAccount {
				t.Error("federation invented a service account; Multipass says nothing about that")
			}
			if claims.OnBehalfOf != "" {
				t.Errorf("federation invented a delegation: %q", claims.OnBehalfOf)
			}
		})
	}
}

// FederatedClaims must not write into the caller's reserved-name slice. It
// appends the development identity, and appending to a slice with spare
// capacity edits the array behind it.
func TestFederatedClaimsDoesNotMutateTheReservedList(t *testing.T) {
	t.Parallel()
	reserved := make([]string, 1, 4)
	reserved[0] = "root-admin"
	policy := FederationPolicy{ReservedUsernames: reserved}
	if _, err := FederatedClaims(MultipassUser{ID: "u", Username: "alice"}, nil, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reserved[:cap(reserved)]; got[1] != "" {
		t.Fatalf("the caller's slice was written through: %#v", got)
	}
}
