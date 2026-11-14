package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
)

const (
	ActionCreate = "CREATE"
	ActionRead   = "READ"
	ActionMutate = "MUTATE"
)

// Decision comes from current authenticated authority, never request JSON.
// ManageOthers authorizes management in this exact Scope, not global ownership.
type Decision struct {
	Identity     domain.Identity
	ManageOthers bool
}

// Authority checks the current operator and exact scope. A stored request is
// not lasting authorization; every operation must consult authority again.
type Authority interface {
	Require(context.Context, string, domain.Scope) (Decision, error)
}
