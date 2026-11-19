package controlplane

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidEnvironmentDetailInput distinguishes selection decoding failures
// from native lifecycle errors returned by a Space authorization callback.
var ErrInvalidEnvironmentDetailInput = errors.New("invalid Environment detail input")

// ErrEnvironmentDetailUnavailable identifies only a missing native Space
// capability, retaining lifecycle categories for callback/catalog failures.
var ErrEnvironmentDetailUnavailable = errors.New("Environment detail unavailable")

// environmentDetailSpaceError retains which existing Space boundary failed so
// REST can keep its error-only input/configuration bodies without relabeling a
// catalog error or a callback's native lifecycle error.
type environmentDetailSpaceError struct {
	err   error
	stage string
}

func (err *environmentDetailSpaceError) Error() string { return err.err.Error() }
func (err *environmentDetailSpaceError) Unwrap() error { return err.err }

func (err *environmentDetailSpaceError) Is(target error) bool {
	switch target {
	case ErrInvalidEnvironmentDetailInput:
		return err.stage == "decode"
	case ErrEnvironmentDetailUnavailable:
		return err.stage == "authorize" && errors.Is(err.err, ErrSpaceSelectionUnavailable) ||
			err.stage == "membership" && errors.Is(err.err, ErrSpaceMembershipUnavailable)
	default:
		return false
	}
}

// ReadAuthorizedEnvironmentDetail shares the full native REST read with wire
// adapters. Callers authenticate the original carrier first. selectSpace is a
// pure, lazy decoder: resource authority must precede even malformed selection.
// A nil selector means unscoped; a present empty identity remains invalid.
// The clock is sampled only after catalog reads and a matching record, retaining
// the REST liveness boundary when a repository read is slow.
func ReadAuthorizedEnvironmentDetail(ctx context.Context, repository EnvironmentCatalogReader, governance *GovernanceService, principal Principal, options HandlerOptions, environment string, selectSpace func() (*string, error), now func() time.Time) (EnvironmentCatalogRecord, error) {
	if !governance.AuthorizeResourceOperation(ctx, principal, options, environmentRID(environment), OperationReadResource) {
		return EnvironmentCatalogRecord{}, ErrForbidden
	}
	var id *string
	if selectSpace != nil {
		var err error
		id, err = selectSpace()
		if err != nil {
			return EnvironmentCatalogRecord{}, &environmentDetailSpaceError{err: err, stage: "decode"}
		}
	}
	spaceID := ""
	if id != nil {
		if !nativeSpaceSelectionID.MatchString(*id) {
			return EnvironmentCatalogRecord{}, &environmentDetailSpaceError{err: ErrInvalidSpaceSelection, stage: "decode"}
		}
		spaceID = *id
	}
	if err := authorizeIndexSpaceSelection(ctx, principal, options, spaceID); err != nil {
		return EnvironmentCatalogRecord{}, &environmentDetailSpaceError{err: err, stage: "authorize"}
	}
	space, err := loadIndexSpaceMembership(ctx, principal, options, spaceID)
	if err != nil {
		return EnvironmentCatalogRecord{}, &environmentDetailSpaceError{err: err, stage: "membership"}
	}
	if !space.environment(environment) {
		return EnvironmentCatalogRecord{}, ErrNotFound
	}
	records, err := ListEnvironmentCatalog(ctx, repository)
	if err != nil {
		return EnvironmentCatalogRecord{}, err
	}
	for _, record := range records {
		if record.Environment.ID == environment {
			if now == nil {
				now = time.Now
			}
			record.Spoke = spokeLiveness(record.Reported, now().UTC())
			return record, nil
		}
	}
	return EnvironmentCatalogRecord{}, ErrNotFound
}
