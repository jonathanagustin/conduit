// Package repoclone checks out a remote git ref into an isolated directory.
// Network access during the clone is limited to the repository URL's host,
// and to addresses the operator's source-host policy permits.
package repoclone

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Checkout is one isolated worktree pinned to a commit SHA.
type Checkout struct {
	Dir  string
	SHA  string
	Host string
	// ConnectionID names the operator's source connection whose credential
	// was offered to the host. It is empty for an anonymous fetch. It is an
	// identifier and never the secret.
	ConnectionID string
	// Cached reports that the files came from an earlier fetch of the same
	// commit and nothing was downloaded for this call.
	Cached bool
}

// Clone checks out repoURL at ref under the default policy with loopback
// permitted and no checkout cache. Link-local, metadata and private addresses
// are refused. It exists for callers and fixtures that predate Cloner. The Hub
// builds a Cloner from operator configuration instead.
func Clone(ctx context.Context, repoURL, ref string) (Checkout, func(), error) {
	return legacy.Clone(ctx, repoURL, ref)
}

var legacy = &Cloner{policy: Policy{Timeout: DefaultTimeout, MaxBytes: DefaultMaxBytes}, loopback: true}

// Clone copies repoURL at ref into a temporary directory and resolves the
// commit SHA. Only the URL host is reachable during the fetch, and only when
// the source-host policy permits the name and every address it connects to.
// The caller must call the returned cleanup function, even when err is not nil.
func (cloner *Cloner) Clone(ctx context.Context, repoURL, ref string) (Checkout, func(), error) {
	checkout, cleanup, err := cloner.clone(ctx, repoURL, ref)
	if cloner.policy.OnCheckout != nil {
		event := Event{Host: checkout.Host, Commit: checkout.SHA, ConnectionID: checkout.ConnectionID, Cached: checkout.Cached}
		if err != nil {
			event.Error = err.Error()
			if parsed, parseErr := url.Parse(strings.TrimSpace(repoURL)); parseErr == nil {
				event.Host = parsed.Host
			}
		}
		cloner.policy.OnCheckout(event)
	}
	return checkout, cleanup, err
}

func (cloner *Cloner) clone(parent context.Context, repoURL, ref string) (Checkout, func(), error) {
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
	if err := cloner.checkName(parsed); err != nil {
		return zero, cleanup, err
	}
	credential, err := cloner.credentialFor(parsed)
	if err != nil {
		return zero, cleanup, err
	}
	timeout := cloner.policy.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithCancelCause(parent)
	timer := time.AfterFunc(timeout, func() { cancel(ErrTimeLimit) })
	dir, err := os.MkdirTemp("", "conduit-planning-")
	if err != nil {
		timer.Stop()
		cancel(nil)
		return zero, cleanup, err
	}
	proxy, err := cloner.newHostProxy(parsed)
	if err != nil {
		timer.Stop()
		cancel(nil)
		os.RemoveAll(dir)
		return zero, cleanup, err
	}
	git := gitRunner{proxyURL: proxy.URL, credential: credential, observe: cloner.observe}
	stopWatch := watchSize(ctx, dir, cloner.policy.MaxBytes, cancel)
	// release ends the network and limit machinery. The checkout directory
	// outlives it and is removed by the returned cleanup.
	release := func() {
		stopWatch()
		timer.Stop()
		cancel(nil)
		proxy.Close()
	}
	cleanup = func() {
		release()
		os.RemoveAll(dir)
	}
	// limited replaces a git failure with the limit or policy refusal that
	// caused it, so the caller sees the reason and not git's exit status.
	limited := func(err error) error {
		if cause := context.Cause(ctx); errors.Is(cause, ErrTooLarge) || errors.Is(cause, ErrTimeLimit) {
			return fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, cause)
		}
		if denied := proxy.refusal(); denied != nil {
			return fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, denied)
		}
		return err
	}
	if err := git.run(ctx, dir, "init", "--quiet"); err != nil {
		return zero, cleanup, limited(err)
	}
	if err := git.run(ctx, dir, "remote", "add", "origin", repoURL); err != nil {
		return zero, cleanup, limited(err)
	}
	key := cacheKey{url: repoURL, connection: credential.id}
	if cloner.cache != nil {
		if key.sha = cloner.remoteCommit(ctx, git, dir, key, ref); key.sha != "" {
			if entry := cloner.cache.acquire(key); entry != nil {
				copied, copyErr := copyOut(entry.dir)
				cloner.cache.release(entry)
				if copyErr == nil {
					cleanup()
					return Checkout{Dir: copied, SHA: entry.key.sha, Host: parsed.Host, ConnectionID: credential.id, Cached: true},
						func() { os.RemoveAll(copied) }, nil
				}
			}
		}
	}
	if err := fetchRef(ctx, git, dir, ref); err != nil {
		if limitErr := limited(nil); limitErr != nil {
			return zero, cleanup, limitErr
		}
		expanded, expandErr := expandCommit(ctx, parsed, ref, proxy.URL, credential.caFile)
		if expandErr != nil || expanded == "" || expanded == ref {
			return zero, cleanup, limited(fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, err))
		}
		if err := fetchRef(ctx, git, dir, expanded); err != nil {
			return zero, cleanup, limited(fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, err))
		}
	}
	if err := git.run(ctx, dir, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		if err := git.run(ctx, dir, "checkout", "--quiet", "--detach", ref, "--"); err != nil {
			return zero, cleanup, limited(fmt.Errorf("checkout %s: %w", ref, err))
		}
	}
	sha, err := git.out(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return zero, cleanup, limited(err)
	}
	sha = strings.TrimSpace(sha)
	if len(sha) != 40 {
		return zero, cleanup, fmt.Errorf("resolved commit is invalid")
	}
	// The watcher samples. This check always runs, and it covers the working
	// tree the checkout just expanded.
	if overLimit(dir, cloner.policy.MaxBytes) {
		return zero, cleanup, fmt.Errorf("clone %s at %s: %w", parsed.Host, ref, ErrTooLarge)
	}
	checkout := Checkout{Dir: dir, SHA: sha, Host: parsed.Host, ConnectionID: credential.id}
	if cloner.cache == nil {
		return checkout, cleanup, nil
	}
	// The cache keeps a private copy. The caller gets its own directory, so a
	// write there cannot change what the next turn reads.
	key.sha = sha
	if stored, copyErr := copyOut(dir); copyErr == nil {
		cloner.cache.add(key, stored)
	}
	release()
	return checkout, func() { os.RemoveAll(dir) }, nil
}

