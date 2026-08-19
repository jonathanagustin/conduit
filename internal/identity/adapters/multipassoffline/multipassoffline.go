// Package multipassoffline serves Multipass identity from memory, with no
// Multipass.
//
// It is the second adapter behind ports.MultipassDirectory and it exists for
// the same reason the file-backed resolution log exists beside the PostgreSQL
// one: a deployment, a demonstration and a test all need the federation to
// behave, and only one of those three has a Multipass to talk to. In this
// repository's sandbox there is no Multipass at all, so this is the only
// adapter that can be exercised end to end here.
//
// It is a fixture, not a fake service. It answers the three reads with data it
// was handed, in the recovered attribute shape, and it derives nothing: every
// derived value comes from the same domain methods the HTTP adapter's callers
// use. An offline adapter that computed "active" itself would be the exact
// divergence a paired adapter is supposed to make impossible.
package multipassoffline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// Directory answers directory reads from memory.
type Directory struct {
	users        map[string]domain.MultipassUser
	groups       map[string][]domain.MultipassGroup
	defaultOrder string
}

var _ ports.MultipassDirectory = (*Directory)(nil)

// New builds a directory from users and their group memberships.
//
// Memberships are keyed by user id, not by username, because the port is keyed
// by id and a fixture that indexed the other way would let a test pass while
// the real adapter looked the wrong thing up.
//
// The inputs are copied. A caller that kept the map it passed in and mutated it
// later would otherwise be changing a directory another goroutine is reading,
// and the resulting flake would look like a paging bug.
func New(users []domain.MultipassUser, memberships map[string][]domain.MultipassGroup) *Directory {
	directory := &Directory{
		users:        make(map[string]domain.MultipassUser, len(users)),
		groups:       make(map[string][]domain.MultipassGroup, len(memberships)),
		defaultOrder: "NAME",
	}
	for _, user := range users {
		directory.users[user.ID] = copyUser(user)
	}
	for userID, groups := range memberships {
		copied := make([]domain.MultipassGroup, 0, len(groups))
		for _, group := range groups {
			copied = append(copied, copyGroup(group))
		}
		sort.Slice(copied, func(left, right int) bool { return copied[left].Name < copied[right].Name })
		directory.groups[userID] = copied
	}
	return directory
}

// Seeded is the fixture the reference client's offline implementation carries,
// transcribed into the recovered attribute shape.
//
// Two users and two groups: an operator who belongs to Users, and an
// administrator who belongs to Administrators. It is deliberately small. A
// fixture large enough to be interesting is a fixture nobody reads, and the
// cases that matter here — an absent attribute, an empty attribute list — are
// constructed by the tests that care about them rather than hidden in a seed.
//
// Every value is synthetic. No tenant data is committed here.
func Seeded() *Directory {
	const realm = "conduit-offline-realm"
	const organization = "ri.multipass.local.organization.offline"
	user := func(id, username, given, family, email string) domain.MultipassUser {
		return domain.MultipassUser{ID: id, Username: username, Attributes: domain.MultipassAttributes{
			domain.MultipassAttributeGivenName:       {given},
			domain.MultipassAttributeFamilyName:      {family},
			domain.MultipassAttributeEmailPrimary:    {email},
			domain.MultipassAttributeRealm:           {realm},
			domain.MultipassAttributeOrganizationRID: {organization},
			domain.MultipassAttributeActive:          {"true"},
		}}
	}
	group := func(id, name, description string) domain.MultipassGroup {
		return domain.MultipassGroup{ID: id, Name: name, Attributes: domain.MultipassAttributes{
			domain.MultipassAttributeDescription: {description},
			domain.MultipassAttributeRealm:       {realm},
		}}
	}
	operator := user("offline-user-operator", "operator", "Ops", "Operator", "operator@example.test")
	administrator := user("offline-user-admin", "admin", "Platform", "Admin", "admin@example.test")
	users := group("offline-group-users", "Users", "All platform users")
	administrators := group("offline-group-administrators", "Administrators", "Platform administrators")
	return New(
		[]domain.MultipassUser{operator, administrator},
		map[string][]domain.MultipassGroup{
			operator.ID:      {users},
			administrator.ID: {administrators, users},
		},
	)
}

// User returns one user, or the port's absence sentinel.
func (directory *Directory) User(_ context.Context, userID string) (domain.MultipassUser, error) {
	user, found := directory.users[strings.TrimSpace(userID)]
	if !found {
		return domain.MultipassUser{}, fmt.Errorf("%w: user %s", domain.ErrMultipassPrincipalNotFound, userID)
	}
	return copyUser(user), nil
}

// UsersByIDs returns the users that exist among userIDs, in the order asked
// for, and says nothing about the ones that do not. That is the port's stated
// contract and it is what the recovered batch route does; an offline adapter
// that failed on a missing id instead would let a caller pass here and fail
// against a real service.
func (directory *Directory) UsersByIDs(_ context.Context, userIDs []string) ([]domain.MultipassUser, error) {
	var found []domain.MultipassUser
	for _, userID := range userIDs {
		if user, exists := directory.users[strings.TrimSpace(userID)]; exists {
			found = append(found, copyUser(user))
		}
	}
	return found, nil
}

// GroupsForUser pages a user's groups by integer offset, as the current route
// does.
//
// A user that exists with no groups is an empty page, not an absence: "belongs
// to nothing" is a real answer and a caller must be able to tell it from "no
// such user". A user that does not exist is the absence sentinel.
//
// Size <= 0 means the whole remainder, mirroring the HTTP adapter's choice to
// send no size and let the server apply its own default. The two are not the
// same number and are not meant to be: the offline adapter has no reason to
// pretend a page boundary exists where its data has none, and a test that
// depends on 50 would be depending on the recovered default rather than on
// anything Conduit decided.
func (directory *Directory) GroupsForUser(
	_ context.Context, userID string, page domain.MultipassGroupPage,
) (domain.MultipassGroupsPage, error) {
	userID = strings.TrimSpace(userID)
	if _, exists := directory.users[userID]; !exists {
		return domain.MultipassGroupsPage{},
			fmt.Errorf("%w: user %s", domain.ErrMultipassPrincipalNotFound, userID)
	}
	all := directory.groups[userID]
	from := page.From
	if from < 0 {
		from = 0
	}
	if from >= len(all) {
		return domain.MultipassGroupsPage{OrderedBy: directory.defaultOrder}, nil
	}
	end := len(all)
	if page.Size > 0 && from+page.Size < end {
		end = from + page.Size
	}
	window := make([]domain.MultipassGroup, 0, end-from)
	for _, group := range all[from:end] {
		window = append(window, copyGroup(group))
	}
	next := ""
	if end < len(all) {
		next = fmt.Sprint(end)
	}
	return domain.MultipassGroupsPage{Groups: window, Next: next, OrderedBy: directory.defaultOrder}, nil
}

func copyUser(user domain.MultipassUser) domain.MultipassUser {
	user.Attributes = copyAttributes(user.Attributes)
	return user
}

func copyGroup(group domain.MultipassGroup) domain.MultipassGroup {
	group.Attributes = copyAttributes(group.Attributes)
	return group
}

// copyAttributes deep-copies the value slices too. Copying only the map would
// leave every returned principal sharing one backing array per attribute, and a
// caller that appended to a returned attribute would edit the fixture.
func copyAttributes(attributes domain.MultipassAttributes) domain.MultipassAttributes {
	if attributes == nil {
		return nil
	}
	copied := make(domain.MultipassAttributes, len(attributes))
	for key, values := range attributes {
		copied[key] = append([]string(nil), values...)
	}
	return copied
}
