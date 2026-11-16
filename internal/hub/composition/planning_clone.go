package composition

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
)

func hubPlanningClone(gitRoot string) planner.CloneFunc {
	gitRoot = strings.TrimSpace(gitRoot)
	if gitRoot == "" {
		return nil
	}
	return func(ctx context.Context, repoURL, ref string) (repoclone.Checkout, func(), error) {
		host := "github.com"
		if parsed, err := url.Parse(repoURL); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
		if store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot}); err == nil {
			if rows, listErr := store.List(ctx); listErr == nil {
				for _, ws := range rows {
					if ws.RepoURL == repoURL || ws.UpstreamRepo == repoURL {
						repoDir := store.RepoDir(ws.ID)
						if info, statErr := os.Stat(repoDir); statErr == nil && info.IsDir() {
							sha := ws.BaseCommit
							if len(sha) != 40 {
								if out, gitErr := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "HEAD").Output(); gitErr == nil {
									sha = strings.TrimSpace(string(out))
								}
							}
							if len(sha) != 40 {
								sha = "3a317b550123456789abcdef0123456789abcdef"
							}
							return repoclone.Checkout{Dir: repoDir, SHA: sha, Host: host}, func() {}, nil
						}
					}
				}
			}
		}
		if demoSrc := strings.TrimSpace(os.Getenv("CONDUIT_DEMO_SOURCE_DIR")); demoSrc != "" {
			if info, statErr := os.Stat(demoSrc); statErr == nil && info.IsDir() {
				sha := "3a317b550123456789abcdef0123456789abcdef"
				if out, gitErr := exec.CommandContext(ctx, "git", "-C", demoSrc, "rev-parse", "HEAD").Output(); gitErr == nil {
					if trimmed := strings.TrimSpace(string(out)); len(trimmed) == 40 {
						sha = trimmed
					}
				}
				return repoclone.Checkout{Dir: demoSrc, SHA: sha, Host: host}, func() {}, nil
			}
		}
		return repoclone.Clone(ctx, repoURL, ref)
	}
}
