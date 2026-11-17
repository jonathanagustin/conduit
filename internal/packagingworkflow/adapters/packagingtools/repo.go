package packagingtools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	maxRepoTreeEntries = 400
	maxRepoFileBytes   = 64 * 1024
)

var skipDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".cache":       true,
}

// WithRepoRoot binds inspect_repo and read_file to a source checkout. Paths
// are resolved under this root only.
func WithRepoRoot(root string) Option {
	return func(service *Service) {
		service.repoRoot = root
	}
}

func (service *Service) boundRoot() (string, error) {
	root := strings.TrimSpace(service.repoRoot)
	if root == "" {
		return "", fmt.Errorf("no repository is bound")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	// Derive every returned relative path against the same canonical root used
	// for confinement, including checkouts reached through filesystem aliases.
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return abs, nil
}

// RepositoryBound reports whether inspect_repo and read_file have a source
// checkout to read: a root was given and it is an existing directory.
func (service *Service) RepositoryBound() bool {
	_, err := service.boundRoot()
	return err == nil
}

func (service *Service) InspectRepo(ctx context.Context) (domain.RepoInspect, error) {
	if err := ctx.Err(); err != nil {
		return domain.RepoInspect{}, err
	}
	root, err := service.boundRoot()
	if err != nil {
		return domain.RepoInspect{}, err
	}
	return inspectRepoRoot(root)
}

func (service *Service) ReadFile(ctx context.Context, rel string) (domain.RepoFile, error) {
	if err := ctx.Err(); err != nil {
		return domain.RepoFile{}, err
	}
	root, err := service.boundRoot()
	if err != nil {
		return domain.RepoFile{}, err
	}
	return readRepoFile(root, rel)
}

func inspectRepoRoot(root string) (domain.RepoInspect, error) {
	tree, err := listRepoTree(root)
	if err != nil {
		return domain.RepoInspect{}, err
	}
	out := domain.RepoInspect{Tree: tree}
	out.HasDockerfile = fileExists(root, "Dockerfile") || fileExists(root, "dockerfile")
	detectLanguage(&out, root)
	detectScriptsAndKind(&out, root)
	return out, nil
}

func listRepoTree(root string) ([]string, error) {
	var tree []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() && skipDirNames[entry.Name()] {
			tree = append(tree, rel+"/")
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if len(tree) >= maxRepoTreeEntries {
			return fs.SkipAll
		}
		tree = append(tree, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(tree)
	return tree, nil
}

func fileExists(root, name string) bool {
	info, err := os.Stat(filepath.Join(root, name))
	return err == nil && !info.IsDir()
}

func readOptional(root, name string) string {
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return ""
	}
	return string(data)
}

func detectLanguage(out *domain.RepoInspect, root string) {
	switch {
	case fileExists(root, "go.mod"):
		out.Language = "go"
		out.PackageManager = "go"
	case fileExists(root, "pnpm-lock.yaml"):
		out.Language = "javascript"
		out.PackageManager = "pnpm"
	case fileExists(root, "yarn.lock"):
		out.Language = "javascript"
		out.PackageManager = "yarn"
	case fileExists(root, "package.json"):
		out.Language = "javascript"
		out.PackageManager = "npm"
	case fileExists(root, "Cargo.toml"):
		out.Language = "rust"
		out.PackageManager = "cargo"
	case fileExists(root, "pyproject.toml") || fileExists(root, "requirements.txt"):
		out.Language = "python"
		out.PackageManager = "pip"
	case fileExists(root, "Gemfile"):
		out.Language = "ruby"
		out.PackageManager = "bundler"
	case fileExists(root, "composer.json"):
		out.Language = "php"
		out.PackageManager = "composer"
	case fileExists(root, "pom.xml"):
		out.Language = "java"
		out.PackageManager = "maven"
	default:
		if hasSuffixInTree(root, ".html") {
			out.Language = "html"
		}
	}
	if fileExists(root, "tsconfig.json") && out.Language == "javascript" {
		out.Language = "typescript"
	}
}

func hasSuffixInTree(root, suffix string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() && skipDirNames[entry.Name()] {
			if entry != nil && entry.IsDir() && skipDirNames[entry.Name()] {
				return filepath.SkipDir
			}
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func detectScriptsAndKind(out *domain.RepoInspect, root string) {
	if pkg := readOptional(root, "package.json"); pkg != "" {
		applyPackageJSON(out, pkg)
	}
	if fileExists(root, "Makefile") {
		out.BuildScripts = appendUnique(out.BuildScripts, "make")
	}
	if out.Language == "go" {
		out.BuildScripts = appendUnique(out.BuildScripts, "go build")
		if out.StartCommand == "" {
			out.StartCommand = "go run ."
		}
		out.Kind = "server"
	}
	if out.Kind == "" {
		if out.Language == "html" || looksStatic(root, out) {
			out.Kind = "static"
		} else if out.StartCommand != "" {
			out.Kind = "server"
		}
	}
	if ports := portsFromDockerfile(readOptional(root, "Dockerfile")); len(ports) > 0 && len(out.Ports) == 0 {
		out.Ports = ports
	}
	if len(out.Ports) == 0 && out.Kind != "" {
		out.Ports = []int{8080}
	}
}

func applyPackageJSON(out *domain.RepoInspect, raw string) {
	var pkg struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]any    `json:"dependencies"`
		DevDeps      map[string]any    `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
		return
	}
	for _, name := range []string{"build", "lint", "test", "start", "preview", "serve"} {
		if pkg.Scripts[name] != "" {
			out.BuildScripts = appendUnique(out.BuildScripts, "npm run "+name)
		}
	}
	if pkg.Scripts["start"] != "" {
		out.StartCommand = "npm start"
	} else if pkg.Scripts["preview"] != "" {
		out.StartCommand = "npm run preview"
	} else if pkg.Scripts["serve"] != "" {
		out.StartCommand = "npm run serve"
	}
	serverish := false
	for name := range mergeMaps(pkg.Dependencies, pkg.DevDeps) {
		switch strings.ToLower(name) {
		case "express", "fastify", "koa", "hono", "next", "@nestjs/core", "restify":
			serverish = true
		}
	}
	if serverish {
		out.Kind = "server"
	} else if pkg.Scripts["build"] != "" || pkg.Scripts["preview"] != "" || pkg.Scripts["serve"] != "" {
		out.Kind = "static"
	}
}

func looksStatic(root string, out *domain.RepoInspect) bool {
	if out.Language == "html" {
		return true
	}
	return fileExists(root, "index.html") || fileExists(root, "public/index.html")
}

func portsFromDockerfile(text string) []int {
	var ports []int
	for _, inst := range parseDockerfile(text) {
		if inst.cmd != "EXPOSE" {
			continue
		}
		for _, field := range strings.Fields(inst.args) {
			field = strings.SplitN(field, "/", 2)[0]
			port, err := strconv.Atoi(field)
			if err == nil && port > 0 {
				ports = append(ports, port)
			}
		}
	}
	return ports
}

func mergeMaps(parts ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			out[key] = value
		}
	}
	return out
}

func appendUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}

func readRepoFile(root, rel string) (domain.RepoFile, error) {
	clean, err := confinedPath(root, rel)
	if err != nil {
		return domain.RepoFile{}, err
	}
	// Refuse by the requested name and by the name a symlink resolves to.
	if secretPath(strings.TrimSpace(rel)) || secretPath(resolvedRel(root, clean)) {
		return domain.RepoFile{}, ErrSecretPath
	}
	file, err := os.Open(clean)
	if err != nil {
		return domain.RepoFile{}, fmt.Errorf("read file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return domain.RepoFile{}, err
	}
	if info.IsDir() {
		return domain.RepoFile{}, fmt.Errorf("path is a directory")
	}
	limited := io.LimitReader(file, maxRepoFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return domain.RepoFile{}, err
	}
	truncated := len(data) > maxRepoFileBytes
	if truncated {
		data = data[:maxRepoFileBytes]
	}
	if isBinary(data) {
		return domain.RepoFile{}, fmt.Errorf("path is a binary file")
	}
	sum := sha256.Sum256(data)
	display, err := filepath.Rel(root, clean)
	if err != nil {
		return domain.RepoFile{}, fmt.Errorf("repository-relative file path: %w", err)
	}
	return domain.RepoFile{
		Path:      filepath.ToSlash(display),
		Content:   redactSecrets(string(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		Truncated: truncated,
	}, nil
}

// resolvedRel is the symlink-resolved path relative to the repository root.
// It falls back to the file name when the relation cannot be computed.
func resolvedRel(root, abs string) string {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(abs)
	}
	return rel
}

func confinedPath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(rel) || pathLooksAbsolute(rel) {
		return "", fmt.Errorf("path must be inside the repository")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must be inside the repository")
	}
	abs := filepath.Join(root, cleaned)
	abs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Fall back to the unresolved path so missing files still error as not found
		// after the confinement check on the logical path.
		abs = filepath.Join(root, cleaned)
		relToRoot, relErr := filepath.Rel(root, abs)
		if relErr != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("path must be inside the repository")
		}
		return abs, nil
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved = root
	}
	relToRoot, err := filepath.Rel(rootResolved, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must be inside the repository")
	}
	return abs, nil
}

func pathLooksAbsolute(rel string) bool {
	return strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || (len(rel) > 1 && rel[1] == ':')
}

func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 512 {
		limit = 512
	}
	return strings.IndexByte(string(data[:limit]), 0) >= 0
}