var fullSHA = regexp.MustCompile(`(?i)^[0-9a-f]{40}$`)

// remoteCommit names the commit ref points at without fetching it. It returns
// "" when that is not known, and the caller clones.
func (cloner *Cloner) remoteCommit(ctx context.Context, git gitRunner, dir string, key cacheKey, ref string) string {
	if fullSHA.MatchString(ref) {
		return strings.ToLower(ref)
	}
	if shortSHA.MatchString(ref) {
		if sha := cloner.cache.expand(key, strings.ToLower(ref)); sha != "" {
			return sha
		}
	}
	// A peeled tag is listed with its tag, so it needs no pattern of its own.
	listed, err := git.out(ctx, dir, "ls-remote", "--", "origin", ref, "refs/"+ref, "refs/tags/"+ref, "refs/heads/"+ref)
	if err != nil {
		return ""
	}
	found := map[string]string{}
	for _, line := range strings.Split(listed, "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && fullSHA.MatchString(sha) {
			found[name] = strings.ToLower(sha)
		}
	}
	// This is the order git fetch uses to resolve a short ref name.
	for _, name := range []string{ref, "refs/" + ref, "refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref} {
		if sha := found[name]; sha != "" {
			return sha
		}
	}
	return ""
}

func fetchRef(ctx context.Context, git gitRunner, dir, ref string) error {
	if err := git.run(ctx, dir, "fetch", "--depth", "1", "--no-recurse-submodules", "--", "origin", ref); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return git.run(ctx, dir, "fetch", "--no-recurse-submodules", "--", "origin", ref)
	}
	return nil
}

var shortSHA = regexp.MustCompile(`(?i)^[0-9a-f]{4,39}$`)
var fromCommit = regexp.MustCompile(`(?i)^From\s+([0-9a-f]{40})\b`)

