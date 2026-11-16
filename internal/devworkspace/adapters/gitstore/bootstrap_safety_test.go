package gitstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func bootstrapTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := runGit(t.Context(), dir, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	bootstrapTestCommit(t, dir, "initial")
	return dir
}
func bootstrapTestCommit(t *testing.T, dir, text string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), dir, "-c", "user.name=fixture", "-c", "user.email=fixture@local", "-c", "commit.gpgsign=false", "commit", "-m", "chore(test): record source"); err != nil {
		t.Fatal(err)
	}
	head, err := gitOut(t.Context(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(head)
}

func TestBootstrapPushKeepsDivergentRemoteAndDoesNotFallBack(t *testing.T) {
	source := bootstrapTestRepo(t)
	remote := t.TempDir()
	if err := runGit(t.Context(), remote, "init", "--bare", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), source, "remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if err := pushGitRef(t.Context(), source, "", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	bootstrapTestCommit(t, source, "fast forward")
	if err := pushGitRef(t.Context(), source, "", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	competitor := filepath.Join(t.TempDir(), "competitor")
	if err := runGit(t.Context(), "", "clone", remote, competitor); err != nil {
		t.Fatal(err)
	}
	competingHead := bootstrapTestCommit(t, competitor, "other writer")
	if err := pushGitRef(t.Context(), competitor, "", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	localHead := bootstrapTestCommit(t, source, "local divergent work")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(404) }))
	defer server.Close()
	err := pushBranchToGitHubOrOrigin(t.Context(), source, "main", server.URL, "fixture-token", "owner", "repo")
	if !errors.Is(err, domain.ErrConflict) || calls != 0 {
		t.Fatalf("rejection=%v API calls=%d", err, calls)
	}
	observed, _ := gitOut(t.Context(), remote, "rev-parse", "refs/heads/main")
	if strings.TrimSpace(observed) != competingHead {
		t.Fatal("competing work was overwritten")
	}
	observed, _ = gitOut(t.Context(), source, "rev-parse", "HEAD")
	if strings.TrimSpace(observed) != localHead {
		t.Fatal("local work was lost")
	}
}

func TestBootstrapGitAuthenticationFailureDoesNotFallBackOrExposeToken(t *testing.T) {
	source := bootstrapTestRepo(t)
	apiCalls := 0
	credentialSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info/refs") {
			credentialSeen = r.Header.Get("Authorization") != ""
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(401)
			return
		}
		apiCalls++
		w.WriteHeader(404)
	}))
	defer server.Close()
	if err := runGit(t.Context(), source, "remote", "add", "origin", server.URL+"/repo.git"); err != nil {
		t.Fatal(err)
	}
	err := pushBranchToGitHubOrOrigin(t.Context(), source, "main", server.URL, "never-print-this-token", "owner", "repo")
	if !errors.Is(err, domain.ErrForbidden) || apiCalls != 0 || !credentialSeen {
		t.Fatalf("auth failure=%v API calls=%d credential seen=%v", err, apiCalls, credentialSeen)
	}
	if strings.Contains(err.Error(), "never-print-this-token") {
		t.Fatal("token exposed in error")
	}
}

type bootstrapAPIFixture struct {
	malformedCreatedRef bool
	lookupStatus        int
	lookupBody          any
	createStatus        int
	parentStatus        int
	cancel              context.CancelFunc
	writes              []string
	methods             []string
	head                string
	treeModes           map[string]string
	message             string
	parents             []string
}

