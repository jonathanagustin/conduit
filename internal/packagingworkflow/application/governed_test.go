package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestGovernedSelectsAcceptedCommitThroughStoreBeforeModel(t *testing.T) {
	for _, tc := range []struct {
		name, commit   string
		selectionError error
		wantCalls      int
	}{
		{name: "accepted commit", commit: "abc123", wantCalls: 1},
		{name: "selection refused", commit: "abc123", selectionError: errors.New("accepted commit unavailable"), wantCalls: 0},
		{name: "no accepted commit", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{selectErr: tc.selectionError}
			writer := &stubWriter{proposals: []domain.PackageProposal{{}}}
			packager := WorkspacePackager{Store: store, Writer: writer, Checks: &stubChecks{}, Builder: &stubBuilder{}, Inspector: &stubInspector{}}
			result, err := packager.Governed(context.Background(), GovernedRequest{Commit: tc.commit, AppName: "fixture", ImageTag: "fixture:v1"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.commit != "" && (store.selectedDir != "/tmp/ws" || store.selectedCommit != tc.commit) {
				t.Fatalf("wrong accepted source binding: dir=%q commit=%q", store.selectedDir, store.selectedCommit)
			}
			if tc.commit == "" && (store.selectedDir != "" || store.selectedCommit != "") {
				t.Fatal("selected a commit when none was requested")
			}
			if writer.calls != tc.wantCalls {
				t.Fatalf("model calls=%d want %d", writer.calls, tc.wantCalls)
			}
			if tc.selectionError != nil && result.Error != tc.selectionError.Error() {
				t.Fatalf("selection refusal lost: %+v", result)
			}
			if len(store.commits) != 0 {
				t.Fatal("committed files before model supplied a Dockerfile")
			}
		})
	}
}
