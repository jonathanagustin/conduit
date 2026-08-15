package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrCorrupt is content that does not match the digest it is stored under.
	ErrCorrupt = errors.New("artifact content is corrupt")

	// ErrLayerMissing is an artifact layer a Bundle says was transferred out of
	// band, which the receiving store does not have. It is a distinct failure
	// from corruption: the bytes are not wrong, they are absent.
	ErrLayerMissing = errors.New("artifact layer is not present")
)

// BlobDescriptor is the content identity used at every artifact boundary. It
// intentionally mirrors the required portion of an OCI content descriptor.
// Digest and size describe the raw bytes; MediaType is contextual metadata.
type BlobDescriptor struct {
	MediaType string    `json:"mediaType"`
	Digest    string    `json:"digest"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// NormalizeMediaType settles what an artifact is labelled as. An absent label
// becomes the generic binary type rather than being rejected: the label is
// metadata, and the digest is what identifies the content.
func NormalizeMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

// ValidateExpectedDigest checks a caller-supplied digest before any bytes are
// read. An empty digest is permitted — that is a server-computed upload, where
// the store assigns the identity — but a malformed one is refused rather than
// compared against, because a comparison against nonsense can only fail in a
// way that reads like corruption.
func ValidateExpectedDigest(digest string) error {
	if digest == "" || digestPattern.MatchString(digest) {
		return nil
	}
	return fmt.Errorf("%w: expected digest must be lowercase sha256", ErrDigestMismatch)
}

// CarriesEnvironmentSettings reports whether a Bundle brings this
// Environment's own settings, as opposed to merely mentioning it. A resource
// marked deleted does not carry settings; it withdraws them.
func CarriesEnvironmentSettings(envelope Envelope, environment string) bool {
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type == ResourceTypeEnvironment && resource.ID == environment && !resource.Deleted {
			return true
		}
	}
	return false
}

// FirstEnvironment names the Environment a Bundle is about when it does not say
// so anywhere else. Only a well-formed identifier counts: a malformed one is
// not a weaker answer, it is a different resource.
func FirstEnvironment(envelope Envelope) string {
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type == ResourceTypeEnvironment && dnsLabelPattern.MatchString(resource.ID) {
			return resource.ID
		}
	}
	return ""
}

// ResourceTypeEnvironment is the one resource type this package must recognise
// by name, because two envelope questions are about Environments specifically.
// Restating it rather than importing keeps the dependency direction: the
// contexts that own these resources know about Bundles, not the other way.
const ResourceTypeEnvironment = "ENVIRONMENT"

// A DNS label, restated for the same reason.
var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
