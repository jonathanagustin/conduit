package modelhttp

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testKey    = "sk-test-0123456789-do-not-leak"
	testPrompt = "PROMPT-BODY-do-not-leak"
)

type seen struct {
	method, path, authorization, contentType string
	body                                     chatRequest
}

func completionJSON(model, content string) string {
	raw, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "model": model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}}},
	})
	return string(raw)
}

func fake(t *testing.T, got *seen, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		record(got, request)
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func record(got *seen, request *http.Request) {
	if got == nil {
		return
	}
	got.method, got.path = request.Method, request.URL.Path
	got.authorization, got.contentType = request.Header.Get("Authorization"), request.Header.Get("Content-Type")
	_ = json.NewDecoder(request.Body).Decode(&got.body)
}

func keyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustNew(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func noLeak(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if text := err.Error(); strings.Contains(text, testKey) || strings.Contains(text, testPrompt) {
		t.Fatalf("error leaks the key or the prompt: %q", text)
	}
}

func TestCompleteSendsOneChatCompletionAndReportsTheResponseModel(t *testing.T) {
	var got seen
	server := fake(t, &got, http.StatusOK, completionJSON("served-model-v2", "hello back"))
	client := mustNew(t, Config{BaseURL: server.URL + "/v1/", Model: "asked-model"})
	if client.ReportedModel() != "" {
		t.Fatal("reported a model before any response")
	}
	completion, err := client.Complete(context.Background(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Text != "hello back" || completion.Model != "served-model-v2" {
		t.Fatalf("completion = %+v", completion)
	}
	if client.ReportedModel() != "served-model-v2" || client.RequestedModel() != "asked-model" {
		t.Fatalf("reported %q requested %q", client.ReportedModel(), client.RequestedModel())
	}
	if got.method != http.MethodPost || got.path != "/v1/chat/completions" || got.contentType != "application/json" {
		t.Fatalf("request = %s %s %s", got.method, got.path, got.contentType)
	}
	if got.body.Model != "asked-model" || got.body.Stream || len(got.body.Messages) != 1 ||
		got.body.Messages[0].Role != "user" || got.body.Messages[0].Content != testPrompt {
		t.Fatalf("body = %+v", got.body)
	}
	if got.authorization != "" {
		t.Fatalf("keyless client sent Authorization %q", got.authorization)
	}
}

func TestCompleteReadsTheKeyFromTheFileOnEveryCall(t *testing.T) {
	var got seen
	server := fake(t, &got, http.StatusOK, completionJSON("m", "ok"))
	path := keyFile(t, testKey+"\n")
	client := mustNew(t, Config{BaseURL: server.URL, Model: "m", APIKeyFile: path})
	if _, err := client.Complete(context.Background(), testPrompt); err != nil {
		t.Fatal(err)
	}
	if got.authorization != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", got.authorization)
	}
	if err := os.WriteFile(path, []byte("rotated-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), testPrompt); err != nil {
		t.Fatal(err)
	}
	if got.authorization != "Bearer rotated-key" {
		t.Fatalf("Authorization after rotation = %q", got.authorization)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err := client.Complete(context.Background(), testPrompt)
	if !errors.Is(err, ErrKeyFile) {
		t.Fatalf("missing key file: %v", err)
	}
}

func TestNewRefusesAnUnusableKeyFile(t *testing.T) {
	for name, content := range map[string]string{"empty": " \n", "newline": "a\nb", "space": "a b"} {
		_, err := New(Config{BaseURL: "https://model.example/v1", Model: "m", APIKeyFile: keyFile(t, content)})
		if !errors.Is(err, ErrKeyFile) {
			t.Fatalf("%s: %v", name, err)
		}
		if err != nil && content != " \n" && strings.Contains(err.Error(), content) {
			t.Fatalf("%s: error holds the key content", name)
		}
	}
	if _, err := New(Config{BaseURL: "https://model.example/v1", Model: "m", APIKeyFile: filepath.Join(t.TempDir(), "absent")}); !errors.Is(err, ErrKeyFile) {
		t.Fatalf("absent: %v", err)
	}
}

func TestNewRequiresHTTPSUnlessLoopbackOrOptedIn(t *testing.T) {
	for _, base := range []string{
		"http://model.example/v1", "http://10.0.0.7:8000/v1", "http://vllm.models.svc.cluster.local:8000/v1",
		"http://localhost.example/v1", "http://127.0.0.1.example/v1", "HTTP://MODEL.EXAMPLE/v1",
	} {
		if _, err := New(Config{BaseURL: base, Model: "m"}); !errors.Is(err, ErrInsecureEndpoint) {
			t.Fatalf("%s: %v", base, err)
		}
		if _, err := New(Config{BaseURL: base, Model: "m", AllowInsecureHTTP: true}); err != nil {
			t.Fatalf("%s with opt-in: %v", base, err)
		}
	}
	for _, base := range []string{
		"https://model.example/v1", "http://127.0.0.1:11434/v1", "http://localhost:11434/v1", "http://[::1]:8000/v1",
	} {
		if _, err := New(Config{BaseURL: base, Model: "m"}); err != nil {
			t.Fatalf("%s: %v", base, err)
		}
	}
}

func TestNewRefusesInvalidConfiguration(t *testing.T) {
	if _, err := New(Config{Model: "m"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty base: %v", err)
	}
	for _, base := range []string{
		"model.example/v1", "ftp://model.example/v1", "https://user:pass@model.example/v1",
		"https://model.example/v1?key=x", "https://model.example/v1#f", "https:///v1",
	} {
		_, err := New(Config{BaseURL: base, Model: "m", AllowInsecureHTTP: true})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s: %v", base, err)
		}
		if strings.Contains(err.Error(), "pass") {
			t.Fatalf("%s: error repeats URL credentials: %v", base, err)
		}
	}
	if _, err := New(Config{BaseURL: "https://model.example/v1"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := New(Config{BaseURL: "https://model.example/v1", Model: "m", CAFile: keyFile(t, "not pem")}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad CA: %v", err)
	}
}

func TestCompleteTrustsTheExtraRootOnly(t *testing.T) {
	var got seen
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		record(&got, request)
		_, _ = io.WriteString(writer, completionJSON("tls-model", "over tls"))
	}))
	t.Cleanup(server.Close)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, block, 0o600); err != nil {
		t.Fatal(err)
	}

	untrusting := mustNew(t, Config{BaseURL: server.URL, Model: "m", APIKeyFile: keyFile(t, testKey)})
	_, err := untrusting.Complete(context.Background(), testPrompt)
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("without the CA file: %v", err)
	}
	noLeak(t, err)
	if got.authorization != "" {
		t.Fatal("the key reached a server whose certificate was not trusted")
	}

	trusting := mustNew(t, Config{BaseURL: server.URL, Model: "m", APIKeyFile: keyFile(t, testKey), CAFile: caPath})
	completion, err := trusting.Complete(context.Background(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Text != "over tls" || got.authorization != "Bearer "+testKey {
		t.Fatalf("completion %+v authorization %q", completion, got.authorization)
	}
}

func TestCompleteClassifiesStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"401", http.StatusUnauthorized, ErrUnauthorized},
		{"403", http.StatusForbidden, ErrUnauthorized},
		{"500", http.StatusInternalServerError, ErrUpstream},
		{"503", http.StatusServiceUnavailable, ErrUpstream},
		{"404", http.StatusNotFound, ErrStatus},
		{"400", http.StatusBadRequest, ErrStatus},
	}
	all := []error{ErrUnauthorized, ErrUpstream, ErrStatus, ErrRateLimited, ErrMalformed, ErrTimeout, ErrResponseTooLarge}
	for _, tc := range cases {
		// The error body echoes the prompt and key, as some servers do.
		server := fake(t, nil, tc.status, `{"error":"bad `+testPrompt+` `+testKey+`"}`)
		client := mustNew(t, Config{BaseURL: server.URL, Model: "m", APIKeyFile: keyFile(t, testKey)})
		_, err := client.Complete(context.Background(), testPrompt)
		for _, candidate := range all {
			if errors.Is(err, candidate) != (candidate == tc.want) {
				t.Fatalf("%s: errors.Is(%v, %v) is wrong", tc.name, err, candidate)
			}
		}
		noLeak(t, err)
	}
}

func TestCompleteSurfacesRetryAfterOn429(t *testing.T) {
	for header, want := range map[string]time.Duration{"17": 17 * time.Second, "": 0, "soon": 0, "-3": 0} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if header != "" {
				writer.Header().Set("Retry-After", header)
			}
			writer.WriteHeader(http.StatusTooManyRequests)
		}))
		client := mustNew(t, Config{BaseURL: server.URL, Model: "m"})
		_, err := client.Complete(context.Background(), testPrompt)
		server.Close()
		var limited *RateLimitError
		if !errors.As(err, &limited) || !errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUpstream) {
			t.Fatalf("Retry-After %q: %v", header, err)
		}
		if limited.RetryAfter != want {
			t.Fatalf("Retry-After %q: got %s want %s", header, limited.RetryAfter, want)
		}
		if want > 0 && !strings.Contains(err.Error(), "17s") {
			t.Fatalf("the delay is not in the message: %q", err.Error())
		}
	}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if got := retryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Fatalf("HTTP date: %s", got)
	}
	if got := retryAfter(now.Add(-time.Minute).Format(http.TimeFormat), now); got != 0 {
		t.Fatalf("past HTTP date: %s", got)
	}
}

