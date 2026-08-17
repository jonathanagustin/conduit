package consoleui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"
)

const (
	BootstrapSchemaVersion = "conduit.console.bootstrap/v1"
	// BootstrapMetaName is the meta the Hub injects into the application shell
	// and the console reads before it starts. One meta carrying the whole
	// document, rather than one per section: the console's validator is a closed
	// surface over a single object -- it refuses unknown and missing fields
	// alike, which is what makes tenant data unable to smuggle in a capability
	// -- and splitting it across several metas would make an extra meta the
	// silently-ignored case instead of the refused one.
	BootstrapMetaName = "x-conduit-console-bootstrap"
	maxBootstrapBytes = 64 << 10
)

var (
	tenantIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$`)
	accentPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Bootstrap is the complete tenant-controlled runtime surface for the
// operator console. It deliberately has no API-origin, authentication,
// capability, script, feature-toggle, or security-policy fields.
type Bootstrap struct {
	SchemaVersion string              `json:"schemaVersion"`
	Tenant        BootstrapTenant     `json:"tenant"`
	Brand         BootstrapBrand      `json:"brand"`
	Provenance    BootstrapProvenance `json:"provenance"`
}

type BootstrapTenant struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type BootstrapBrand struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Accent   string `json:"accent"`
	Icon     string `json:"icon"`
}

type BootstrapProvenance struct {
	ClaimClass  string `json:"claimClass"`
	EvidenceRef string `json:"evidenceRef"`
}

// DefaultBootstrap identifies an explicitly Conduit-native console. It is not
// an inferred tenant profile and cannot grant runtime capabilities.
func DefaultBootstrap() Bootstrap {
	return Bootstrap{
		SchemaVersion: BootstrapSchemaVersion,
		Tenant: BootstrapTenant{
			ID:          "conduit",
			DisplayName: "Conduit",
		},
		Brand: BootstrapBrand{
			Title:    "Conduit",
			Subtitle: "Control plane",
			Accent:   "#2d72d2",
			Icon:     "dashboard",
		},
		Provenance: BootstrapProvenance{
			ClaimClass:  "conduit-extension",
			EvidenceRef: "conduit://console/default",
		},
	}
}

// LoadBootstrapFile reads a runtime bootstrap from path. An empty path selects
// the explicit Conduit default. Callers should fail startup on any other error.
func LoadBootstrapFile(path string) (Bootstrap, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return DefaultBootstrap(), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return Bootstrap{}, fmt.Errorf("open console bootstrap: %w", err)
	}
	defer file.Close()
	bootstrap, err := DecodeBootstrap(file)
	if err != nil {
		return Bootstrap{}, fmt.Errorf("decode console bootstrap: %w", err)
	}
	return bootstrap, nil
}

// DecodeBootstrap accepts exactly one bounded JSON document and rejects
// unknown fields so a misspelled security-sensitive option cannot be ignored.
func DecodeBootstrap(reader io.Reader) (Bootstrap, error) {
	if reader == nil {
		return Bootstrap{}, errors.New("console bootstrap reader is required")
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxBootstrapBytes+1))
	if err != nil {
		return Bootstrap{}, fmt.Errorf("read console bootstrap: %w", err)
	}
	if len(contents) > maxBootstrapBytes {
		return Bootstrap{}, fmt.Errorf("console bootstrap exceeds %d bytes", maxBootstrapBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var bootstrap Bootstrap
	if err := decoder.Decode(&bootstrap); err != nil {
		return Bootstrap{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Bootstrap{}, errors.New("console bootstrap must contain exactly one JSON document")
		}
		return Bootstrap{}, err
	}
	if err := bootstrap.Validate(); err != nil {
		return Bootstrap{}, err
	}
	return bootstrap, nil
}

// Validate enforces the same closed value contract as the browser parser.
func (bootstrap Bootstrap) Validate() error {
	if bootstrap.SchemaVersion != BootstrapSchemaVersion {
		return fmt.Errorf("unsupported console bootstrap schema %q", bootstrap.SchemaVersion)
	}
	if !tenantIDPattern.MatchString(bootstrap.Tenant.ID) {
		return errors.New("tenant id must be 1-63 lowercase letters, digits, dots, underscores, or hyphens")
	}
	for name, value := range map[string]struct {
		text string
		max  int
	}{
		"tenant display name": {bootstrap.Tenant.DisplayName, 80},
		"brand title":         {bootstrap.Brand.Title, 80},
		"brand subtitle":      {bootstrap.Brand.Subtitle, 120},
	} {
		if err := validateDisplayText(value.text, value.max); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if !accentPattern.MatchString(bootstrap.Brand.Accent) {
		return errors.New("brand accent must be a six-digit hexadecimal color")
	}
	switch bootstrap.Brand.Icon {
	case "application", "cloud", "dashboard":
	default:
		return fmt.Errorf("brand icon %q is not allowed", bootstrap.Brand.Icon)
	}
	switch bootstrap.Provenance.ClaimClass {
	case "tenant-observation":
		return validateEvidenceRef(bootstrap.Provenance.EvidenceRef, "evidence")
	case "conduit-extension":
		return validateEvidenceRef(bootstrap.Provenance.EvidenceRef, "conduit")
	default:
		return fmt.Errorf("bootstrap claim class %q is not allowed", bootstrap.Provenance.ClaimClass)
	}
}

func validateDisplayText(value string, maximum int) error {
	if value == "" || strings.TrimSpace(value) != value {
		return errors.New("value must be non-empty and have no surrounding whitespace")
	}
	if len([]rune(value)) > maximum {
		return fmt.Errorf("value exceeds %d characters", maximum)
	}
	lower := strings.ToLower(value)
	if strings.ContainsAny(value, "<>") || strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") {
		return errors.New("value contains executable markup or a URI scheme")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("value contains a control character")
		}
	}
	return nil
}

func validateEvidenceRef(value, expectedScheme string) error {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return errors.New("bootstrap evidence reference is empty, too long, or not canonical")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != expectedScheme || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.String() != value || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return fmt.Errorf("bootstrap evidence reference must be a canonical %s:// coordinate", expectedScheme)
	}
	return nil
}
