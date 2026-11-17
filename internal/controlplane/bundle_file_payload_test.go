package controlplane

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestFileBundlePayloadRecoveryRequiresMatchingDigest(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		prettyStorage bool
		wrongDigest   bool
	}{
		{
			name:          "recover indented storage without interpreting the payload",
			payload:       `{"number":1.00,"text":"two  spaces","escaped":"line\nnext","object":{"b":2,"a":1}}`,
			prettyStorage: true,
		},
		{name: "preserve already matching raw bytes", payload: "{\n  \"number\": 1.00, \"text\": \"two  spaces\"\n}"},
		{name: "leave an unrecoverable mismatch unchanged", payload: "{\n  \"tampered\": true\n}", wrongDigest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, err := NewFileRepository(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			signer, err := NewBundleService(repository, mustBundleKey(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			resource := BundleResource{Type: "TEST", ID: "opaque", Payload: json.RawMessage(test.payload)}
			resource.Digest = sha256Digest(sha256.Sum256(resource.Payload))
			if test.wrongDigest {
				resource.Digest = sha256Digest(sha256.Sum256([]byte("different original bytes")))
			}
			envelope, err := signer.signBundle(ExportPipeline{ID: "payload", Revision: 1}, 1,
				BundleSnapshot, "", "", "", false, false, []BundleResource{resource}, nil, nil, BundleSecurityReport{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.prettyStorage {
				if _, err := repository.CreateBundle(t.Context(), envelope); err != nil {
					t.Fatal(err)
				}
			} else {
				// Keep the chosen payload bytes literal in the storage fixture.
				// json.Marshal itself would compact a RawMessage before writing it.
				encoded, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				var compact bytes.Buffer
				if err := json.Compact(&compact, resource.Payload); err != nil {
					t.Fatal(err)
				}
				from := append([]byte(`"payload":`), compact.Bytes()...)
				to := append([]byte(`"payload":`), resource.Payload...)
				if bytes.Count(encoded, from) != 1 {
					t.Fatal("payload fixture must replace exactly one JSON value")
				}
				if err := os.WriteFile(repository.bundlePath(envelope.Manifest.ID), bytes.Replace(encoded, from, to, 1), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := repository.GetBundle(t.Context(), envelope.Manifest.ID)
			if err != nil {
				t.Fatal(err)
			}
			listed, err := repository.ListBundles(t.Context(), "payload")
			if err != nil || len(listed) != 1 {
				t.Fatalf("list = %#v, %v", listed, err)
			}
			for name, result := range map[string]BundleEnvelope{"get": stored, "list": listed[0]} {
				if !bytes.Equal(result.Manifest.Resources[0].Payload, resource.Payload) ||
					result.Manifest.Resources[0].Digest != resource.Digest {
					t.Errorf("%s rewrote opaque bytes or their declared digest: %q", name, result.Manifest.Resources[0].Payload)
				}
				err := signer.verifyEnvelope(result)
				if test.wrongDigest && !errors.Is(err, ErrDigestMismatch) || !test.wrongDigest && err != nil {
					t.Errorf("%s verification = %v", name, err)
				}
			}
		})
	}
}