// expandCommit asks the host's web endpoint for the full form of an
// abbreviated commit. The request carries no source-connection credential.
func expandCommit(ctx context.Context, repo *url.URL, ref, proxyURL, caFile string) (string, error) {
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
	transport := &http.Transport{Proxy: http.ProxyURL(mustURL(proxyURL))}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return "", fmt.Errorf("source connection CA file is not readable")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", fmt.Errorf("source connection CA file holds no certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
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

// gitRunner starts git for one clone with the proxy and, when a source
// connection applies, its credential.
type gitRunner struct {
	proxyURL   string
	credential credential
	// observe sees the argv and environment of each git process. Tests use it
	// to prove where the credential is and is not.
	observe func(argv, env []string)
}

func (git gitRunner) run(ctx context.Context, dir string, args ...string) error {
	_, err := git.out(ctx, dir, args...)
	return err
}

func (git gitRunner) out(ctx context.Context, dir string, args ...string) (string, error) {
	config := []string{
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ssh.allow=never",
		"-c", "protocol.git.allow=never",
		"-c", "http.followRedirects=false",
		"-c", "http.proxy=" + git.proxyURL,
		"-c", "https.proxy=" + git.proxyURL,
		"-c", "credential.helper=",
	}
	cmd := exec.CommandContext(ctx, "git", append(config, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv(git.proxyURL, git.credential)
	// A killed git can leave a transport helper holding the output pipe.
	cmd.WaitDelay = 2 * time.Second
	if git.observe != nil {
		git.observe(cmd.Args, cmd.Env)
	}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		output := git.credential.redact(strings.TrimSpace(out.String()))
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return out.String(), nil
}

func gitEnv(proxyURL string, credential credential) []string {
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
	env = append(env,
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
	// The credential is git configuration delivered in the environment. It is
	// not on the command line, so a process listing does not show it, and the
	// key carries the connection's own origin, so git offers it to no other.
	pairs := credential.config()
	if len(pairs) == 0 {
		return env
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(pairs)))
	for index, pair := range pairs {
		env = append(env,
			"GIT_CONFIG_KEY_"+strconv.Itoa(index)+"="+pair[0],
			"GIT_CONFIG_VALUE_"+strconv.Itoa(index)+"="+pair[1],
		)
	}
	return env
}

type hostProxy struct {
	*http.Server
	URL       string
	listener  net.Listener
	transport *http.Transport
	denied    atomic.Pointer[error]
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
	if proxy.transport != nil {
		proxy.transport.CloseIdleConnections()
	}
}

// refusal is the policy error that stopped a connection, if one did.
func (proxy *hostProxy) refusal() error {
	if denied := proxy.denied.Load(); denied != nil {
		return *denied
	}
	return nil
}

func (proxy *hostProxy) refuse(err error) bool {
	if !errors.Is(err, ErrAddressRefused) {
		return false
	}
	refusal := error(ErrAddressRefused)
	proxy.denied.Store(&refusal)
	return true
}

func (cloner *Cloner) newHostProxy(repo *url.URL) (*hostProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	allowed := canonicalHost(repo.Host, repo.Scheme)
	// The transport dials the host itself. It does not use an outbound proxy
	// from the environment, because the address rule can only be enforced on
	// a connection this process makes.
	transport := &http.Transport{
		DialContext:           cloner.dial,
		ResponseHeaderTimeout: 2 * time.Minute,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	proxy := &hostProxy{URL: "http://" + listener.Addr().String(), listener: listener, transport: transport}
	proxy.Server = &http.Server{Handler: proxy.restrictHost(allowed, cloner.dial), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = proxy.Server.Serve(listener) }()
	return proxy, nil
}

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// dial connects to address and refuses, at connect time, any resolved address
// the policy does not permit for that name. Checking here and not at lookup
// means a name that changes its answer between the two cannot pass.
func (cloner *Cloner) dial(ctx context.Context, network, address string) (net.Conn, error) {
	exact := cloner.named(address)
	dialer := net.Dialer{Timeout: 15 * time.Second, Control: func(_, resolved string, _ syscall.RawConn) error {
		return cloner.checkResolved(resolved, exact)
	}}
	return dialer.DialContext(ctx, network, address)
}

func (proxy *hostProxy) restrictHost(allowed string, dial dialFunc) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodConnect {
			if !hostAllowed(request.Host, "https", allowed) {
				http.Error(writer, "git host is not allowed", http.StatusForbidden)
				return
			}
			proxy.tunnel(writer, request, dial)
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
		response, err := proxy.transport.RoundTrip(outbound)
		if err != nil {
			if proxy.refuse(err) {
				http.Error(writer, ErrAddressRefused.Error(), http.StatusForbidden)
				return
			}
			http.Error(writer, "git host is not reachable", http.StatusBadGateway)
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

func (proxy *hostProxy) tunnel(writer http.ResponseWriter, request *http.Request, dial dialFunc) {
	destination, err := dial(request.Context(), "tcp", request.Host)
	if err != nil {
		if proxy.refuse(err) {
			http.Error(writer, ErrAddressRefused.Error(), http.StatusForbidden)
			return
		}
		http.Error(writer, "git host is not reachable", http.StatusBadGateway)
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
