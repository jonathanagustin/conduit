package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProviderReply(t *testing.T) {
	cases := []struct {
		name, in string
		json     bool
		want     string
	}{
		{"plain", "  hello \n", false, "hello"},
		{"fenced json", "```json\n{\"a\":1}\n```", true, `{"a":1}`},
		{"prose around object", "Sure! Here you go: {\"a\":{\"b\":\"}\"}} hope that helps", true, `{"a":{"b":"}"}}`},
		{"prose kept when not json", "Sure {not json", true, "Sure {not json"},
		{"prose untouched when text expected", "x {\"a\":1} y", false, `x {"a":1} y`},
	}
	for _, tc := range cases {
		if got := normalizeProviderReply(tc.in, tc.json); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestProviderArgvDefaultsAndOverride(t *testing.T) {
	for _, name := range []string{providerClaude, providerCodex, providerGemini, providerGrok} {
		argv, err := providerArgv(name)
		if err != nil || argv[0] != name {
			t.Fatalf("%s: argv=%v err=%v", name, argv, err)
		}
	}
	if _, err := providerArgv("bogus"); err == nil {
		t.Fatal("unknown provider must error")
	}
	t.Setenv("CONDUIT_PROVIDER_CLAUDE_CMD", "/opt/x --flag")
	argv, _ := providerArgv(providerClaude)
	if len(argv) != 2 || argv[0] != "/opt/x" {
		t.Fatalf("override ignored: %v", argv)
	}
}

func TestRunProviderPromptDelegatesToCLIOnStdin(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-llm")
	body := "#!/bin/sh\ncat > \"$0.stdin\"\nprintf 'Here:\\n```json\\n{\"toolCalls\":[{\"name\":\"inspect_repo\",\"arguments\":{}}]}\\n```\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONDUIT_PROVIDER_GEMINI_CMD", script)
	got, err := runProviderPrompt(context.Background(), providerGemini, "Inspect it. Reply with toolCalls JSON.")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"toolCalls":[{"name":"inspect_repo","arguments":{}}]}` {
		t.Fatalf("reply = %q", got)
	}
	sent, _ := os.ReadFile(script + ".stdin")
	if !strings.Contains(string(sent), "Inspect it.") || !strings.Contains(string(sent), "no markdown fences") {
		t.Fatalf("prompt not delivered on stdin: %q", sent)
	}
}

func TestRunProviderPromptSurfacesFailureAndEmptyReply(t *testing.T) {
	dir := t.TempDir()
	fail := filepath.Join(dir, "fail")
	_ = os.WriteFile(fail, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755)
	t.Setenv("CONDUIT_PROVIDER_CODEX_CMD", fail)
	if _, err := runProviderPrompt(context.Background(), providerCodex, "x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want stderr in error, got %v", err)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755)
	t.Setenv("CONDUIT_PROVIDER_CODEX_CMD", empty)
	if _, err := runProviderPrompt(context.Background(), providerCodex, "x"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want empty-reply error, got %v", err)
	}
}
