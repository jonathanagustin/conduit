// Package computecontrol defines the provider-neutral streaming compute lifecycle.
//
// Provenance: com.palantir.flink.manager:flink-cluster-manager-api-{objects,dialogue}:0.519.2.
// These are domain intents and observations, not a handwritten copy of the recovered Conjure wire
// beans. A provider adapter owns translation to its generated client and keeps credentials local.
package computecontrol

import (
	"errors"
	"fmt"
	"strings"
)

type ClusterID string
type JobID string
type CheckpointID string

// ClusterIntent is portable job-cluster intent. None of its fields authorize or locate provider resources.
type ClusterIntent struct {
	EngineVersion string
	JobClass      string
	Profiles      []string
	StateBackend  string
}

// SpokeMaterial contains environment-owned scheduling and integration references.
// References are opaque identifiers resolved by the spoke; credentials and provider payloads do not cross this port.
type SpokeMaterial struct {
	ResourceChannel       string
	ExtraConfiguration    map[string]string
	NetworkPolicyRefs     []string
	SidecarApplicationRef string
	ResourceConstraintRef string
	UsePlatformMetrics    bool
}

type LaunchRequest struct {
	Intent ClusterIntent
	Spoke  SpokeMaterial
}

// ArtifactLocator is portable job input. Exactly one locator should be set.
type ArtifactLocator struct {
	ApplicationCoordinate string
	ArtifactRef           string
	AssetRef              string
}

type RestoreSettings struct {
	Path                  string
	AllowNonRestoredState bool
}

type RunIntent struct {
	ProgramArguments []string
	Artifact         ArtifactLocator
	Parallelism      int
	StableJobID      JobID
	Restore          *RestoreSettings
}

// AccessRef names spoke-local authorization material. It is deliberately not a bearer token.
type AccessRef string

type ClusterHandle struct {
	ID          ClusterID
	AccessRef   AccessRef
	Description string
	Resources   ClusterResources
}

type ClusterResources struct {
	JobManager ComputeResources
	TaskRunner ComputeResources
}

type ComputeResources struct {
	Cores     float64
	MemoryMiB float64
	Instances int
}

type ClusterRef struct {
	ID        ClusterID
	AccessRef AccessRef
}

type JobRef struct {
	Cluster ClusterRef
	JobID   JobID
}

type ClusterState string

const (
	ClusterQueued     ClusterState = "queued"
	ClusterActive     ClusterState = "active"
	ClusterRestarting ClusterState = "restarting"
	ClusterFailed     ClusterState = "failed"
	ClusterCompleted  ClusterState = "completed"
)

type ClusterStatus struct {
	State       ClusterState
	Description string
}

type JobState string

const (
	JobSubmitted JobState = "submitted"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobKilled    JobState = "killed"
	JobFailed    JobState = "failed"
)

type JobStatus struct {
	State           JobState
	FailureSummary  string
	ExecutionCounts map[string]int
}

type ClusterOverview struct {
	TaskManagers int
	Slots        int
	FreeSlots    int
	RunningJobs  int
	Engine       string
	Runtime      string
}

type CheckpointState string

const (
	CheckpointInProgress CheckpointState = "in-progress"
	CheckpointCompleted  CheckpointState = "completed"
	CheckpointFailed     CheckpointState = "failed"
)

type CheckpointSummary struct {
	History []CheckpointObservation
}

type CheckpointObservation struct {
	ID                   CheckpointID
	State                CheckpointState
	Savepoint            bool
	StateSizeBytes       int64
	DurationMillis       int64
	Subtasks             int
	AcknowledgedSubtasks int
}

type ShutdownRequest struct {
	Savepoint bool
}

type RestartResult string

const (
	RestartSucceeded RestartResult = "succeeded"
	RestartFailed    RestartResult = "failed"
	RestartShutdown  RestartResult = "cluster-shutdown"
)

type ShutdownResult struct {
	SavepointPath string
	Failure       string
}

type Profile struct {
	Name        string
	Description string
	Category    string
}

type ProfileImport struct {
	ProfileName string
	ProjectRef  string
}

func (request LaunchRequest) Validate() error {
	if blank(request.Intent.EngineVersion) {
		return errors.New("engine version is required")
	}
	if blank(request.Spoke.ResourceChannel) {
		return errors.New("spoke resource channel is required")
	}
	return nil
}

func (intent RunIntent) Validate() error {
	if intent.Parallelism < 1 {
		return errors.New("parallelism must be positive")
	}
	locators := 0
	for _, value := range []string{intent.Artifact.ApplicationCoordinate, intent.Artifact.ArtifactRef, intent.Artifact.AssetRef} {
		if !blank(value) {
			locators++
		}
	}
	if locators != 1 {
		return fmt.Errorf("exactly one artifact locator is required, got %d", locators)
	}
	if intent.Restore != nil && blank(intent.Restore.Path) {
		return errors.New("restore path is required when restore settings are present")
	}
	return nil
}

func (ref ClusterRef) Validate() error {
	if blank(string(ref.ID)) || blank(string(ref.AccessRef)) {
		return errors.New("cluster ID and spoke-local access reference are required")
	}
	return nil
}

func (ref JobRef) Validate() error {
	if err := ref.Cluster.Validate(); err != nil {
		return err
	}
	if blank(string(ref.JobID)) {
		return errors.New("job ID is required")
	}
	return nil
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }
