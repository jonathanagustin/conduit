package domain

import "time"

// MaxHistoryEntries bounds a Studio snapshot; omitted commits remain explicit.
const MaxHistoryEntries = 20

// CommitSummary is committed Git metadata, never a working-tree change.
type CommitSummary struct {
	Commit      string    `json:"commit"`
	CommittedAt time.Time `json:"committedAt"`
	Subject     string    `json:"subject"`
}

// CommitHistory is pinned to the workspace's recorded Git head and source context.
// It is a bounded snapshot, not a permanent or complete commit inventory.
//
// CONDUIT-INVENTED: devworkspace.CommitHistory
// Displaced-by: An authoritative workspace commit-history contract and paging rules.
type CommitHistory struct {
	WorkspaceID      string          `json:"workspaceId"`
	Branch           string          `json:"branch"`
	HeadCommit       string          `json:"headCommit"`
	ProposalRevision string          `json:"proposalRevision"`
	SourceDigest     string          `json:"sourceDigest"`
	Entries          []CommitSummary `json:"entries"`
	Truncated        bool            `json:"truncated"`
	Current          bool            `json:"current"`
	ValueTier        string          `json:"valueTier"`
}

// ValidCommitID accepts canonical full SHA-1 or SHA-256 object ids, not revision syntax.
func ValidCommitID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
