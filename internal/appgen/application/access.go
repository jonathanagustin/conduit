package application

import (
	"context"
	"strings"
)

// owns reports whether actor may see a row recorded for owner.
// A blank owner is a row from before owners were recorded. It is not visible
// to anyone, including a caller who also has no name.
func owns(actor, owner string) bool {
	actor = strings.TrimSpace(actor)
	owner = strings.TrimSpace(owner)
	return actor != "" && owner != "" && actor == owner
}

// conceal records a refused read and leaves the caller with the same answer
// a missing id produces. The audit is the record that the row existed.
func (service Service) conceal(ctx context.Context, actor, id string) error {
	if service.Access == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	return service.Access.Deny(ctx, Denial{
		Actor:  strings.TrimSpace(actor),
		ID:     id,
		Action: ActionRead,
		Reason: "the generation is not visible to this operator",
	})
}
