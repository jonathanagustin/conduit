// Package ocipublish publishes rendered Flux manifests as an OCI artifact, and
// reads back which source digest an environment is currently converging on.
//
// It exists because the Flux convergence path was unreachable without it. The
// converger renders manifests and hands them to a ManifestStore; the Hub binary
// supplied none, so `GITLESS_CONVERGENCE=flux` failed at startup and the whole
// feature could not be turned on. Describing the store as "supplied by the
// deployment" was true of the design and false of the product.
//
// The published artifact is what a cluster's Flux pulls. That is the whole point
// of Gitless GitOps: the manifests live in a registry, not in Git, and the
// cluster needs no Git credential to reach them.
//
// CurrentDigest answers a question about the SOURCE, not about this artifact.
// The converger asks "which digest is this environment converging on", and the
// answer is the `spec.ref.digest` inside the published OCIRepository -- not the
// digest of the manifest blob carrying it. Confusing the two would make every
// republish look like a source change.
package ocipublish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
)

const (
	manifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	configMediaType   = "application/vnd.cncf.flux.config.v1+json"
	layerMediaType    = "application/vnd.cncf.flux.content.v1.tar+gzip"
	// maximumManifest bounds a read back from the registry. Rendered manifests
	// are a couple of kilobytes; an unbounded read would let a registry decide
	// how much memory the Hub spends.
	maximumManifest = 1 << 20
)

// Credentials supplies registry authentication. Empty strings mean anonymous,
// which is what a local or public registry accepts.
type Credentials interface {
	Basic(ctx context.Context, host string) (username, password string, err error)
}

// Anonymous is the credential source for a registry that needs none.
type Anonymous struct{}

func (Anonymous) Basic(context.Context, string) (string, string, error) { return "", "", nil }

// Publisher writes rendered manifests to a registry repository, one tag per
// environment.
type Publisher struct {
	client      *http.Client
	credentials Credentials
	host        string
	name        string
	// insecure sends http rather than https. It exists for a local registry in
	// a test or an air-gapped mirror that terminates TLS elsewhere, and it is
	// explicit rather than inferred from the host so it can never be switched
	// on by a hostname somebody chose.
	//
	// It governs reaching THIS registry. It does not reach the token exchange:
	// a credential is posted over https whatever this says. See the
	// registrychallenge package.
	insecure bool
	// realms are the cross-host token realms an operator declared. Nil means
	// this registry may only direct credentials at itself.
	realms registrychallenge.Allowlist
}

// Option configures a Publisher.
type Option func(*Publisher)

// WithTokenRealms declares which token realm hosts each registry may direct
// credentials to, beyond its own host.
func WithTokenRealms(realms registrychallenge.Allowlist) Option {
	return func(publisher *Publisher) { publisher.realms = realms }
}

// New parses "registry.example/team/manifests" into its parts and refuses
// anything that is not registry-qualified.
func New(repository string, client *http.Client, credentials Credentials, insecure bool,
	options ...Option) (*Publisher, error) {
	host, name, found := strings.Cut(strings.TrimSpace(repository), "/")
	if !found || !strings.Contains(host, ".") && !strings.Contains(host, ":") {
		return nil, fmt.Errorf(
			"%q is not a registry-qualified repository; it must start with a registry host such as ghcr.io",
			repository)
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if credentials == nil {
		credentials = Anonymous{}
	}
	publisher := &Publisher{
		// The redirect policy travels with the client rather than with each
		// credentialed request: a push offers the credential in do() and in
		// token(), and a rule remembered in two places is a rule that will be
		// remembered in one. See registrychallenge/redirect.go.
		client: registrychallenge.WithRedirectPolicy(client), credentials: credentials,
		host: host, name: name, insecure: insecure,
	}
	for _, option := range options {
		option(publisher)
	}
	return publisher, nil
}

// ownHost reports whether a URL addresses the registry this Publisher was built
// for, at the scheme it was built for.
//
// Scheme is part of the test, not decoration: an https publisher downgraded to
// http on its own host still puts the credential on the wire in cleartext, and
// a passive observer is a real reader.
func (publisher *Publisher) ownHost(target *url.URL) bool {
	if target == nil {
		return false
	}
	return strings.EqualFold(target.Host, publisher.host) &&
		strings.EqualFold(target.Scheme, publisher.scheme())
}

func (publisher *Publisher) scheme() string {
	if publisher.insecure {
		return "http"
	}
	return "https"
}

func (publisher *Publisher) url(format string, arguments ...any) string {
	return fmt.Sprintf("%s://%s/v2/%s%s", publisher.scheme(), publisher.host, publisher.name,
		fmt.Sprintf(format, arguments...))
}

func digestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Put publishes the manifests for one environment under a tag of that name.
func (publisher *Publisher) Put(ctx context.Context, environment string, manifest []byte) error {
	if strings.TrimSpace(environment) == "" {
		return fmt.Errorf("publishing needs a named environment")
	}
	config := []byte("{}")
	if err := publisher.putBlob(ctx, config); err != nil {
		return fmt.Errorf("publish config blob: %w", err)
	}
	if err := publisher.putBlob(ctx, manifest); err != nil {
		return fmt.Errorf("publish manifest blob: %w", err)
	}

	document, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     manifestMediaType,
		"config":        descriptor(configMediaType, config),
		"layers":        []any{descriptor(layerMediaType, manifest)},
	})
	if err != nil {
		return err
	}
	request, err := publisher.request(ctx, http.MethodPut, publisher.url("/manifests/%s", environment), bytes.NewReader(document))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", manifestMediaType)
	response, err := publisher.do(ctx, request, func() io.Reader { return bytes.NewReader(document) })
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return fmt.Errorf("registry answered HTTP %d publishing %s", response.StatusCode, environment)
	}
	return nil
}

