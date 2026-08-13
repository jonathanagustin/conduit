package controlplane

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/artifacts"
)

// A malformed envelope has always answered 409, and writeLifecycleError decides
// that from ErrConflict. Artifacts calls the same condition ErrEnvelopeInvalid,
// which is the right name in a package that knows nothing about HTTP, so this
// package restates it on the way out.
//
// This test exists because deleting that restatement breaks no other test: the
// import path would start answering 500, and nothing would say so.
func TestMalformedEnvelopeStillReadsAsConflict(t *testing.T) {
	t.Parallel()
	privateKey := mustBundleKey(t)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	envelope, err := artifacts.Sign(
		privateKey,
		artifacts.Origin{PipelineID: "pipe", Kind: "SIDEWAYS"},
		time.Now(),
		nil, nil, nil, artifacts.SecurityReport{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	service, err := NewBundleService(
		NewMemoryRepository(),
		privateKey,
		map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.verifyEnvelope(envelope)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a malformed envelope must read as ErrConflict so the handler answers 409, got %v", err)
	}
	if !errors.Is(err, artifacts.ErrEnvelopeInvalid) {
		t.Fatalf("the specific reason must survive the restatement, got %v", err)
	}
}