func (f *bootstrapAPIFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.methods = append(f.methods, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		status, body := f.lookupStatus, f.lookupBody
		if strings.HasSuffix(r.URL.Path, "/heads/main") && f.parentStatus != 0 {
			status = f.parentStatus
			body = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": strings.Repeat("e", 40)}}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	if r.Method == http.MethodPatch {
		w.WriteHeader(500)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/blobs"):
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": strings.Repeat("b", 40)})
	case strings.HasSuffix(r.URL.Path, "/trees"):
		var payload struct{ Tree []struct{ Path, Mode string } }
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.treeModes = map[string]string{}
		for _, entry := range payload.Tree {
			f.treeModes[entry.Path] = entry.Mode
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": strings.Repeat("c", 40)})
	case strings.HasSuffix(r.URL.Path, "/commits"):
		var payload struct {
			Message string
			Parents []string
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.message, f.parents = payload.Message, payload.Parents
		if f.cancel != nil {
			f.cancel()
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": strings.Repeat("d", 40)})
	case strings.HasSuffix(r.URL.Path, "/refs"):
		if f.createStatus != 0 {
			f.head = "competing head"
			w.WriteHeader(f.createStatus)
			return
		}
		var payload struct{ Ref, SHA string }
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.head = payload.SHA
		if f.malformedCreatedRef {
			payload.Ref = "refs/heads/foreign"
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": payload.Ref, "object": map[string]string{"sha": payload.SHA, "type": "commit"}})
	default:
		w.WriteHeader(500)
	}
}

func TestBootstrapAPIFallbackCreatesOnlyMissingBranches(t *testing.T) {
	for _, emptyStatus := range []int{404, 409} {
		t.Run(fmt.Sprint(emptyStatus), func(t *testing.T) {
			source := bootstrapTestRepo(t)
			if err := os.WriteFile(filepath.Join(source, "run.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("README.md", filepath.Join(source, "link")); err != nil {
				t.Fatal(err)
			}
			bootstrapTestCommit(t, source, "snapshot")
			fixture := &bootstrapAPIFixture{lookupStatus: emptyStatus, lookupBody: map[string]string{"message": "Git Repository is empty."}}
			server := httptest.NewServer(http.HandlerFunc(fixture.serve))
			defer server.Close()
			if err := syncBranchViaGitHubDataAPI(t.Context(), source, "main", server.URL, "fixture-token", "owner", "repo"); err != nil {
				t.Fatal(err)
			}
			if fixture.head != strings.Repeat("d", 40) || fixture.message != "chore(workspace): bootstrap source snapshot" || len(fixture.parents) != 0 {
				t.Fatalf("unexpected publication: %+v", fixture)
			}
			if fixture.treeModes["run.sh"] != "100755" || fixture.treeModes["link"] != "120000" {
				t.Fatalf("modes lost: %+v", fixture.treeModes)
			}
			for _, method := range fixture.methods {
				if strings.HasPrefix(method, "PATCH ") {
					t.Fatal("updated an existing ref")
				}
			}
		})
	}
}

func TestBootstrapAPIFallbackRefusesUnprovenAbsenceBeforeWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   any
	}{
		{"existing", 200, map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": strings.Repeat("a", 40), "type": "commit"}}},
		{"wrong ref", 200, map[string]any{"ref": "refs/heads/other", "object": map[string]string{"sha": strings.Repeat("a", 40), "type": "commit"}}},
		{"malformed", 200, map[string]string{"object": "invalid"}},
		{"unauthorized", 401, nil}, {"unavailable", 503, nil}, {"unexpected conflict", 409, map[string]string{"message": "different conflict"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &bootstrapAPIFixture{lookupStatus: test.status, lookupBody: test.body}
			server := httptest.NewServer(http.HandlerFunc(fixture.serve))
			defer server.Close()
			err := syncBranchViaGitHubDataAPI(t.Context(), bootstrapTestRepo(t), "main", server.URL, "fixture-token", "owner", "repo")
			if err == nil || len(fixture.writes) != 0 {
				t.Fatalf("failure=%v writes=%v", err, fixture.writes)
			}
		})
	}
}

func TestBootstrapAPICreateRaceKeepsCompetingRef(t *testing.T) {
	fixture := &bootstrapAPIFixture{lookupStatus: 404, createStatus: 422}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	err := syncBranchViaGitHubDataAPI(t.Context(), bootstrapTestRepo(t), "main", server.URL, "fixture-token", "owner", "repo")
	if !errors.Is(err, domain.ErrConflict) || fixture.head != "competing head" {
		t.Fatalf("race result: %v head=%s", err, fixture.head)
	}
	for _, method := range fixture.methods {
		if strings.HasPrefix(method, "PATCH ") {
			t.Fatal("force-updated competitor")
		}
	}
}

func TestBootstrapAPICancellationDoesNotCreateRef(t *testing.T) {
	for _, when := range []string{"before lookup", "after objects"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := &bootstrapAPIFixture{lookupStatus: 404}
			if when == "before lookup" {
				cancel()
			} else {
				fixture.cancel = cancel
			}
			server := httptest.NewServer(http.HandlerFunc(fixture.serve))
			defer server.Close()
			err := syncBranchViaGitHubDataAPI(ctx, bootstrapTestRepo(t), "main", server.URL, "fixture-token", "owner", "repo")
			if !errors.Is(err, context.Canceled) || fixture.head != "" {
				t.Fatalf("cancellation=%v head=%s", err, fixture.head)
			}
		})
	}
}

func TestBootstrapAPINewWorkspaceRequiresValidParentLookup(t *testing.T) {
	for _, status := range []int{200, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fixture := &bootstrapAPIFixture{lookupStatus: 404, parentStatus: status}
			server := httptest.NewServer(http.HandlerFunc(fixture.serve))
			defer server.Close()
			err := syncBranchViaGitHubDataAPI(t.Context(), bootstrapTestRepo(t), "workspace/owner/one", server.URL, "fixture-token", "owner", "repo")
			if status == 503 {
				if err == nil || len(fixture.writes) != 0 {
					t.Fatalf("parent failure=%v writes=%v", err, fixture.writes)
				}
				return
			}
			if err != nil || len(fixture.parents) != 1 || fixture.parents[0] != strings.Repeat("e", 40) {
				t.Fatalf("parent=%v err=%v", fixture.parents, err)
			}
		})
	}
}

func TestBootstrapAPIExactExistingHeadIsReadOnly(t *testing.T) {
	source := bootstrapTestRepo(t)
	head, _ := gitOut(t.Context(), source, "rev-parse", "HEAD")
	fixture := &bootstrapAPIFixture{lookupStatus: 200, lookupBody: map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": strings.TrimSpace(head), "type": "commit"}}}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	if err := syncBranchViaGitHubDataAPI(t.Context(), source, "main", server.URL, "fixture-token", "owner", "repo"); err != nil {
		t.Fatal(err)
	}
	if len(fixture.writes) != 0 {
		t.Fatalf("idempotent read mutated GitHub: %v", fixture.writes)
	}
}

func TestBootstrapAPIRejectsUnsupportedTreeBeforeWrites(t *testing.T) {
	source := bootstrapTestRepo(t)
	head, _ := gitOut(t.Context(), source, "rev-parse", "HEAD")
	if err := runGit(t.Context(), source, "update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(head)+",submodule"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), source, "-c", "user.name=fixture", "-c", "user.email=fixture@local", "commit", "-m", "chore(test): record gitlink"); err != nil {
		t.Fatal(err)
	}
	fixture := &bootstrapAPIFixture{lookupStatus: 404}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	err := syncBranchViaGitHubDataAPI(t.Context(), source, "main", server.URL, "fixture-token", "owner", "repo")
	if err == nil || len(fixture.writes) != 0 {
		t.Fatalf("unsupported tree=%v writes=%v", err, fixture.writes)
	}
}

func TestBootstrapGitPushKeepsCredentialsOutOfArguments(t *testing.T) {
	directory := t.TempDir()
	argsPath := filepath.Join(directory, "args")
	configPath := filepath.Join(directory, "config")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CONDUIT_TEST_GIT_ARGS_RECEIPT"
printf '%s\n' "$GIT_CONFIG_COUNT" "$GIT_CONFIG_KEY_0" "$GIT_CONFIG_VALUE_0" > "$CONDUIT_TEST_GIT_CONFIG_RECEIPT"
`
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CONDUIT_TEST_GIT_ARGS_RECEIPT", argsPath)
	t.Setenv("CONDUIT_TEST_GIT_CONFIG_RECEIPT", configPath)
	const token = "fixture-private-token"
	if err := pushGitRef(t.Context(), directory, token, "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "push\n--porcelain\norigin\nHEAD:refs/heads/main\n" {
		t.Fatalf("unexpected push arguments: %s", args)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := "1\nhttp.extraHeader\nAUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)) + "\n"
	if string(config) != expected {
		t.Fatal("Git authentication configuration was not retained")
	}
}

func TestBootstrapAPIRequiresExactCreatedRefReceipt(t *testing.T) {
	fixture := &bootstrapAPIFixture{lookupStatus: 404, malformedCreatedRef: true}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	if err := syncBranchViaGitHubDataAPI(t.Context(), bootstrapTestRepo(t), "main", server.URL, "fixture-token", "owner", "repo"); err == nil {
		t.Fatal("accepted a receipt for another ref")
	}
}