func descriptor(mediaType string, content []byte) map[string]any {
	return map[string]any{"mediaType": mediaType, "digest": digestOf(content), "size": len(content)}
}

// putBlob uploads one blob, skipping the upload when the registry already has
// it. Re-uploading an identical blob every interval would make a five-minute
// reconcile loop a five-minute upload loop.
func (publisher *Publisher) putBlob(ctx context.Context, content []byte) error {
	digest := digestOf(content)
	head, err := publisher.request(ctx, http.MethodHead, publisher.url("/blobs/%s", digest), nil)
	if err != nil {
		return err
	}
	existing, err := publisher.do(ctx, head, nil)
	if err != nil {
		return err
	}
	existing.Body.Close()
	if existing.StatusCode == http.StatusOK {
		return nil
	}

	start, err := publisher.request(ctx, http.MethodPost, publisher.url("/blobs/uploads/"), nil)
	if err != nil {
		return err
	}
	session, err := publisher.do(ctx, start, nil)
	if err != nil {
		return err
	}
	session.Body.Close()
	if session.StatusCode != http.StatusAccepted {
		return fmt.Errorf("registry answered HTTP %d starting a blob upload", session.StatusCode)
	}
	location := session.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("registry accepted a blob upload without saying where to send it")
	}
	if strings.HasPrefix(location, "/") {
		location = fmt.Sprintf("%s://%s%s", publisher.scheme(), publisher.host, location)
	}
	separator := "?"
	if strings.Contains(location, "?") {
		separator = "&"
	}
	upload, err := publisher.request(ctx, http.MethodPut, location+separator+"digest="+digest, bytes.NewReader(content))
	if err != nil {
		return err
	}
	upload.Header.Set("Content-Type", "application/octet-stream")
	finished, err := publisher.do(ctx, upload, func() io.Reader { return bytes.NewReader(content) })
	if err != nil {
		return err
	}
	defer finished.Body.Close()
	if finished.StatusCode != http.StatusCreated {
		return fmt.Errorf("registry answered HTTP %d finishing a blob upload", finished.StatusCode)
	}
	return nil
}

// CurrentDigest reports the source digest inside the published manifests, or
// empty when nothing has been published for this environment yet.
func (publisher *Publisher) CurrentDigest(ctx context.Context, environment string) (string, error) {
	request, err := publisher.request(ctx, http.MethodGet, publisher.url("/manifests/%s", environment), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", manifestMediaType)
	response, err := publisher.do(ctx, request, nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		// Never published. Absent is not an error: it is what an environment
		// that has not converged yet looks like.
		return "", nil
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry answered HTTP %d reading %s", response.StatusCode, environment)
	}
	var document struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maximumManifest)).Decode(&document); err != nil {
		return "", fmt.Errorf("registry returned an unreadable manifest for %s: %w", environment, err)
	}
	if len(document.Layers) == 0 {
		return "", fmt.Errorf("published artifact for %s carries no manifests", environment)
	}

	blob, err := publisher.request(ctx, http.MethodGet, publisher.url("/blobs/%s", document.Layers[0].Digest), nil)
	if err != nil {
		return "", err
	}
	fetched, err := publisher.do(ctx, blob, nil)
	if err != nil {
		return "", err
	}
	defer fetched.Body.Close()
	if fetched.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry answered HTTP %d reading the published manifests for %s", fetched.StatusCode, environment)
	}
	body, err := io.ReadAll(io.LimitReader(fetched.Body, maximumManifest))
	if err != nil {
		return "", err
	}
	return sourceDigest(body)
}

