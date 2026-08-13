package controlplane

import (
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/environments"
)

// An invalid profile has always answered 409, and writeLifecycleError decides
// that from ErrConflict. Environments calls the same condition ErrInvalid,
// which is right in a package that knows nothing about HTTP, so this package
// restates it on the way out.
//
// This test exists because deleting that restatement breaks no other test: the
// profile endpoints would begin answering 500, and nothing would say so.
func TestInvalidProfileStillReadsAsConflict(t *testing.T) {
	t.Parallel()
	profile := EnvironmentProfile{
		EnvironmentID: "dev-use2",
		Accreditation: "il4", // lowercase: not a valid accreditation
		Source:        EnvironmentProfileSourceOperatorAPI,
		UpdatedBy:     "operator",
		Rationale:     "because",
		CreatedAt:     time.Unix(0, 0).UTC(),
		UpdatedAt:     time.Unix(0, 0).UTC(),
		Revision:      1,
	}
	err := validateEnvironmentProfile(profile)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("an invalid profile must read as ErrConflict so the handler answers 409, got %v", err)
	}
	if !errors.Is(err, environments.ErrInvalid) {
		t.Fatalf("the specific reason must survive the restatement, got %v", err)
	}
}
