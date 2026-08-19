// Package multipasshttp reads Multipass over its recovered HTTP routes.
//
// This is one of two adapters behind ports.MultipassDirectory, and it is the
// one that talks to a real service. It knows the route strings, the query
// parameter names, the batch body shape and the error envelope, and it knows
// nothing else: every question about what an attribute MEANS is answered by
// internal/identity/domain, so this adapter and the offline one cannot disagree
// about whether a user is active.
//
// Route provenance. The paths and their parameter names are transcribed from
// the deterministic TypeScript client generated from the recovered legacy
// Conjure contract (`multipass-legacy-api.yml`, itself recovered from
// `com.palantir.multipass:multipass-api:3.2636.0`). Two of the three —
// `GET /multipass/api/users/{userId}` and `POST /multipass/api/users` — are
// additionally corroborated by observed tenant traffic. The third,
// `GET /multipass/api/users/{userId}/groups/v3`, is contract-only. See the
// tenant-fidelity table on the pull request that introduced this package.
//
// It has never been run against a live Multipass. Everything asserted about it
// is asserted against an httptest server in this package's tests, which
// establishes that this client sends what the recovered contract says and
// decodes what that contract says comes back — and establishes nothing at all
// about how a real deployment behaves.
package multipasshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// DefaultTimeout bounds one directory read. An identity lookup with no timeout
// blocks whatever was rendering a username behind it, and a page that resolves
// several of them can hold as many connections as it has subjects.
const DefaultTimeout = 10 * time.Second

// MaximumResponseBytes bounds one decoded response.
//
// A page of groups is kilobytes. The limit is here because this client reads a
// service Conduit does not operate: a response body is attacker-influenced
// input in the sense that matters, and an unbounded json.Decoder over an
// unbounded body is an out-of-memory condition somebody else can trigger.
const MaximumResponseBytes = 8 << 20

// Directory reads users and groups from a Multipass deployment.
type Directory struct {
	baseURL    string
	client     *http.Client
	authorizer func(*http.Request)
}

var _ ports.MultipassDirectory = (*Directory)(nil)

// Option adjusts a Directory at construction.
type Option func(*Directory)

// WithHTTPClient supplies the client requests are made with. A nil client is
// ignored rather than installed, so a caller cannot accidentally remove the
// default timeout by passing one it failed to build.
func WithHTTPClient(client *http.Client) Option {
	return func(directory *Directory) {
		if client != nil {
			directory.client = client
		}
	}
}

// WithBearerToken authenticates every request with a bearer token.
//
// The token is held in a closure rather than in a struct field so that it is
// not reachable from a %#v of the Directory. Adapters in this repository end up
// in error strings and log lines, and a credential that can be printed
// eventually is.
func WithBearerToken(token string) Option {
	return func(directory *Directory) {
		token = strings.TrimSpace(token)
		if token == "" {
			return
		}
		directory.authorizer = func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
		}
	}
}

// New refuses a base URL a request could not be built from.
//
// The base URL is the deployment's root, not the API prefix: this adapter owns
// the `/multipass/api/...` path because those paths are the recovered contract
// and a caller that could supply them could quietly point this client at a
// route with different semantics.
func New(baseURL string, options ...Option) (*Directory, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("a Multipass base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Multipass base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("Multipass base URL must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("Multipass base URL names no host")
	}
	directory := &Directory{baseURL: baseURL, client: &http.Client{Timeout: DefaultTimeout}}
	for _, option := range options {
		option(directory)
	}
	return directory, nil
}

// User reads GET /multipass/api/users/{userId}.
func (directory *Directory) User(ctx context.Context, userID string) (domain.MultipassUser, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.MultipassUser{}, errors.New("a Multipass user id is required")
	}
	var user domain.MultipassUser
	err := directory.call(ctx, http.MethodGet, "/multipass/api/users/"+url.PathEscape(userID), nil, nil, &user)
	return user, err
}