// sourceDigest reads spec.ref.digest out of the published OCIRepository.
//
// It parses rather than pattern-matches. A regular expression over the document
// would also match a digest in a comment or in some future field, and this
// answer decides whether a deployment happens.
func sourceDigest(document []byte) (string, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(document))
	for {
		var object struct {
			Kind string `yaml:"kind"`
			Spec struct {
				Ref struct {
					Digest string `yaml:"digest"`
				} `yaml:"ref"`
			} `yaml:"spec"`
		}
		if err := decoder.Decode(&object); err != nil {
			break
		}
		if object.Kind == "OCIRepository" && object.Spec.Ref.Digest != "" {
			return object.Spec.Ref.Digest, nil
		}
	}
	return "", fmt.Errorf("the published manifests declare no OCIRepository digest")
}

func (publisher *Publisher) request(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, body)
}

// do sends a request, following one Basic or Bearer challenge. rewind supplies
// the body again for the retry, because a challenged request has already
// consumed it.
func (publisher *Publisher) do(ctx context.Context, request *http.Request, rewind func() io.Reader) (*http.Response, error) {
	// Captured before the first round trip, because a Transport may rewrite
	// request.URL in place -- the test harness's routed transport does exactly
	// that, and a proxy or mirror in production may too. Reading it afterwards
	// would test where the request was SENT rather than where this code chose to
	// send it, which is the decision that matters here.
	intended := *request.URL
	response, err := publisher.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", request.Method, request.URL.Redacted(), err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		return response, nil
	}
	challenge := response.Header.Get("WWW-Authenticate")
	response.Body.Close()

	// The credential belongs to publisher.host and goes nowhere else. This is the
	// last gate before it is attached, deliberately: every request that could
	// carry it passes through here, so one check covers paths that do not exist
	// yet as well as the one that did.
	//
	// It exists because the token realm was not the only place a registry could
	// name a destination. A blob upload answers with a Location header, and an
	// absolute Location was used verbatim -- so a registry could reply
	// `Location: http://attacker.example/hijacked`, answer the PUT with a 401,
	// and be handed the push robot's password in cleartext. Constraining the
	// realm alone left that open; constraining the credential itself does not.
	if !publisher.ownHost(&intended) {
		return nil, fmt.Errorf(
			"refusing to send %s credentials to %s: a registry may not direct them to another host",
			publisher.host, intended.Redacted(),
		)
	}
	basic := strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "basic")
	var username, password, token string
	if basic {
		username, password, err = publisher.credentials.Basic(ctx, publisher.host)
		if err != nil {
			return nil, fmt.Errorf("credentials for %s: %w", publisher.host, err)
		}
		if username == "" && password == "" {
			return nil, fmt.Errorf("registry requires Basic authentication but no credentials are configured for %s", publisher.host)
		}
	} else {
		token, err = publisher.token(ctx, challenge)
		if err != nil {
			return nil, err
		}
	}
	var retryBody io.Reader
	if rewind != nil {
		retryBody = rewind()
	}
	retry, err := http.NewRequestWithContext(ctx, request.Method, request.URL.String(), retryBody)
	if err != nil {
		return nil, err
	}
	retry.Header = request.Header.Clone()
	if basic {
		retry.SetBasicAuth(username, password)
	} else {
		retry.Header.Set("Authorization", "Bearer "+token)
	}
	sent, err := publisher.client.Do(retry)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", retry.Method, retry.URL.Redacted(), err)
	}
	return sent, nil
}

func (publisher *Publisher) token(ctx context.Context, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", fmt.Errorf("registry refused the credentials offered for %s", publisher.host)
	}
	realm, service := "", ""
	for _, part := range strings.Split(challenge[len("Bearer "):], ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "realm":
			realm = strings.Trim(value, `"`)
		case "service":
			service = strings.Trim(value, `"`)
		}
	}
	if realm == "" {
		return "", fmt.Errorf("registry issued a challenge with no realm")
	}
	// Constrained BEFORE the credential is read. The realm came out of an
	// unauthenticated 401, so it names wherever that response wanted the push
	// credential sent. `insecure` is deliberately not consulted: it says this
	// registry is reached over http, not that a push credential may be posted
	// in the clear.
	endpoint, err := publisher.realms.TokenEndpoint(realm, publisher.host)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		registrychallenge.WithScope(endpoint, "repository:"+publisher.name+":pull,push", service), nil)
	if err != nil {
		return "", err
	}
	username, password, err := publisher.credentials.Basic(ctx, publisher.host)
	if err != nil {
		return "", fmt.Errorf("credentials for %s: %w", publisher.host, err)
	}
	if username != "" || password != "" {
		request.SetBasicAuth(username, password)
	}
	response, err := publisher.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("get a push token for %s: %w", publisher.name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint answered HTTP %d", response.StatusCode)
	}
	var issued struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maximumManifest)).Decode(&issued); err != nil {
		return "", err
	}
	if issued.Token != "" {
		return issued.Token, nil
	}
	if issued.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned an empty token")
	}
	return issued.AccessToken, nil
}
