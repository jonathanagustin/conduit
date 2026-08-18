// Package ociresolve answers which exact bytes a selector names, over the OCI
// distribution API.
//
// It is deliberately small. Resolution asks a registry one question -- what
// digest does this reference point at right now -- and the registry answers it
// in a response header. Pulling in a registry client library to read one header
// would add a dependency surface far larger than the thing it does.
//
// A digest selector is not sent to the registry at all. It already names exact
// bytes, and asking a registry to confirm a digest invites it to answer
// something else; the domain would catch that, but not asking is simpler than
// catching. What the registry is for is turning a mutable name into an
// immutable one.
package ociresolve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// acceptable lists the manifest media types a registry may answer with. Sending
// them all matters: a registry given no Accept header may return a v1 manifest
// with a different digest than the one a client pulling normally would get.
var acceptable = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

var digestHeader = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Credentials supplies registry authentication. Returning an empty string is
// how an anonymous pull is expressed, which public registries accept.
type Credentials interface {
	// Basic returns the username and password for a registry host.
	Basic(ctx context.Context, host string) (username, password string, err error)
}

// Anonymous is the credential source for public artifacts.
type Anonymous struct{}

func (Anonymous) Basic(context.Context, string) (string, string, error) { return "", "", nil }

// Resolver turns a selector into a pin.
type Resolver struct {
	client      *http.Client
	credentials Credentials
}

func New(client *http.Client, credentials Credentials) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if credentials == nil {
		credentials = Anonymous{}
	}
	return &Resolver{client: client, credentials: credentials}
}

func (resolver *Resolver) Resolve(ctx context.Context, selector domain.Selector) (domain.Pin, error) {
	if err := selector.Validate(); err != nil {
		return domain.Pin{}, err
	}
	if selector.Kind == domain.ByDigest {
		// Already exact. See the package comment: there is nothing to ask.
		return domain.Pin{Repository: selector.Repository, Digest: selector.Digest}, nil
	}

	host, name, err := split(selector.Repository)
	if err != nil {
		return domain.Pin{}, err
	}
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, name, selector.Channel)

	digest, err := resolver.head(ctx, url, "")
	var challenge *authChallenge
	if asChallenge(err, &challenge) {
		if challenge.basic {
			username, password, credentialErr := resolver.credentials.Basic(ctx, host)
			if credentialErr != nil {
				return domain.Pin{}, fmt.Errorf("credentials for %s: %w", host, credentialErr)
			}
			if username == "" && password == "" {
				return domain.Pin{}, fmt.Errorf(
					"registry requires Basic authentication but no credentials are configured for %s", host)
			}
			authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
			digest, err = resolver.head(ctx, url, authorization)
		} else {
			token, tokenErr := resolver.token(ctx, host, name, challenge)
			if tokenErr != nil {
				return domain.Pin{}, tokenErr
			}
			digest, err = resolver.head(ctx, url, "Bearer "+token)
		}
	}
	if err != nil {
		return domain.Pin{}, err
	}
	if !digestHeader.MatchString(digest) {
		return domain.Pin{}, fmt.Errorf(
			"%s answered %q for %s, which is not an exact sha256 coordinate", host, digest, selector)
	}
	return domain.Pin{Repository: selector.Repository, Digest: digest}, nil
}

// authChallenge is a registry's 401 telling us to offer Basic credentials or
// where to exchange them for a Bearer token.
type authChallenge struct {
	realm   string
	service string
	basic   bool
}

func (challenge *authChallenge) Error() string {
	if challenge.basic {
		return "registry requires Basic authentication"
	}
	return "registry requires a token from " + challenge.realm
}

func asChallenge(err error, target **authChallenge) bool {
	if err == nil {
		return false
	}
	challenge, ok := err.(*authChallenge)
	if ok {
		*target = challenge
	}
	return ok
}

func (resolver *Resolver) head(ctx context.Context, url, authorization string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", acceptable)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := resolver.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("ask %s for a digest: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		if challenge := parseChallenge(response.Header.Get("WWW-Authenticate")); challenge != nil && authorization == "" {
			return "", challenge
		}
		return "", fmt.Errorf("%s refused the credentials offered for this registry", url)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered HTTP %d", url, response.StatusCode)
	}
	return response.Header.Get("Docker-Content-Digest"), nil
}

func parseChallenge(header string) *authChallenge {
	header = strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(header), "basic") {
		return &authChallenge{basic: true}
	}
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return nil
	}
	challenge := &authChallenge{}
	for _, part := range strings.Split(header[len("Bearer "):], ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "realm":
			challenge.realm = value
		case "service":
			challenge.service = value
		}
	}
	if challenge.realm == "" {
		return nil
	}
	return challenge
}

func (resolver *Resolver) token(ctx context.Context, host, name string, challenge *authChallenge) (string, error) {
	url := fmt.Sprintf("%s?scope=repository:%s:pull", challenge.realm, name)
	if challenge.service != "" {
		url += "&service=" + challenge.service
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	username, password, err := resolver.credentials.Basic(ctx, host)
	if err != nil {
		return "", fmt.Errorf("credentials for %s: %w", host, err)
	}
	if username != "" || password != "" {
		request.SetBasicAuth(username, password)
	}
	response, err := resolver.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("get a pull token for %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint for %s answered HTTP %d", name, response.StatusCode)
	}
	var issued struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&issued); err != nil {
		return "", fmt.Errorf("token endpoint for %s did not return a token: %w", name, err)
	}
	if issued.Token != "" {
		return issued.Token, nil
	}
	if issued.AccessToken == "" {
		return "", fmt.Errorf("token endpoint for %s returned an empty token", name)
	}
	return issued.AccessToken, nil
}

func split(repository string) (host, name string, err error) {
	host, name, found := strings.Cut(repository, "/")
	if !found || !strings.Contains(host, ".") {
		return "", "", fmt.Errorf(
			"%q is not a registry-qualified repository; it must start with a registry host such as ghcr.io",
			repository)
	}
	return host, name, nil
}
