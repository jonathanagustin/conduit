package domain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseTrustedKeysAcceptsBothBase64SpellingsAndNamesTheBadKey(t *testing.T) {
	t.Parallel()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, encoded := range map[string]string{
		"padded":   base64.StdEncoding.EncodeToString(public),
		"unpadded": base64.RawStdEncoding.EncodeToString(public),
	} {
		keys, err := ParseTrustedKeys([]byte(`{"` + SigningKeyID(public) + `":"` + encoded + `"}`))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !keys[SigningKeyID(public)].Equal(public) {
			t.Errorf("%s: key did not round-trip", name)
		}
	}
	_, err = ParseTrustedKeys([]byte(`{"broken-key":"` + base64.StdEncoding.EncodeToString(public[:16]) + `"}`))
	if err == nil || !strings.Contains(err.Error(), "broken-key") {
		t.Fatalf("error = %v", err)
	}
	if _, err := ParseTrustedKeys([]byte(`[]`)); err == nil {
		t.Fatal("a JSON array was accepted as a trust map")
	}
}
