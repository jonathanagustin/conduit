// Package repoclone checks out a remote git ref into an isolated directory.
// Network access during the clone is limited to the repository URL's host.
package repoclone

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Checkout is one isolated worktree pinned to a commit SHA.
type Checkout struct {
	Dir  string
	SHA  string
	Host string
}

// Clone copies repoURL at ref into a temporary directory and resolves the
// commit SHA. Only the URL host is reachable during the fetch. The caller
// must call the returned cleanup function, even when err is not nil.
func Clone(ctx context.Context, repoURL, ref string) (Checkout, func(), error) {
	var zero Checkout
	cleanup := func() {}
	repoURL = strings.TrimSpace(repoURL)
	ref = strings.TrimSpace(ref)
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return zero, cleanup, fmt.Errorf("repository URL is not valid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return zero, cleanup, fmt.Errorf("repository URL must be an http or https URL")
	}
	if parsed.User != nil {
		return zero, cleanup, fmt.Errorf("repository URL must not include credentials")
	}
	if parsed.Host == "" {
		return zero, cleanup, fmt.Errorf("repository URL must include a host")
	}
	if ref == "" {
		return zero, cleanup, fmt.Errorf("ref is required")
	}
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, ":") || strings.Contains(ref, "..") || strings.ContainsAny(ref, " \t\r\n") {
		return zero, cleanup, fmt.Errorf("ref is not a valid git revision")
	}
	dir, err := os.MkdirTemp("", "conduit-planning-")
	if err != nil {
		return zero, cleanup, err
	}
	proxy, err := newHostProxy(parsed)
	if err != nil {
		os.RemoveAll(dir)
		return zero, cleanup, err
	}
	cleanup = func() {
		proxy.Close()
		os.RemoveAll(dir)
	}
	if err := runGit(ctx, dir, proxy.URL, "init", "--quiet"); err != nil {
		return zero, cleanup, err
	}
	if err := runGit(ctx, dir, proxy.URL, "remote", "add", "origin", repoURL); err != nil {
		return zero, cleanup, err
	}
	if err := fetchRef(ctx, dir, proxy.URL, ref); err != nil {
		expanded, expandErr := expandCommit(ctx, parsed, ref, proxy.URL)
		if expandErr != nil || expanded == "" || expanded == ref {
			return zero, cleanup, fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, err)
		}
		if err := fetchRef(ctx, dir, proxy.URL, expanded); err != nil {
			return zero, cleanup, fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, err)
		}
	}
	if err := runGit(ctx, dir, proxy.URL, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		if err := runGit(ctx, dir, proxy.URL, "checkout", "--quiet", "--detach", ref, "--"); err != nil {
			return zero, cleanup, fmt.Errorf("checkout %s: %w", ref, err)
		}
	}
	sha, err := gitOut(ctx, dir, proxy.URL, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return zero, cleanup, err
	}
	sha = strings.TrimSpace(sha)
	if len(sha) != 40 {
		return zero, cleanup, fmt.Errorf("resolved commit is invalid")
	}
	return Checkout{Dir: dir, SHA: sha, Host: parsed.Host}, cleanup, nil
}

func fetchRef(ctx context.Context, dir, proxyURL, ref string) error {
	if err := runGit(ctx, dir, proxyURL, "fetch", "--depth", "1", "--no-recurse-submodules", "--", "origin", ref); err != nil {
		return runGit(ctx, dir, proxyURL, "fetch", "--no-recurse-submodules", "--", "origin", ref)
	}
	return nil
}

var shortSHA = regexp.MustCompile(`(?i)^[0-9a-f]{4,39}$`)
var fromCommit = regexp.MustCompile(`(?i)^From\s+([0-9a-f]{40})\b`)

