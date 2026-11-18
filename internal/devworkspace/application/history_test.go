package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
	"strings"
	"testing"
	"time"
)

type historyFiles struct {
	ports.Files
	calls  int
	result domain.CommitHistory
	after  func()
}

func (files *historyFiles) History(context.Context, string, string) (domain.CommitHistory, error) {
	files.calls++
	if files.after != nil {
		files.after()
	}
	return files.result, nil
}
func TestHistoryScopesAccessAndRejectsLateContextChanges(t *testing.T) {
	for _, scenario := range []string{"healthy", "anonymous", "hidden", "stale head", "stale revision", "context changed", "owner changed", "invalid receipt"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, _ := newService(t)
			row, err := service.Create(t.Context(), caller(), "profile", "east", "History")
			if err != nil {
				t.Fatal(err)
			}
			row.Branch = "workspace/ada/history"
			row.HeadCommit = strings.Repeat("a", 40)
			if err := store.Save(t.Context(), row); err != nil {
				t.Fatal(err)
			}
			files := &historyFiles{Files: store.Files(), result: domain.CommitHistory{HeadCommit: row.HeadCommit, Entries: []domain.CommitSummary{{Commit: row.HeadCommit, CommittedAt: time.Now().UTC(), Subject: "fix: actual metadata"}}}}
			service.files = files
			actor := caller()
			head, revision := row.HeadCommit, row.ProposalRevision
			want := error(nil)
			switch scenario {
			case "anonymous":
				actor = domain.Caller{}
				want = domain.ErrUnauthorized
			case "hidden":
				actor.Username = "other"
				want = domain.ErrNotFound
			case "stale head":
				head = strings.Repeat("b", 40)
				want = domain.ErrStale
			case "stale revision":
				revision = "old"
				want = domain.ErrStale
			case "context changed":
				files.after = func() {
					row.SourceDigest = "sha256:changed"
					if err := store.Save(t.Context(), row); err != nil {
						t.Fatal(err)
					}
				}
				want = domain.ErrStale
			case "owner changed":
				files.after = func() {
					row.Owner = "other"
					if err := store.Save(t.Context(), row); err != nil {
						t.Fatal(err)
					}
				}
				want = domain.ErrNotFound
			case "invalid receipt":
				files.result.HeadCommit = strings.Repeat("b", 40)
				want = domain.ErrStale
			}
			result, err := service.History(t.Context(), actor, row.ID, revision, head, nil)
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatal("refusal", err, want)
				}
				if scenario != "context changed" && scenario != "owner changed" && scenario != "invalid receipt" && files.calls != 0 {
					t.Fatal("unauthorized or stale request reached history adapter")
				}
				return
			}
			if err != nil || !result.Current || result.ValueTier != domain.ValueTier || result.WorkspaceID != row.ID || result.Branch != row.Branch || result.SourceDigest != row.SourceDigest || result.ProposalRevision != row.ProposalRevision {
				t.Fatalf("bound result=%+v err=%v", result, err)
			}
		})
	}
}
func TestMetadataOnlyWorkspacesDoNotInventCommitHistory(t *testing.T) {
	service, _, _ := newService(t)
	row, err := service.Create(t.Context(), caller(), "profile", "east", "Metadata only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.History(t.Context(), caller(), row.ID, row.ProposalRevision, "", nil); !errors.Is(err, domain.ErrDisabled) {
		t.Fatal("fabricated history", err)
	}
}
