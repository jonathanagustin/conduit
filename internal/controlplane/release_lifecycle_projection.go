package controlplane

import (
	"context"
	environmentapp "github.com/jonathanagustin/conduit/internal/environments/application"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// releaseLifecycleProjectionPort answers what has been withdrawn and what has been
// promoted from the Plans the repository holds, which is the only copy both Hub
// replicas can see. Apollo asked its own memory before this existed, and its
// memory was written only by the request that accepted the desired state: the
// replica that did not serve that POST reported a recalled Release as not
// recalled, and so did every replica after a restart.
//
// The cost is one repository read per Apollo catalog read. On this Hub that is
// the same unit of work every other read route already pays -- the repository
// materializes its whole state per operation either way -- and it buys an
// answer that does not depend on which process the client reached.
type releaseLifecycleProjectionPort struct {
	repository environmentProjectionPlanReader
}

func (port releaseLifecycleProjectionPort) ReleaseLifecycle(ctx context.Context) (environments.ReleaseLifecycle, error) {
	return (environmentapp.ProjectionReader{Source: environmentProjectionSource(port)}).ReleaseLifecycle(ctx)
}
