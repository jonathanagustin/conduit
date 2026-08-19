package multipasshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// There is no Multipass to talk to here, and this repository's sandbox permits
// outbound TCP on 80 and 443 only. Every assertion below is against an
// httptest server in this process. That establishes that this client sends the
// routes, parameters and body shapes the recovered contract states and decodes
// what that contract says comes back. It establishes NOTHING about a live
// Multipass deployment's behaviour.

// recorder captures what the client sent so the route can be asserted on
// rather than assumed.
type recorder struct {
	method      string
	path        string
	rawQuery    string
	accept      string
	contentType string
	authorized  string
	body        []byte
}

func serve(t *testing.T, status int, response string, into *recorder) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		*into = recorder{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			accept: request.Header.Get("Accept"), contentType: request.Header.Get("Content-Type"),
			authorized: request.Header.Get("Authorization"), body: body,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUserReadsTheRecoveredRouteAndDecodesAttributes(t *testing.T) {
	t.Parallel()
	var sent recorder
	// The response is the recovered wire shape: three fields, everything else
	// hanging off attributes. A client that modelled givenName as a field would
	// decode this into a user with no name and report no error.
	server := serve(t, http.StatusOK, `{
		"id": "seed-user-current",
		"username": "operator",
		"attributes": {
			"multipass:given-name": ["Ops"],
			"multipass:family-name": ["Operator"],
			"multipass:email:primary": ["operator@example.test"],
			"multipass:active": ["true"]
		}
	}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()), WithBearerToken("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := directory.User(context.Background(), "seed-user-current")
	if err != nil {
		t.Fatal(err)
	}
	if sent.method != http.MethodGet || sent.path != "/multipass/api/users/seed-user-current" {
		t.Fatalf("sent %s %s", sent.method, sent.path)
	}
	if sent.accept != "application/json" {
		t.Errorf("Accept = %q", sent.accept)
	}
	if sent.authorized != "Bearer secret-token" {
		t.Errorf("Authorization = %q", sent.authorized)
	}
	if user.ID != "seed-user-current" || user.Username != "operator" {
		t.Fatalf("user = %#v", user)
	}
	if user.FullName() != "Ops Operator" || user.EmailPrimary() != "operator@example.test" || !user.Active() {
		t.Fatalf("derived values wrong: %q %q %v", user.FullName(), user.EmailPrimary(), user.Active())
	}
}

// A user id is a path segment and may contain characters that would otherwise
// change the route. An unescaped id is a request to a different endpoint.
func TestUserEscapesTheIDIntoOnePathSegment(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK, `{"id":"a/b","username":"x","attributes":{}}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.User(context.Background(), "a/b?c"); err != nil {
		t.Fatal(err)
	}
	if sent.path != "/multipass/api/users/a/b?c" {
		// http.Request.URL.Path is already unescaped, so the assertion is that
		// the server saw one segment carrying the whole id rather than two.
		t.Fatalf("path = %q", sent.path)
	}
	if sent.rawQuery != "" {
		t.Fatalf("the id leaked into the query string: %q", sent.rawQuery)
	}
}

func TestUsersByIDsPostsTheBareArray(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK,
		`[{"id":"a","username":"alice","attributes":{}},{"id":"b","username":"bob","attributes":{}}]`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	users, err := directory.UsersByIDs(context.Background(), []string{"a", " b ", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if sent.method != http.MethodPost || sent.path != "/multipass/api/users" {
		t.Fatalf("sent %s %s", sent.method, sent.path)
	}
	if sent.contentType != "application/json" {
		t.Errorf("Content-Type = %q", sent.contentType)
	}
	// The recovered body is the bare set of ids, not an object wrapping them.
	// Decoding into a slice is what fails if that ever becomes a wrapper.
	var decoded []string
	if err := json.Unmarshal(sent.body, &decoded); err != nil {
		t.Fatalf("body was not a bare array: %s", sent.body)
	}
	if strings.Join(decoded, ",") != "a,b" {
		t.Fatalf("body = %v, want the trimmed non-empty ids", decoded)
	}
	if len(users) != 2 {
		t.Fatalf("users = %#v", users)
	}
}

// An empty batch is answered without a request. One round trip to be told
// nothing is worse than none, and the recovered contract does not say what the
// server does with an empty array.
func TestUsersByIDsWithNoIDsSendsNoRequest(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK, `[]`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	users, err := directory.UsersByIDs(context.Background(), []string{"  ", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %#v", users)
	}
	if sent.method != "" {
		t.Fatalf("a request was sent for an empty batch: %s %s", sent.method, sent.path)
	}
}

func TestGroupsForUserSendsOnlyTheCursorsThatWereAskedFor(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		page      domain.MultipassGroupPage
		wantQuery string
	}{
		{name: "no cursor at all leaves the server's defaults in force", wantQuery: ""},
		{name: "size only", page: domain.MultipassGroupPage{Size: 25}, wantQuery: "size=25"},
		{name: "offset only", page: domain.MultipassGroupPage{From: 50}, wantQuery: "from=50"},
		{
			name:      "hidden groups are asked for only when wanted",
			page:      domain.MultipassGroupPage{From: 50, Size: 25, IncludeHidden: true},
			wantQuery: "from=50&includeHiddenGroups=true&size=25",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var sent recorder
			server := serve(t, http.StatusOK, `{"groups":[],"orderedBy":"NAME"}`, &sent)
			directory, err := New(server.URL, WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := directory.GroupsForUser(context.Background(), "u", testCase.page); err != nil {
				t.Fatal(err)
			}
			if sent.path != "/multipass/api/users/u/groups/v3" {
				t.Fatalf("path = %q; the deprecated route has no /v3 suffix", sent.path)
			}
			if sent.rawQuery != testCase.wantQuery {
				t.Fatalf("query = %q, want %q", sent.rawQuery, testCase.wantQuery)
			}
		})
	}
}

func TestGroupsForUserDecodesThePageEnvelope(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK, `{
		"groups": [{"id":"g1","name":"Users","attributes":{"multipass:description":["All platform users"]}}],
		"next": "1",
		"orderedBy": "NAME"
	}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	page, err := directory.GroupsForUser(context.Background(), "u", domain.MultipassGroupPage{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Groups) != 1 || page.Groups[0].Name != "Users" {
		t.Fatalf("groups = %#v", page.Groups)
	}
	if page.Groups[0].Description() != "All platform users" {
		t.Errorf("description = %q", page.Groups[0].Description())
	}
	if page.Next != "1" || page.OrderedBy != "NAME" {
		t.Errorf("next=%q orderedBy=%q", page.Next, page.OrderedBy)
	}
}

// Absence and unreachability are different answers and are kept different. The
// recovered error NAME is what makes it absence; a bare status is not enough,
// because a misrouted base URL also answers 404.
func TestNotFoundRequiresTheRecoveredErrorName(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name        string
		status      int
		response    string
		wantAbsence bool
	}{
		{
			name:   "the recovered user-not-found envelope",
			status: http.StatusNotFound,
			response: `{"errorCode":"NOT_FOUND","errorName":"Multipass:UserNotFound",` +
				`"errorInstanceId":"x","parameters":{"userId":"ghost"}}`,
			wantAbsence: true,
		},
		{
			name:   "the recovered group-not-found envelope",
			status: http.StatusNotFound,
			response: `{"errorCode":"NOT_FOUND","errorName":"Multipass:GroupNotFound",` +
				`"errorInstanceId":"x","parameters":{"groupId":"ghost"}}`,
			wantAbsence: true,
		},
		{
			name:     "a bare 404 from a misrouted base URL is not absence",
			status:   http.StatusNotFound,
			response: `not found`,
		},
		{
			name:     "a 404 carrying some other error name is not absence",
			status:   http.StatusNotFound,
			response: `{"errorName":"Conjure:NotFound"}`,
		},
		{name: "a 503 is not absence", status: http.StatusServiceUnavailable, response: `{}`},
		{name: "a 401 is not absence", status: http.StatusUnauthorized, response: `{}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var sent recorder
			server := serve(t, testCase.status, testCase.response, &sent)
			directory, err := New(server.URL, WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = directory.User(context.Background(), "ghost")
			if err == nil {
				t.Fatal("a non-2xx was reported as success")
			}
			if got := errors.Is(err, domain.ErrMultipassPrincipalNotFound); got != testCase.wantAbsence {
				t.Fatalf("absence = %v, want %v (err %v)", got, testCase.wantAbsence, err)
			}
		})
	}
}

// A failure names the route and the status and nothing else. Response bodies
// carry principal attributes, and an error string is the least controlled
// place in the system for them to end up.
func TestFailuresDoNotEchoTheResponseBody(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusInternalServerError,
		`{"errorName":"Multipass:Internal","parameters":{"email":"leak@example.test"}}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = directory.User(context.Background(), "u")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "leak@example.test") {
		t.Fatalf("the response body reached the error string: %v", err)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "/multipass/api/users/u") {
		t.Fatalf("the error names neither the status nor the route: %v", err)
	}
}

func TestNewRefusesAnUnusableBaseURL(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{"", "   ", "multipass.example.test", "ftp://multipass.example.test", "https://"} {
		if _, err := New(baseURL); err == nil {
			t.Errorf("base URL %q was accepted", baseURL)
		}
	}
}

func TestEmptyIdentifiersAreRefusedWithoutARequest(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK, `{}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.User(context.Background(), "  "); err == nil {
		t.Error("an empty user id was accepted")
	}
	if _, err := directory.GroupsForUser(context.Background(), "", domain.MultipassGroupPage{}); err == nil {
		t.Error("an empty user id was accepted for a group read")
	}
	if sent.method != "" {
		t.Fatalf("a request was sent for an empty id: %s %s", sent.method, sent.path)
	}
}

// The token is held in a closure so that printing the adapter cannot print it.
// Adapters end up in error strings and log lines in this codebase, and a
// credential that can be printed eventually is.
func TestTheBearerTokenIsNotReachableFromAFormattedDirectory(t *testing.T) {
	t.Parallel()
	directory, err := New("https://multipass.example.test", WithBearerToken("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	rendered := fmt.Sprintf("%#v %+v %p", directory, *directory, directory.authorizer)
	if strings.Contains(rendered, "secret-token") {
		t.Fatalf("the token is printable: %s", rendered)
	}
	// ...and it is still actually sent, so the hiding is not achieved by
	// dropping it.
	var sent recorder
	server := serve(t, http.StatusOK, `{"id":"u","username":"u","attributes":{}}`, &sent)
	authenticated, err := New(server.URL, WithHTTPClient(server.Client()), WithBearerToken("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authenticated.User(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if sent.authorized != "Bearer secret-token" {
		t.Fatalf("Authorization = %q", sent.authorized)
	}
}

// A blank token installs no authorizer at all, rather than an "Authorization:
// Bearer " header that a server would read as a malformed credential.
func TestABlankBearerTokenSendsNoAuthorizationHeader(t *testing.T) {
	t.Parallel()
	var sent recorder
	server := serve(t, http.StatusOK, `{"id":"u","username":"u","attributes":{}}`, &sent)
	directory, err := New(server.URL, WithHTTPClient(server.Client()), WithBearerToken("   "))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.User(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if sent.authorized != "" {
		t.Fatalf("Authorization = %q, want none", sent.authorized)
	}
}
