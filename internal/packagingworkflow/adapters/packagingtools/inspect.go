package packagingtools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const maxBlobBytes = 1 << 20

type imageRef struct {
	host     string
	repo     string
	tag      string
	digest   string
	insecure bool
}

func parseImageRef(raw string) (imageRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return imageRef{}, fmt.Errorf("image ref is empty")
	}
	var parsed imageRef
	if host, rest, ok := strings.Cut(raw, "://"); ok {
		switch strings.ToLower(host) {
		case "http":
			parsed.insecure = true
		case "https":
		default:
			return imageRef{}, fmt.Errorf("image ref scheme is not supported")
		}
		raw = rest
	}
	if name, digest, ok := strings.Cut(raw, "@"); ok {
		raw = name
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			return imageRef{}, fmt.Errorf("image digest is not a sha256 digest")
		}
		for _, r := range digest[len("sha256:"):] {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return imageRef{}, fmt.Errorf("image digest is not a sha256 digest")
			}
		}
		parsed.digest = digest
	}
	path := raw
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		return imageRef{}, fmt.Errorf("image ref is empty")
	}
	if looksLikeHost(parts[0]) {
		parsed.host = parts[0]
		if len(parts) < 2 {
			return imageRef{}, fmt.Errorf("image ref is missing a repository")
		}
		parsed.repo = strings.Join(parts[1:], "/")
	} else {
		parsed.host = "registry-1.docker.io"
		if len(parts) == 1 {
			parsed.repo = "library/" + parts[0]
		} else {
			parsed.repo = strings.Join(parts, "/")
		}
	}
	if name, tag, ok := strings.Cut(parsed.repo, ":"); ok && !strings.Contains(tag, "/") {
		parsed.repo = name
		parsed.tag = tag
	}
	if parsed.digest == "" && parsed.tag == "" {
		parsed.tag = "latest"
	}
	if !parsed.insecure {
		parsed.insecure = isLoopbackHost(parsed.host)
	}
	return parsed, nil
}

func looksLikeHost(part string) bool {
	host, _, err := net.SplitHostPort(part)
	if err == nil {
		return host != ""
	}
	return part == "localhost" || strings.Contains(part, ".") || strings.Contains(part, ":")
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (parsed imageRef) pathRef() string {
	if parsed.digest != "" {
		return parsed.digest
	}
	return url.PathEscape(parsed.tag)
}

func (parsed imageRef) origin() string {
	scheme := "https"
	if parsed.insecure {
		scheme = "http"
	}
	return scheme + "://" + parsed.host
}

func (service *Service) InspectImage(ctx context.Context, ref string) (domain.ImageInspect, error) {
	parsed, err := parseImageRef(ref)
	if err != nil {
		return domain.ImageInspect{}, err
	}
	origin := parsed.origin()
	if service.baseURL != "" {
		origin = strings.TrimRight(service.baseURL, "/")
	}
	manifest, manifestDigest, err := service.getManifest(ctx, origin, parsed)
	if err != nil {
		return domain.ImageInspect{}, err
	}
	configDigest, err := configDigestFromManifest(manifest)
	if err != nil {
		return domain.ImageInspect{}, err
	}
	config, err := service.getConfig(ctx, origin, parsed.repo, configDigest)
	if err != nil {
		return domain.ImageInspect{}, err
	}
	if manifestDigest == "" {
		manifestDigest = configDigest
	}
	return domain.ImageInspect{
		Ref:          ref,
		Digest:       manifestDigest,
		User:         config.User,
		ExposedPorts: keys(config.ExposedPorts),
		Entrypoint:   config.Entrypoint,
		Cmd:          config.Cmd,
		Env:          envNames(config.Env),
		Labels:       config.Labels,
	}, nil
}

type imageConfig struct {
	User         string              `json:"User"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	Env          []string            `json:"Env"`
	Entrypoint   []string            `json:"Entrypoint"`
	Cmd          []string            `json:"Cmd"`
	Labels       map[string]string   `json:"Labels"`
}

func (service *Service) getManifest(ctx context.Context, origin string, parsed imageRef) (map[string]any, string, error) {
	body, digest, media, err := service.get(ctx, origin, "/v2/"+parsed.repo+"/manifests/"+parsed.pathRef(),
		"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json")
	if err != nil {
		return nil, "", err
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, "", fmt.Errorf("decode image manifest: %w", err)
	}
	if isIndex(media, manifest) {
		child, err := firstManifest(manifest)
		if err != nil {
			return nil, "", err
		}
		body, digest, _, err = service.get(ctx, origin, "/v2/"+parsed.repo+"/manifests/"+url.PathEscape(child),
			"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		if err != nil {
			return nil, "", err
		}
		manifest = map[string]any{}
		if err := json.Unmarshal(body, &manifest); err != nil {
			return nil, "", fmt.Errorf("decode image manifest: %w", err)
		}
	}
	return manifest, digest, nil
}

func (service *Service) getConfig(ctx context.Context, origin, repo, digest string) (imageConfig, error) {
	body, _, _, err := service.get(ctx, origin, "/v2/"+repo+"/blobs/"+digest, "application/vnd.oci.image.config.v1+json, application/json")
	if err != nil {
		return imageConfig{}, err
	}
	var wrapper struct {
		Config imageConfig `json:"config"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return imageConfig{}, fmt.Errorf("decode image config: %w", err)
	}
	return wrapper.Config, nil
}

