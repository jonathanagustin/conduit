package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
	"unicode/utf8"
)

func historyContext(row domain.Workspace) [7]string {
	return [7]string{row.ID, row.Owner, row.EnvironmentID, row.Branch, row.HeadCommit, row.ProposalRevision, row.SourceDigest}
}

// History reads one visible workspace's real commits at its exact recorded head.
func (service *Service) History(ctx context.Context, caller domain.Caller, id, revision, head string, visible func(string) bool) (domain.CommitHistory, error) {
	row, err := service.Get(ctx, caller, id, visible)
	if err != nil {
		return domain.CommitHistory{}, err
	}
	reader, ok := service.files.(ports.History)
	if !ok || row.Branch == "" || !domain.ValidCommitID(row.HeadCommit) {
		return domain.CommitHistory{}, domain.ErrDisabled
	}
	if revision == "" || revision != row.ProposalRevision || head != row.HeadCommit {
		return domain.CommitHistory{}, domain.ErrStale
	}
	result, err := reader.History(ctx, id, head)
	if err != nil {
		return domain.CommitHistory{}, err
	}
	latest, err := service.Get(ctx, caller, id, visible)
	if err != nil {
		return domain.CommitHistory{}, err
	}
	if historyContext(row) != historyContext(latest) || result.HeadCommit != head {
		return domain.CommitHistory{}, domain.ErrStale
	}
	if len(result.Entries) == 0 || len(result.Entries) > domain.MaxHistoryEntries || result.Entries[0].Commit != head {
		return domain.CommitHistory{}, errors.New("invalid workspace history response")
	}
	seen := map[string]bool{}
	for _, entry := range result.Entries {
		if !domain.ValidCommitID(entry.Commit) || seen[entry.Commit] || entry.CommittedAt.IsZero() || len(entry.Subject) > 4096 || !utf8.ValidString(entry.Subject) {
			return domain.CommitHistory{}, errors.New("invalid workspace history entry")
		}
		seen[entry.Commit] = true
	}
	result.WorkspaceID = row.ID
	result.Branch = row.Branch
	result.ProposalRevision = row.ProposalRevision
	result.SourceDigest = row.SourceDigest
	result.Current = true
	result.ValueTier = domain.ValueTier
	return result, nil
}
