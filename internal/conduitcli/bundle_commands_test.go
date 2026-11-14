package conduitcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifacts "github.com/jonathanagustin/conduit/internal/artifacts/domain"
)

func TestBundleVerifyUsesTheSharedVerifierAndDoesNotContactAHub(t *testing.T) {
	t.Parallel()
	key, err := artifacts.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	resource := artifacts.Resource{Type: "RELEASE", ID: "checkout", Payload: json.RawMessage(`{"name":"checkout"}`)}
	resource.Digest = artifacts.ResourceDigest(resource)
	envelope, err := artifacts.Sign(
		key,
		artifacts.Origin{PipelineID: "pipe", PipelineRevision: 1, Sequence: 1, Kind: artifacts.KindSnapshot, ResourceTypes: []string{"RELEASE"}},
		time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		[]artifacts.Resource{resource}, nil, nil,
		artifacts.SecurityReport{Outcome: "PASS"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	keyID := artifacts.SigningKeyID(public)
	directory := t.TempDir()
	keysPath := filepath.Join(directory, "keys.json")
	encoded := base64.RawStdEncoding.EncodeToString(public)
	if err := os.WriteFile(keysPath, []byte(`{"`+keyID+`":"`+encoded+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(directory, "bundle.json")
	if err := os.WriteFile(bundlePath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	var output bytes.Buffer
	root := NewRoot(Runtime{
		Stdout: &output,
		Stderr: &output,
		Getenv: func(string) string { return "" },
		ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
			called = true
			return nil, func() {}, errors.New("hub should not be contacted")
		},
	})
	root.SetArgs([]string{"bundle", "verify", bundlePath, "--trusted-keys", keysPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("verify: %v\n%s", err, output.String())
	}
	if called {
		t.Fatal("offline verification constructed a Hub client")
	}
	if !strings.Contains(output.String(), `"verified": true`) || !strings.Contains(output.String(), envelope.Manifest.ID) {
		t.Fatalf("output=%s", output.String())
	}

	if err := os.WriteFile(keysPath, []byte(`{"signer-1":"`+encoded+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	root = NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"bundle", "verify", bundlePath, "--trusted-keys", keysPath})
	err = root.Execute()
	var status StatusError
	if err == nil || errors.As(err, &status) || !strings.Contains(err.Error(), `invalid trusted Bundle signing key "signer-1"`) {
		t.Fatalf("error=%v", err)
	}

	if err := os.WriteFile(keysPath, []byte(`{"`+keyID+`":"`+encoded+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope.Manifest.Resources[0].Payload = json.RawMessage(`{"name":"other"}`)
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	root = NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"-o", "table", "bundle", "verify", bundlePath, "--trusted-keys", keysPath})
	err = root.Execute()
	if !errors.As(err, &status) || status.ExitCode() != bundleRejectedStatus {
		t.Fatalf("error=%v", err)
	}
	if !errors.Is(err, artifacts.ErrDigestMismatch) && !errors.Is(err, artifacts.ErrUntrusted) {
		t.Fatalf("rejection was not the shared verifier: %v", err)
	}
}
