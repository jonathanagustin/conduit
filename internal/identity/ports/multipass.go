package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// MultipassDirectory reads identity out of Multipass, and reads nothing else.
//
// Multipass is an identity SOURCE here and never an authority. This port has no
// method that answers "may they", returns no verdict, and carries no grant:
// everything it produces is an input Governance then decides about. The reason
// to say that in the interface's own documentation rather than only in a design
// note is that the recovered service has authorization-shaped reads on it —
// marking access, constraint verification, organization permissions — and the
// cheapest possible mistake is to add one here later because it was available.
//
// Three methods, and each is here because a Conduit read needs it:
//
//   - User resolves the stable Multipass id back to a username and the derived
//     display attributes. The id is what survives a rename, so it is what a
//     federated Conduit record should carry; every later render of that record —
//     a Change Request's author, an authorization audit entry's subject — has to
//     turn it back into something a person recognises.
//
//   - UsersByIDs is the plural of that read, and it is a separate method rather
//     than a loop because the recovered contract has a real batch route. A page
//     of the authorization audit resolves tens of subjects at once, and doing it
//     one round trip at a time turns one read of Conduit's own table into N
//     calls to somebody else's IAM service.
//
//   - GroupsForUser is the only read that feeds Governance. ResolvePrincipal
//     intersects a principal's groups with each Team's AdminGroups and
//     MemberGroups, and those groups are exactly what this returns. It is also
//     the only read here whose result can widen what a principal may do, which
//     is why FederatedClaims — not an adapter — decides what a group is called
//     by the time Governance sees it.
//
// What is deliberately absent is longer than what is present, and the omissions
// are choices rather than a backlog:
//
//   - The caller's-own-user read (`GET /me`) resolves an identity FROM a bearer
//     token. Conduit does not authenticate against Multipass and this work is
//     explicitly not a login flow; a port method that turned a token into a user
//     would be the first half of one, and would be reached for as such.
//   - Principal search, and the enumerate-every-user and enumerate-every-group
//     administration reads, have no Conduit caller. Enumerating a tenant's whole
//     directory is not a thing to build before something needs it.
//   - Every group-membership WRITE on the recovered administration service is
//     out of scope by construction. Conduit federates with an identity source;
//     it does not administer one.
//   - The deprecated first-generation per-user group read is omitted in favour
//     of the current one, so there is no second path with different cursor
//     semantics for a caller to pick wrongly.
//
// An implementation reports domain.ErrMultipassPrincipalNotFound, wrapped or
// bare, when the directory's stable answer is that no such principal exists.
// Any other error means the directory could not be consulted, and a caller must
// not read it as absence.
type MultipassDirectory interface {
	// User returns one user by its Multipass id.
	User(ctx context.Context, userID string) (domain.MultipassUser, error)

	// UsersByIDs returns the users among userIDs that exist.
	//
	// It does NOT report absence: the recovered batch route answers with the
	// users it found rather than failing on the first id it did not, so a caller
	// that needs to know an id was missing must compare what it asked for with
	// what came back. Stating that here is the point of the comment — an
	// implementation that returned ErrMultipassPrincipalNotFound for a partial
	// result would make a page of audit entries unrenderable because one subject
	// had been deleted.
	UsersByIDs(ctx context.Context, userIDs []string) ([]domain.MultipassUser, error)

	// GroupsForUser returns one page of the groups a user belongs to.
	GroupsForUser(
		ctx context.Context, userID string, page domain.MultipassGroupPage,
	) (domain.MultipassGroupsPage, error)
}
