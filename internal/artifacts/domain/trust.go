package domain

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ParseTrustedKeys reads the trust map a Hub loads from its trusted-keys file.
// The document is a JSON object whose values are base64-encoded Ed25519 public
// keys, padded or not. The object key is the identifier VerifyEnvelope looks up.
func ParseTrustedKeys(contents []byte) (map[string]ed25519.PublicKey, error) {
	encoded := make(map[string]string)
	if err := json.Unmarshal(contents, &encoded); err != nil {
		return nil, fmt.Errorf("decode trusted-key map: %w", err)
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	for id, value := range encoded {
		decoded, decodeErr := base64.RawStdEncoding.DecodeString(value)
		if decodeErr != nil {
			decoded, decodeErr = base64.StdEncoding.DecodeString(value)
		}
		if decodeErr != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("trusted Bundle key %q must be a base64 Ed25519 public key", id)
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}
