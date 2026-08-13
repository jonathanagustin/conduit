package computecontrol

import "context"

// Controller is the provider-neutral lifecycle port recovered from the ten-operation cluster-manager service.
// Implementations translate these domain values with an exact generated provider client.
type Controller interface {
	Launch(context.Context, LaunchRequest) (ClusterHandle, error)
	Run(context.Context, ClusterRef, RunIntent) (JobID, error)
	ClusterStatus(context.Context, ClusterRef) (ClusterStatus, error)
	Restart(context.Context, ClusterRef) (RestartResult, error)
	Shutdown(context.Context, ClusterRef, ShutdownRequest) (ShutdownResult, error)
	Overview(context.Context, ClusterRef) (ClusterOverview, error)
	JobStatus(context.Context, JobRef) (JobStatus, error)
	CheckpointSummary(context.Context, JobRef) (CheckpointSummary, error)
	CheckpointDetails(context.Context, JobRef, CheckpointID) (CheckpointObservation, error)
	Cancel(context.Context, JobRef) error
}

// Profiles is the provider-neutral port recovered from the two-operation streaming-profile service.
type Profiles interface {
	AvailableProfiles(context.Context) ([]Profile, error)
	ImportProfile(context.Context, ProfileImport) error
}

// FlinkDialogueAdapter is the provider boundary for the recovered Rubix/Flink Dialogue control plane.
// A future adapter should use generated Conjure types internally; this package intentionally defines no wire DTOs.
type FlinkDialogueAdapter interface {
	Controller
	Profiles
}