func TestCompleteClassifiesMalformedBodies(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     "<html>gateway</html>",
		"no choices":   `{"model":"m","choices":[]}`,
		"null content": `{"model":"m","choices":[{"message":{"content":null}}]}`,
		"wrong type":   `{"model":"m","choices":[{"message":{"content":["parts"]}}]}`,
		"empty":        "",
	} {
		server := fake(t, nil, http.StatusOK, body)
		client := mustNew(t, Config{BaseURL: server.URL, Model: "m"})
		if _, err := client.Complete(context.Background(), testPrompt); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: %v", name, err)
		}
		if client.ReportedModel() != "" {
			t.Fatalf("%s: a malformed response set the reported model", name)
		}
	}
}

func TestCompleteBoundsTheResponseBody(t *testing.T) {
	exact := completionJSON("m", "ok")
	server := fake(t, nil, http.StatusOK, exact)
	fits := mustNew(t, Config{BaseURL: server.URL, Model: "m", MaxResponseBytes: int64(len(exact))})
	if _, err := fits.Complete(context.Background(), testPrompt); err != nil {
		t.Fatalf("a body exactly at the bound: %v", err)
	}
	// Valid JSON past the bound: only the bound can refuse it.
	big := fake(t, nil, http.StatusOK, completionJSON("m", strings.Repeat("x", 4096)))
	bounded := mustNew(t, Config{BaseURL: big.URL, Model: "m", MaxResponseBytes: 1024})
	completion, err := bounded.Complete(context.Background(), testPrompt)
	if !errors.Is(err, ErrResponseTooLarge) || errors.Is(err, ErrMalformed) {
		t.Fatalf("oversized body: %v", err)
	}
	if completion.Text != "" {
		t.Fatal("an oversized body still produced text")
	}
}

func TestCompleteTimesOutAndHonoursCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	slow := mustNew(t, Config{BaseURL: server.URL, Model: "m", Timeout: 50 * time.Millisecond})
	started := time.Now()
	_, err := slow.Complete(context.Background(), testPrompt)
	if !errors.Is(err, ErrTimeout) || errors.Is(err, ErrCanceled) {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("the timeout did not bound the call")
	}
	noLeak(t, err)

	patient := mustNew(t, Config{BaseURL: server.URL, Model: "m", Timeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = patient.Complete(ctx, testPrompt)
	if !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestCrossOriginRedirectCarriesNeitherTheKeyNorThePrompt(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var hits atomic.Int32
		var leakedKey, leakedPrompt atomic.Bool
		other := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			if request.Header.Get("Authorization") != "" {
				leakedKey.Store(true)
			}
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(string(body), testPrompt) {
				leakedPrompt.Store(true)
			}
			_, _ = io.WriteString(writer, completionJSON("other", "from the other host"))
		}))
		first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, other.URL+"/chat/completions", status)
		}))
		client := mustNew(t, Config{BaseURL: first.URL, Model: "m", APIKeyFile: keyFile(t, testKey)})
		completion, err := client.Complete(context.Background(), testPrompt)
		first.Close()
		other.Close()
		if !errors.Is(err, ErrRedirectRefused) {
			t.Fatalf("%d: %v", status, err)
		}
		if completion.Text != "" || hits.Load() != 0 || leakedKey.Load() || leakedPrompt.Load() {
			t.Fatalf("%d: other host saw %d requests, key leaked %v, prompt leaked %v",
				status, hits.Load(), leakedKey.Load(), leakedPrompt.Load())
		}
		noLeak(t, err)
	}
}

