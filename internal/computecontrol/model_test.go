package computecontrol

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLaunchSeparatesPortableIntentFromSpokeMaterial(t *testing.T) {
	request := LaunchRequest{
		Intent: ClusterIntent{EngineVersion: "1.14", JobClass: "example.Job", Profiles: []string{"streaming"}, StateBackend: "rocks-db"},
		Spoke:  SpokeMaterial{ResourceChannel: "channel-ref", NetworkPolicyRefs: []string{"policy-ref"}, SidecarApplicationRef: "sidecar-ref"},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	encodedIntent, err := json.Marshal(request.Intent)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"channel-ref", "policy-ref", "sidecar-ref", "AccessRef", "token"} {
		if strings.Contains(string(encodedIntent), forbidden) {
			t.Fatalf("portable intent leaked spoke material %q: %s", forbidden, encodedIntent)
		}
	}
}

func TestValidationRejectsAmbiguousOrIncompleteLifecycleIntent(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"launch missing engine", (LaunchRequest{Spoke: SpokeMaterial{ResourceChannel: "channel"}}).Validate()},
		{"launch missing channel", (LaunchRequest{Intent: ClusterIntent{EngineVersion: "1.14"}}).Validate()},
		{"run missing artifact", (RunIntent{Parallelism: 1}).Validate()},
		{"run ambiguous artifact", (RunIntent{Parallelism: 1, Artifact: ArtifactLocator{ArtifactRef: "a", AssetRef: "b"}}).Validate()},
		{"run invalid parallelism", (RunIntent{Artifact: ArtifactLocator{ArtifactRef: "a"}}).Validate()},
		{"restore missing path", (RunIntent{Parallelism: 1, Artifact: ArtifactLocator{ArtifactRef: "a"}, Restore: &RestoreSettings{}}).Validate()},
		{"cluster access missing", (ClusterRef{ID: "cluster"}).Validate()},
		{"job ID missing", (JobRef{Cluster: ClusterRef{ID: "cluster", AccessRef: "access-ref"}}).Validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRecoveredPortContainsAllTwelveOperations(t *testing.T) {
	var _ FlinkDialogueAdapter = recordingAdapter{}
	port := reflect.TypeOf((*FlinkDialogueAdapter)(nil)).Elem()
	methods := make([]string, port.NumMethod())
	for index := range port.NumMethod() {
		methods[index] = port.Method(index).Name
	}
	want := []string{"AvailableProfiles", "Cancel", "CheckpointDetails", "CheckpointSummary", "ClusterStatus", "ImportProfile", "JobStatus", "Launch", "Overview", "Restart", "Run", "Shutdown"}
	if !slices.Equal(methods, want) {
		t.Fatalf("adapter operations = %v, want %v", methods, want)
	}
}

type recordingAdapter struct{}

func (recordingAdapter) Launch(context.Context, LaunchRequest) (ClusterHandle, error) {
	return ClusterHandle{}, nil
}
func (recordingAdapter) Run(context.Context, ClusterRef, RunIntent) (JobID, error) { return "", nil }
func (recordingAdapter) ClusterStatus(context.Context, ClusterRef) (ClusterStatus, error) {
	return ClusterStatus{}, nil
}
func (recordingAdapter) Restart(context.Context, ClusterRef) (RestartResult, error) { return "", nil }
func (recordingAdapter) Shutdown(context.Context, ClusterRef, ShutdownRequest) (ShutdownResult, error) {
	return ShutdownResult{}, nil
}
func (recordingAdapter) Overview(context.Context, ClusterRef) (ClusterOverview, error) {
	return ClusterOverview{}, nil
}
func (recordingAdapter) JobStatus(context.Context, JobRef) (JobStatus, error) {
	return JobStatus{}, nil
}
func (recordingAdapter) CheckpointSummary(context.Context, JobRef) (CheckpointSummary, error) {
	return CheckpointSummary{}, nil
}
func (recordingAdapter) CheckpointDetails(context.Context, JobRef, CheckpointID) (CheckpointObservation, error) {
	return CheckpointObservation{}, nil
}
func (recordingAdapter) Cancel(context.Context, JobRef) error                 { return nil }
func (recordingAdapter) AvailableProfiles(context.Context) ([]Profile, error) { return nil, nil }
func (recordingAdapter) ImportProfile(context.Context, ProfileImport) error   { return nil }