func expandCommit(ctx context.Context, repo *url.URL, ref, proxyURL string) (string, error) {
	if !shortSHA.MatchString(ref) {
		return "", fmt.Errorf("ref is not an abbreviated commit")
	}
	path := strings.TrimSuffix(strings.Trim(repo.Path, "/"), ".git")
	if path == "" {
		return "", fmt.Errorf("repository path is empty")
	}
	patchURL := *repo
	patchURL.Path = "/" + path + "/commit/" + ref + ".patch"
	patchURL.RawQuery = ""
	patchURL.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, patchURL.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "conduit-planning")
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(mustURL(proxyURL))},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !hostAllowed(req.URL.Host, req.URL.Scheme, canonicalHost(repo.Host, repo.Scheme)) {
				return fmt.Errorf("git host is not allowed")
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("commit lookup returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", err
	}
	// Prefer a "From <sha>" line from a patch. Fall back to the first 40-char
	// token that starts with the given prefix.
	prefix := strings.ToLower(ref)
	for _, line := range strings.Split(string(body), "\n") {
		if match := fromCommit.FindStringSubmatch(line); len(match) == 2 {
			sha := strings.ToLower(match[1])
			if strings.HasPrefix(sha, prefix) {
				return sha, nil
			}
		}
	}
	hexSHA := regexp.MustCompile(`(?i)\b[0-9a-f]{40}\b`)
	for _, match := range hexSHA.FindAllString(string(body), -1) {
		sha := strings.ToLower(match)
		if strings.HasPrefix(sha, prefix) {
			return sha, nil
		}
	}
	if location := response.Request.URL.Path; location != "" {
		base := location[strings.LastIndex(location, "/")+1:]
		base = strings.TrimSuffix(base, ".patch")
		if len(base) == 40 && strings.HasPrefix(strings.ToLower(base), prefix) {
			return strings.ToLower(base), nil
		}
	}
	return "", fmt.Errorf("could not expand abbreviated commit")
}

func mustURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return parsed
}

func runGit(ctx context.Context, dir, proxyURL string, args ...string) error {
	_, err := gitOut(ctx, dir, proxyURL, args...)
	return err
}

func gitOut(ctx context.Context, dir, proxyURL string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	config := []string{
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ssh.allow=never",
		"-c", "protocol.git.allow=never",
		"-c", "http.followRedirects=false",
		"-c", "http.proxy=" + proxyURL,
		"-c", "https.proxy=" + proxyURL,
		"-c", "credential.helper=",
	}
	cmd := exec.CommandContext(ctx, "git", append(config, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv(proxyURL)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func gitEnv(proxyURL string) []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM",
			"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "GIT_CONFIG_COUNT",
			"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
			"http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		default:
			if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
				continue
			}
			env = append(env, item)
		}
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_NO_LAZY_FETCH=1",
		"http_proxy="+proxyURL,
		"https_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
		"NO_PROXY=",
		"no_proxy=",
	)
}

type hostProxy struct {
	*http.Server
	URL      string
	listener net.Listener
}

func (proxy *hostProxy) Close() {
	if proxy == nil || proxy.Server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = proxy.Server.Shutdown(ctx)
	if proxy.listener != nil {
		_ = proxy.listener.Close()
	}
}

func newHostProxy(repo *url.URL) (*hostProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	allowed := canonicalHost(repo.Host, repo.Scheme)
	server := &http.Server{Handler: restrictHost(allowed), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return &hostProxy{Server: server, URL: "http://" + listener.Addr().String(), listener: listener}, nil
}

func restrictHost(allowed string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodConnect {
			if !hostAllowed(request.Host, "https", allowed) {
				http.Error(writer, "git host is not allowed", http.StatusForbidden)
				return
			}
			tunnel(writer, request)
			return
		}
		target := request.URL
		if target == nil || target.Host == "" {
			http.Error(writer, "git host is not allowed", http.StatusForbidden)
			return
		}
		if !hostAllowed(target.Host, target.Scheme, allowed) {
			http.Error(writer, "git host is not allowed", http.StatusForbidden)
			return
		}
		outbound, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), request.Body)
		if err != nil {
			http.Error(writer, "git host is not allowed", http.StatusBadGateway)
			return
		}
		outbound.Header = request.Header.Clone()
		response, err := http.DefaultTransport.RoundTrip(outbound)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		if location := response.Header.Get("Location"); location != "" {
			if redirected, err := response.Request.URL.Parse(location); err == nil {
				if !hostAllowed(redirected.Host, redirected.Scheme, allowed) {
					http.Error(writer, "git host is not allowed", http.StatusForbidden)
					return
				}
			}
		}
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, response.Body)
	})
}

func tunnel(writer http.ResponseWriter, request *http.Request) {
	destination, err := net.DialTimeout("tcp", request.Host, 15*time.Second)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		destination.Close()
		http.Error(writer, "proxy cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		destination.Close()
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go func() { _, _ = io.Copy(destination, client); destination.Close(); client.Close() }()
	_, _ = io.Copy(client, destination)
	destination.Close()
	client.Close()
}

func hostAllowed(host, scheme, allowed string) bool {
	return canonicalHost(host, scheme) == allowed
}

func canonicalHost(host, scheme string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name = host
		switch scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return strings.ToLower(name) + ":" + port
}
