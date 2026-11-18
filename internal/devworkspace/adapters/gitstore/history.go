package gitstore

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (t treeAdapter) History(ctx context.Context, id, head string) (domain.CommitHistory, error) {
	return t.store.History(ctx, id, head)
}

// History reads bounded committed metadata, with no working-tree or network access.
func (store *Store) History(ctx context.Context, id, head string) (domain.CommitHistory, error) {
	if !domain.ValidCommitID(head) {
		return domain.CommitHistory{}, domain.ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.refreshCatalogLocked()
	row, ok := store.rows[id]
	if !ok {
		return domain.CommitHistory{}, domain.ErrNotFound
	}
	if row.HeadCommit != head {
		return domain.CommitHistory{}, domain.ErrStale
	}
	repo := store.RepoDir(id)
	actual, err := gitOut(ctx, repo, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return domain.CommitHistory{}, err
	}
	if strings.TrimSpace(actual) != head {
		return domain.CommitHistory{}, domain.ErrStale
	}
	branch, err := gitOut(ctx, repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) != row.Branch {
		return domain.CommitHistory{}, domain.ErrStale
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--no-replace-objects", "log", "--no-show-signature", "--no-color", "--encoding=UTF-8", "--no-decorate", "--topo-order", "--format=%H%x00%cI%x00%s%x00", "--max-count="+strconv.Itoa(domain.MaxHistoryEntries+1), "--end-of-options", head, "--")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return domain.CommitHistory{}, err
	}
	if err = cmd.Start(); err != nil {
		return domain.CommitHistory{}, err
	}
	const maximumHistoryBytes = 64 * 1024
	raw, readErr := io.ReadAll(io.LimitReader(stdout, maximumHistoryBytes+1))
	if readErr != nil || len(raw) > maximumHistoryBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return domain.CommitHistory{}, errors.New("workspace commit metadata exceeds the history bound")
	}
	if err = cmd.Wait(); err != nil {
		return domain.CommitHistory{}, errors.New("workspace commit metadata is unavailable")
	}
	fields := strings.Split(string(raw), "\x00")
	if len(fields) < 4 || (len(fields)-1)%3 != 0 || strings.TrimSpace(fields[len(fields)-1]) != "" {
		return domain.CommitHistory{}, errors.New("invalid Git history framing")
	}
	entries := make([]domain.CommitSummary, 0)
	for i := 0; i < len(fields)-1; i += 3 {
		commit := strings.TrimSpace(fields[i])
		at, err := time.Parse(time.RFC3339, fields[i+1])
		subject := fields[i+2]
		if !domain.ValidCommitID(commit) || err != nil || len(subject) > 4096 || !utf8.ValidString(subject) {
			return domain.CommitHistory{}, errors.New("invalid Git commit metadata")
		}
		entries = append(entries, domain.CommitSummary{Commit: commit, CommittedAt: at, Subject: subject})
	}
	actual, err = gitOut(ctx, repo, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	branch, branchErr := gitOut(ctx, repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branchErr != nil || strings.TrimSpace(actual) != head || strings.TrimSpace(branch) != row.Branch {
		return domain.CommitHistory{}, domain.ErrStale
	}
	truncated := len(entries) > domain.MaxHistoryEntries
	if truncated {
		entries = entries[:domain.MaxHistoryEntries]
	}
	return domain.CommitHistory{HeadCommit: head, Entries: entries, Truncated: truncated}, nil
}

var _ ports.History = treeAdapter{}
