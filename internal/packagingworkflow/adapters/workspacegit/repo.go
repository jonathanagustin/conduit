// Package workspacegit is a git-backed packaging workspace. It commits only
// the files it is given, on an isolated branch.
package workspacegit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot is the HEAD of a prepared workspace branch.
type Snapshot struct {
	Dir    string
	Branch string
	Head   string
	Repo   string
	Ref    string
}

// Repo is one isolated checkout.
type Repo struct {
	Dir    string
	Branch string
}

// Prepare initializes or clones a repository and checks out an isolated branch.
func Prepare(ctx context.Context, dir, repoURL, ref, branch string) (Snapshot, error) {
	if strings.TrimSpace(dir) == "" {
		return Snapshot{}, fmt.Errorf("workspace directory is required")
	}
	if strings.TrimSpace(branch) == "" {
		branch = "workspace/onboard"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, err
	}
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if strings.TrimSpace(repoURL) != "" {
			if err := runGit(ctx, "", "clone", "--depth", "1", "--no-recurse-submodules", repoURL, dir); err != nil {
				return Snapshot{}, err
			}
			if strings.TrimSpace(ref) != "" {
				_ = runGit(ctx, dir, "fetch", "--depth", "1", "--no-recurse-submodules", "origin", ref)
				if err := runGit(ctx, dir, "checkout", "--detach", ref); err != nil {
					if err := runGit(ctx, dir, "checkout", ref); err != nil {
						return Snapshot{}, err
					}
				}
			}
		} else if err := runGit(ctx, dir, "init"); err != nil {
			return Snapshot{}, err
		}
	}
	_ = runGit(ctx, dir, "config", "user.name", "workspace")
	_ = runGit(ctx, dir, "config", "user.email", "workspace@local")
	if err := runGit(ctx, dir, "checkout", "-B", branch); err != nil {
		// An empty repository has no HEAD yet; create the branch after the first commit.
		_ = runGit(ctx, dir, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	}
	head, _ := gitOut(ctx, dir, "rev-parse", "HEAD")
	return Snapshot{Dir: dir, Branch: branch, Head: strings.TrimSpace(head), Repo: repoURL, Ref: ref}, nil
}

// Commit writes the given paths only and creates one commit. Paths outside the
// repository or named like secret files are rejected.
func (repo Repo) Commit(ctx context.Context, files map[string]string, message string) (string, error) {
	if repo.Dir == "" {
		return "", fmt.Errorf("workspace directory is required")
	}
	if strings.TrimSpace(message) == "" {
		message = "workspace package update"
	}
	for path, body := range files {
		clean, err := confined(repo.Dir, path)
		if err != nil {
			return "", err
		}
		if secretPath(path) {
			return "", fmt.Errorf("refusing to commit %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(clean, []byte(body), 0o644); err != nil {
			return "", err
		}
		if err := runGit(ctx, repo.Dir, "add", "--", path); err != nil {
			return "", err
		}
	}
	args := []string{"-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "--message", message, "--"}
	for path := range files {
		args = append(args, path)
	}
	if err := runGit(ctx, repo.Dir, args...); err != nil {
		return "", err
	}
	head, err := gitOut(ctx, repo.Dir, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

func confined(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be inside the workspace")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must be inside the workspace")
	}
	return filepath.Join(root, clean), nil
}

func secretPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return true
	case base == "id_rsa" || strings.HasPrefix(base, "id_"):
		return true
	case strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12"):
		return true
	case base == "credentials" || strings.Contains(base, "secret"):
		return true
	default:
		return false
	}
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := gitOut(ctx, dir, args...)
	return err
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}