func (service *Service) get(ctx context.Context, origin, path, accept string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+path, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Accept", accept)
	resp, err := service.client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		if retry, ok := authorize(resp, req); ok {
			resp.Body.Close()
			resp, err = service.client.Do(retry.WithContext(ctx))
			if err != nil {
				return nil, "", "", err
			}
			defer resp.Body.Close()
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBlobBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(body) > maxBlobBytes {
		return nil, "", "", fmt.Errorf("registry response exceeds %d bytes", maxBlobBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("registry returned %s", resp.Status)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	return body, digest, resp.Header.Get("Content-Type"), nil
}

func authorize(resp *http.Response, original *http.Request) (*http.Request, bool) {
	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return nil, false
	}
	params := bearerParams(challenge[len("Bearer "):])
	realm, service, scope := params["realm"], params["service"], params["scope"]
	if realm == "" {
		return nil, false
	}
	tokenURL, err := url.Parse(realm)
	if err != nil {
		return nil, false
	}
	query := tokenURL.Query()
	if service != "" {
		query.Set("service", service)
	}
	if scope != "" {
		query.Set("scope", scope)
	}
	tokenURL.RawQuery = query.Encode()
	tokenResp, err := http.DefaultClient.Get(tokenURL.String())
	if err != nil {
		return nil, false
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusOK {
		return nil, false
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, 1<<16)).Decode(&payload); err != nil {
		return nil, false
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" {
		return nil, false
	}
	retry := original.Clone(original.Context())
	retry.Header.Set("Authorization", "Bearer "+token)
	return retry, true
}

func bearerParams(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		out[strings.ToLower(key)] = strings.Trim(value, `"`)
	}
	return out
}

func isIndex(media string, manifest map[string]any) bool {
	if strings.Contains(media, "image.index") || strings.Contains(media, "manifest.list") {
		return true
	}
	kind, _ := manifest["mediaType"].(string)
	return strings.Contains(kind, "image.index") || strings.Contains(kind, "manifest.list")
}

func firstManifest(index map[string]any) (string, error) {
	list, _ := index["manifests"].([]any)
	for _, item := range list {
		entry, _ := item.(map[string]any)
		digest, _ := entry["digest"].(string)
		if digest != "" {
			return digest, nil
		}
	}
	return "", fmt.Errorf("image index has no manifests")
}

func configDigestFromManifest(manifest map[string]any) (string, error) {
	config, _ := manifest["config"].(map[string]any)
	digest, _ := config["digest"].(string)
	if digest == "" {
		return "", fmt.Errorf("image manifest has no config digest")
	}
	return digest, nil
}

func envNames(values []string) []string {
	var names []string
	seen := map[string]bool{}
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func keys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
