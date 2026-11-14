// Package domain defines immutable publisher-declared download assets.
// It describes metadata, not an Agent execution or artifact verification result.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalid   = errors.New("invalid download-asset declaration")
	ErrNotFound  = errors.New("download-asset release not found")
	ErrConflict  = errors.New("download-asset release is immutable")
	ErrForbidden = errors.New("download-asset operation forbidden")
	ErrCorrupt   = errors.New("download-asset record is inconsistent")
)

const Kind = "download-assets"
const SchemaVersion = "conduit.delivery/download-assets/v1"
const MaxDocumentBytes = 4 << 20

// ValueTier identifies this declaration format as Conduit-owned.
// CONDUIT-INVENTED: productassets.PublisherDeclaredRelease
// Displaced-by: a published or owner-approved download-asset Product contract establishing manifest shape, catalogue authority and execution eligibility.
const ValueTier = "CONDUIT_NATIVE"

var (
	identifier     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	filename       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)
	targetPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	hex256         = regexp.MustCompile(`^[a-f0-9]{64}$`)
	gitCommit      = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type Asset struct {
	Name                string `json:"name"`
	Target              string `json:"target,omitempty"`
	URI                 string `json:"uri"`
	SHA256              string `json:"sha256"`
	SizeBytes           *int64 `json:"sizeBytes,omitempty"`
	SignatureBundleURI  string `json:"signatureBundleUri,omitempty"`
	ProvenanceBundleURI string `json:"provenanceBundleUri,omitempty"`
}
type Source struct {
	RepositoryURI string `json:"repositoryUri"`
	Commit        string `json:"commit"`
}
type Manifest struct {
	APIVersion   string  `json:"apiVersion"`
	ProductGroup string  `json:"productGroup"`
	ProductName  string  `json:"productName"`
	Version      string  `json:"version"`
	Source       *Source `json:"source,omitempty"`
	Assets       []Asset `json:"assets"`
}

func ValidProduct(product string) bool {
	parts := strings.Split(product, ":")
	return len(parts) == 2 && identifier.MatchString(parts[0]) && identifier.MatchString(parts[1])
}
func ValidVersion(version string) bool {
	if !versionPattern.MatchString(version) {
		return false
	}
	for _, part := range strings.Split(version, ".") {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}
func ValidCoordinate(coordinate string) bool {
	parts := strings.Split(coordinate, ":")
	return len(parts) == 3 && ValidProduct(parts[0]+":"+parts[1]) && ValidVersion(parts[2])
}
func safeURI(raw string) bool {
	if len(raw) > 2048 || raw != strings.TrimSpace(raw) || strings.ContainsFunc(raw, unicode.IsControl) {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}
func (manifest Manifest) Product() string    { return manifest.ProductGroup + ":" + manifest.ProductName }
func (manifest Manifest) Coordinate() string { return manifest.Product() + ":" + manifest.Version }
func (manifest Manifest) Validate() error {
	if manifest.APIVersion != SchemaVersion || !ValidProduct(manifest.Product()) || !ValidVersion(manifest.Version) {
		return fmt.Errorf("%w: expected a native Product and stable catalogue version", ErrInvalid)
	}
	if manifest.Source != nil && (!safeURI(manifest.Source.RepositoryURI) || !gitCommit.MatchString(manifest.Source.Commit)) {
		return fmt.Errorf("%w: source requires an HTTPS repository and full commit", ErrInvalid)
	}
	if len(manifest.Assets) == 0 || len(manifest.Assets) > 64 {
		return fmt.Errorf("%w: expected 1 to 64 download assets", ErrInvalid)
	}
	names, targets := map[string]bool{}, map[string]bool{}
	for _, asset := range manifest.Assets {
		key := strings.ToLower(asset.Name)
		if !filename.MatchString(asset.Name) || names[key] || !safeURI(asset.URI) || !hex256.MatchString(asset.SHA256) {
			return fmt.Errorf("%w: assets require unique safe names, HTTPS coordinates and SHA-256 digests", ErrInvalid)
		}
		names[key] = true
		if asset.Target != "" {
			if len(asset.Target) > 128 || !targetPattern.MatchString(asset.Target) || targets[asset.Target] {
				return fmt.Errorf("%w: asset targets must be unique os/architecture coordinates", ErrInvalid)
			}
			targets[asset.Target] = true
		}
		if asset.SizeBytes != nil && *asset.SizeBytes <= 0 {
			return fmt.Errorf("%w: declared byte size must be positive", ErrInvalid)
		}
		for _, uri := range []string{asset.SignatureBundleURI, asset.ProvenanceBundleURI} {
			if uri != "" && !safeURI(uri) {
				return fmt.Errorf("%w: evidence coordinates must be credential-free HTTPS URIs", ErrInvalid)
			}
		}
	}
	return nil
}
func CloneManifest(manifest Manifest) Manifest {
	result := manifest
	result.Assets = append([]Asset(nil), manifest.Assets...)
	if manifest.Source != nil {
		source := *manifest.Source
		result.Source = &source
	}
	for i := range result.Assets {
		if result.Assets[i].SizeBytes != nil {
			size := *result.Assets[i].SizeBytes
			result.Assets[i].SizeBytes = &size
		}
	}
	return result
}
func (manifest Manifest) Canonical() (Manifest, error) {
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	result := CloneManifest(manifest)
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].Name < result.Assets[j].Name })
	return result, nil
}
func (manifest Manifest) Digest() (string, error) {
	canonical, err := manifest.Canonical()
	if err != nil {
		return "", err
	}
	document, err := json.Marshal(struct {
		Kind     string   `json:"kind"`
		Manifest Manifest `json:"manifest"`
	}{Kind, canonical})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(document)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type Prepared struct {
	Manifest        Manifest  `json:"manifest"`
	Digest          string    `json:"digest"`
	PreparedBy      string    `json:"preparedBy"`
	PreparedAt      time.Time `json:"preparedAt"`
	ValueTier       string    `json:"valueTier"`
	DeclarationTier string    `json:"declarationTier"`
}

func NewPrepared(manifest Manifest, subject string, now time.Time) (Prepared, error) {
	canonical, err := manifest.Canonical()
	if err != nil {
		return Prepared{}, err
	}
	digest, err := canonical.Digest()
	if err != nil {
		return Prepared{}, err
	}
	record := Prepared{Manifest: canonical, Digest: digest, PreparedBy: subject, PreparedAt: now.UTC(), ValueTier: ValueTier, DeclarationTier: "PUBLISHER_DECLARED"}
	return record, record.Validate()
}
func (record Prepared) Validate() error {
	digest, err := record.Manifest.Digest()
	if err != nil {
		return err
	}
	if record.Digest != digest || record.PreparedBy == "" || record.PreparedBy != strings.TrimSpace(record.PreparedBy) || len(record.PreparedBy) > 2048 || strings.ContainsFunc(record.PreparedBy, unicode.IsControl) || record.PreparedAt.IsZero() || record.ValueTier != ValueTier || record.DeclarationTier != "PUBLISHER_DECLARED" {
		return ErrCorrupt
	}
	return nil
}
func Clone(record Prepared) Prepared { record.Manifest = CloneManifest(record.Manifest); return record }
