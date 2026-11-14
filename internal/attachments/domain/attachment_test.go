package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestInspectDigestsExactBytesAsLowercaseHex(t *testing.T) {
	body := []byte("replicas: 2\n")
	item, err := Inspect("values.yaml", body)
	if err != nil {
		t.Fatal(err)
	}
	if item.SHA256 != Digest(body) || !ValidDigest(item.SHA256) || item.SHA256 != strings.ToLower(item.SHA256) {
		t.Fatalf("digest %q", item.SHA256)
	}
	if item.Kind != KindValues || item.Size != len(body) || item.Content != string(body) {
		t.Fatalf("item %+v", item)
	}
	other, _ := Inspect("values.yaml", []byte("replicas: 3\n"))
	if other.SHA256 == item.SHA256 {
		t.Fatal("different bytes must have different digests")
	}
}

func TestInspectAllowsTextIntakeTypes(t *testing.T) {
	for name, kind := range map[string]string{
		"Dockerfile": KindDockerfile, "Chart.yaml": KindChart, "values.yaml": KindValues,
		"extra.yml": KindYAML, "app.json": KindJSON, "notes.txt": KindText, "spec.md": KindSpec,
	} {
		item, err := Inspect(name, []byte("hello\n"))
		if err != nil || item.Kind != kind {
			t.Fatalf("%s: kind %q err %v", name, item.Kind, err)
		}
	}
}

func TestInspectRejectsDisallowedTypesWithAReason(t *testing.T) {
	for _, name := range []string{
		"ingress-oom-capture.png", "uds-telemetry-0.31.2.tar.zst", "bundle.zip", "tool.exe", "noext",
		"Dockerfile.bak", "../values.yaml", "a/b.yaml", "",
	} {
		_, err := Inspect(name, []byte("x: 1\n"))
		var rejection Rejection
		if !errors.As(err, &rejection) || rejection.Reason == "" || !errors.Is(err, ErrRejected) {
			t.Fatalf("%q: %v", name, err)
		}
	}
}

func TestInspectEnforcesSizeLimits(t *testing.T) {
	_, err := Inspect("big.txt", []byte(strings.Repeat("a", MaxFileBytes+1)))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("file cap: %v", err)
	}
	if _, err := Inspect("big.txt", []byte(strings.Repeat("a", MaxFileBytes))); err != nil {
		t.Fatalf("exactly the cap: %v", err)
	}
	if _, err := Inspect("spec.md", []byte(strings.Repeat("a", MaxSpecBytes+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("spec cap: %v", err)
	}
	if _, err := Inspect("empty.txt", nil); err == nil {
		t.Fatal("empty file must be rejected")
	}
}

func TestInspectRejectsBinaryContent(t *testing.T) {
	if _, err := Inspect("a.txt", []byte("ab\x00cd")); err == nil {
		t.Fatal("NUL byte must be rejected")
	}
	if _, err := Inspect("a.txt", []byte{0xff, 0xfe, 0xfd}); err == nil {
		t.Fatal("invalid utf-8 must be rejected")
	}
}

func TestSecretScanMatchesTheGenerationChecks(t *testing.T) {
	blocked := []string{
		"-----BEGIN PRIVATE KEY-----", "-----BEGIN CERTIFICATE-----",
		"db: Password=hunter2", "client_secret=abc", "API_KEY: abc", "auth_token=abc",
	}
	for _, text := range blocked {
		if _, ok := SecretReason(text); !ok {
			t.Fatalf("%q should be blocked", text)
		}
		_, err := Inspect("values.yaml", []byte(text+"\n"))
		if !errors.Is(err, ErrRejected) {
			t.Fatalf("%q was not rejected: %v", text, err)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatal("the reason must not echo content")
		}
	}
	if _, ok := SecretReason("memory: 512Mi\nport: 8080\n"); ok {
		t.Fatal("plain values must pass")
	}
}
