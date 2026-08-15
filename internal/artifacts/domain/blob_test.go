package domain

import (
	"errors"
	"testing"
)

func TestNormalizeMediaTypeDefaultsOnlyWhenThereIsNoLabel(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"":                          "application/octet-stream",
		"   ":                       "application/octet-stream",
		"  application/vnd.oci  ":   "application/vnd.oci",
		"application/vnd.oci+json":  "application/vnd.oci+json",
		"application/x-tar; a=b":    "application/x-tar; a=b",
		"APPLICATION/OCTET-STREAM":  "APPLICATION/OCTET-STREAM",
		"not a media type at all":   "not a media type at all",
		"application/vnd.oci.image": "application/vnd.oci.image",
	} {
		// The label is metadata and the digest is the identity, so a label this
		// package does not recognise is carried through rather than replaced.
		// Replacing it would lose what the sender said without gaining anything.
		if got := NormalizeMediaType(input); got != want {
			t.Fatalf("NormalizeMediaType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateExpectedDigestAcceptsAbsenceAndRefusesNonsense(t *testing.T) {
	t.Parallel()
	// An empty digest is a server-computed upload: the store assigns identity.
	if err := ValidateExpectedDigest(""); err != nil {
		t.Fatalf("a server-computed upload was refused: %v", err)
	}
	good := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := ValidateExpectedDigest(good); err != nil {
		t.Fatalf("a well-formed digest was refused: %v", err)
	}
	for name, digest := range map[string]string{
		"uppercase hex":     "sha256:AB12CD34EF560123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123",
		"wrong algorithm":   "sha512:ab12cd34ef560123456789abcdef0123456789abcdef0123456789abcdef0123",
		"too short":         "sha256:ab12cd34",
		"no algorithm":      "ab12cd34ef560123456789abcdef0123456789abcdef0123456789abcdef0123",
		"trailing rubbish":  "sha256:ab12cd34ef560123456789abcdef0123456789abcdef0123456789abcdef0123x",
		"looks like a path": "sha256:../../etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// A malformed digest is refused rather than compared against.
			// Comparing against nonsense can only fail, and it would fail in a
			// way that reads like corruption.
			if err := ValidateExpectedDigest(digest); !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("ValidateExpectedDigest(%q) = %v", digest, err)
			}
		})
	}
}

func TestEnvironmentQuestionsReadTheManifestAndNothingElse(t *testing.T) {
	t.Parallel()
	envelope := Envelope{Manifest: Manifest{Resources: []Resource{
		{Type: "PRODUCT", ID: "app.example:service"},
		{Type: ResourceTypeEnvironment, ID: "dev-use2"},
		{Type: ResourceTypeEnvironment, ID: "prod-use1", Deleted: true},
	}}}

	if !CarriesEnvironmentSettings(envelope, "dev-use2") {
		t.Fatal("a Bundle carrying an Environment was read as not carrying it")
	}
	// A deleted resource withdraws settings; it does not carry them.
	if CarriesEnvironmentSettings(envelope, "prod-use1") {
		t.Fatal("a deleted Environment resource was read as carrying settings")
	}
	if CarriesEnvironmentSettings(envelope, "never-mentioned") {
		t.Fatal("an Environment the Bundle does not mention was read as carried")
	}

	if FirstEnvironment(envelope) != "dev-use2" {
		t.Fatalf("FirstEnvironment = %q", FirstEnvironment(envelope))
	}
	// A malformed identifier is not a weaker answer, it is a different
	// resource, so it is skipped rather than returned.
	malformed := Envelope{Manifest: Manifest{Resources: []Resource{
		{Type: ResourceTypeEnvironment, ID: "Not A Label"},
		{Type: ResourceTypeEnvironment, ID: "dev-use2"},
	}}}
	if FirstEnvironment(malformed) != "dev-use2" {
		t.Fatalf("FirstEnvironment skipped past a valid label: %q", FirstEnvironment(malformed))
	}
	if FirstEnvironment(Envelope{}) != "" {
		t.Fatal("an empty Bundle named an Environment")
	}
}