func TestSameOriginRedirectIsFollowed(t *testing.T) {
	var got seen
	mux := http.NewServeMux()
	mux.HandleFunc("/old/chat/completions", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/new/chat/completions", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/new/chat/completions", func(writer http.ResponseWriter, request *http.Request) {
		record(&got, request)
		_, _ = io.WriteString(writer, completionJSON("m", "moved"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := mustNew(t, Config{BaseURL: server.URL + "/old", Model: "m", APIKeyFile: keyFile(t, testKey)})
	completion, err := client.Complete(context.Background(), testPrompt)
	if err != nil || completion.Text != "moved" {
		t.Fatalf("completion %+v err %v", completion, err)
	}
	if got.authorization != "Bearer "+testKey || got.body.Messages[0].Content != testPrompt {
		t.Fatal("the same-origin redirect lost the request")
	}
}

func TestRedirectPolicyTreatsSubdomainPortAndSchemeAsAnotherOrigin(t *testing.T) {
	original := &http.Request{URL: mustURL(t, "https://model.example/v1/chat/completions"), Header: http.Header{}}
	for target, allowed := range map[string]bool{
		"https://model.example/v2/chat/completions":      true,
		"https://MODEL.example/v2/chat/completions":      true,
		"https://evil.model.example/v1/chat/completions": false,
		"https://model.example:8443/v1/chat/completions": false,
		"http://model.example/v1/chat/completions":       false,
		"https://attacker.example/v1/chat/completions":   false,
	} {
		next := &http.Request{URL: mustURL(t, target), Header: http.Header{"Authorization": {"Bearer " + testKey}}}
		err := checkRedirect(next, []*http.Request{original})
		if allowed != (err == nil) {
			t.Fatalf("%s: err %v", target, err)
		}
		if !allowed && (!errors.Is(err, ErrRedirectRefused) || next.Header.Get("Authorization") != "") {
			t.Fatalf("%s: refused without clearing the credential: %v", target, err)
		}
	}
	loop := make([]*http.Request, redirectLimit)
	for index := range loop {
		loop[index] = original
	}
	if err := checkRedirect(original, loop); !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("redirect loop: %v", err)
	}
}

func TestNilClientIsNotConfigured(t *testing.T) {
	var client *Client
	if _, err := client.Complete(context.Background(), testPrompt); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client: %v", err)
	}
	if client.ReportedModel() != "" || client.RequestedModel() != "" {
		t.Fatal("nil client reported a model")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestUsageIsReportedOnlyWhenTheProviderSendsIt(t *testing.T) {
	with := `{"model":"m","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`
	client, err := New(Config{BaseURL: fake(t, nil, http.StatusOK, with).URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := client.Complete(context.Background(), testPrompt)
	if err != nil || completion.PromptTokens == nil || *completion.PromptTokens != 12 ||
		completion.CompletionTokens == nil || *completion.CompletionTokens != 3 {
		t.Fatalf("completion = %+v, err = %v", completion, err)
	}
	for name, body := range map[string]string{
		"no usage object":  completionJSON("m", "a reply long enough to tempt an estimate"),
		"an empty usage":   `{"model":"m","choices":[{"message":{"content":"hi"}}],"usage":{}}`,
		"a null usage":     `{"model":"m","choices":[{"message":{"content":"hi"}}],"usage":null}`,
		"a negative count": `{"model":"m","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":-1,"completion_tokens":-5}}`,
	} {
		client, err := New(Config{BaseURL: fake(t, nil, http.StatusOK, body).URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		completion, err := client.Complete(context.Background(), testPrompt)
		if err != nil || completion.PromptTokens != nil || completion.CompletionTokens != nil {
			t.Errorf("%s: completion = %+v, err = %v: token counts must be nil", name, completion, err)
		}
	}
}

func TestAMiddlewareSeesTheExactPromptAndMayRetryOrRefuse(t *testing.T) {
	var got seen
	base, err := New(Config{BaseURL: fake(t, &got, http.StatusOK, completionJSON("served", "hi")).URL, Model: "asked"})
	if err != nil {
		t.Fatal(err)
	}
	var sawModel, sawPrompt string
	calls := 0
	twice := base.With(func(ctx context.Context, model, prompt string, next func(context.Context) (Completion, error)) (Completion, error) {
		sawModel, sawPrompt = model, prompt
		calls++
		if _, err := next(ctx); err != nil {
			return Completion{}, err
		}
		return next(ctx)
	})
	completion, err := twice.Complete(context.Background(), testPrompt)
	if err != nil || completion.Text != "hi" || calls != 1 || sawModel != "asked" || sawPrompt != testPrompt || got.body.Messages[0].Content != testPrompt {
		t.Fatalf("completion = %+v, err = %v, saw %q %q", completion, err, sawModel, sawPrompt)
	}
	// The copy and the original report the same provider model.
	if base.ReportedModel() != "served" || twice.ReportedModel() != "served" {
		t.Fatalf("reported = %q / %q", base.ReportedModel(), twice.ReportedModel())
	}
	refusal := errors.New("refused")
	got = seen{}
	refusing := base.With(func(context.Context, string, string, func(context.Context) (Completion, error)) (Completion, error) {
		return Completion{}, refusal
	})
	if _, err := refusing.Complete(context.Background(), testPrompt); !errors.Is(err, refusal) || got.method != "" {
		t.Fatalf("a refused call: err = %v, request sent = %q", err, got.method)
	}
	if base.With(nil) != base {
		t.Fatal("a nil middleware did not return the receiver")
	}
}