// UsersByIDs reads POST /multipass/api/users.
//
// The request body is the bare array of ids, not an object wrapping them. That
// is the recovered shape and it is the kind of detail a reimplementation gets
// wrong in a way that produces an empty page rather than an error.
//
// An empty id list short-circuits without a request. Posting an empty array
// would be one round trip to be told nothing, and the recovered contract does
// not say what the server does with it.
func (directory *Directory) UsersByIDs(ctx context.Context, userIDs []string) ([]domain.MultipassUser, error) {
	ids := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var users []domain.MultipassUser
	if err := directory.call(ctx, http.MethodPost, "/multipass/api/users", nil, ids, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// GroupsForUser reads GET /multipass/api/users/{userId}/groups/v3.
//
// Each cursor parameter is sent only when it was asked for, so an unset Size
// leaves the server's own default in force rather than this client choosing
// one. IncludeHidden is sent only when true for the same reason: the recovered
// default is false, and restating it would make a later change to that default
// invisible here.
func (directory *Directory) GroupsForUser(
	ctx context.Context, userID string, page domain.MultipassGroupPage,
) (domain.MultipassGroupsPage, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.MultipassGroupsPage{}, errors.New("a Multipass user id is required")
	}
	query := url.Values{}
	if page.From != 0 {
		query.Set("from", strconv.Itoa(page.From))
	}
	if page.Size != 0 {
		query.Set("size", strconv.Itoa(page.Size))
	}
	if page.IncludeHidden {
		query.Set("includeHiddenGroups", "true")
	}
	var response groupsResponse
	err := directory.call(ctx, http.MethodGet,
		"/multipass/api/users/"+url.PathEscape(userID)+"/groups/v3", query, nil, &response)
	if err != nil {
		return domain.MultipassGroupsPage{}, err
	}
	return domain.MultipassGroupsPage{
		Groups: response.Groups, Next: response.Next, OrderedBy: response.OrderedBy,
	}, nil
}

// groupsResponse is the recovered GroupsResponse. `next` and `orderedBy` are
// nullable on the recovered class, which is why they are plain strings decoded
// from a possibly-absent field rather than required ones.
type groupsResponse struct {
	Groups    []domain.MultipassGroup `json:"groups"`
	Next      string                  `json:"next"`
	OrderedBy string                  `json:"orderedBy"`
}

// conjureError is the error envelope the recovered service answers with. Only
// errorName is read: errorInstanceId identifies one occurrence and parameters
// can carry tenant data, and neither belongs in a Conduit error string.
type conjureError struct {
	ErrorName string `json:"errorName"`
}

// The recovered error names for a principal that does not exist.
const (
	errorNameUserNotFound  = "Multipass:UserNotFound"
	errorNameGroupNotFound = "Multipass:GroupNotFound"
)

func (directory *Directory) call(
	ctx context.Context, method, path string, query url.Values, body any, into any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Multipass request for %s: %w", path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	target := directory.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("build Multipass request for %s: %w", path, err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if directory.authorizer != nil {
		directory.authorizer(request)
	}
	response, err := directory.client.Do(request)
	if err != nil {
		return fmt.Errorf("read Multipass %s: %w", path, err)
	}
	defer func() { _ = response.Body.Close() }()
	limited := io.LimitReader(response.Body, MaximumResponseBytes)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return directory.failure(response.StatusCode, method, path, limited)
	}
	if err := json.NewDecoder(limited).Decode(into); err != nil {
		return fmt.Errorf("decode Multipass %s: %w", path, err)
	}
	return nil
}

// failure turns a non-2xx into an error, mapping the recovered not-found names
// onto the port's stable absence sentinel.
//
// The status alone is not enough to do that. A 404 from a misconfigured base
// URL — a reverse proxy with no route for /multipass — is also a 404, and
// reading it as "this user does not exist" would report an entire tenant's
// directory as empty rather than as unreachable. So absence requires the
// recovered error NAME, and a bare 404 stays a transport failure.
//
// The response body is never echoed. It can carry principal attributes, and an
// error string is the least controlled place in the system.
func (directory *Directory) failure(status int, method, path string, body io.Reader) error {
	var envelope conjureError
	if err := json.NewDecoder(body).Decode(&envelope); err == nil {
		switch envelope.ErrorName {
		case errorNameUserNotFound, errorNameGroupNotFound:
			return fmt.Errorf("%w: %s %s", domain.ErrMultipassPrincipalNotFound, method, path)
		}
	}
	return fmt.Errorf("Multipass %s %s: unexpected status %d", method, path, status)
}
