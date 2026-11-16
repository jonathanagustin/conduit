// Package domain is the intake attachment contract. It validates one uploaded
// file and names it by the sha256 of its exact bytes. It does not touch a disk.
//
// CONDUIT-INVENTED: intake.AttachmentStore
// Displaced-by: a published intake object schema that states which files an intake request may carry, their limits, and how they are named.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaxFileBytes  = 256 * 1024
	MaxSpecBytes  = 16 * 1024
	MaxTotalBytes = 1024 * 1024
	MaxCount      = 8
	MaxNameBytes  = 100
)

// Kinds an attachment may have. They describe text-like intake files only.
const (
	KindDockerfile = "Dockerfile"
	KindChart      = "Chart"
	KindValues     = "Values"
	KindYAML       = "YAML"
	KindJSON       = "JSON"
	KindText       = "Text"
	KindSpec       = "Spec"
)

var (
	// ErrRejected is wrapped by every refusal that carries a reason for the operator.
	ErrRejected = errors.New("attachment was rejected")
	// ErrTooLarge is a rejection for size or count. The HTTP adapter maps it to 413.
	ErrTooLarge = fmt.Errorf("%w: over a limit", ErrRejected)
	ErrNotFound = errors.New("attachment was not found")
)

// Rejection is a refusal with a reason that is safe to show. It never echoes content.
type Rejection struct {
	Reason string
	Large  bool
}

func (r Rejection) Error() string { return r.Reason }

func (r Rejection) Is(target error) bool {
	if target == ErrTooLarge {
		return r.Large
	}
	return target == ErrRejected
}

func reject(format string, args ...any) error {
	return Rejection{Reason: fmt.Sprintf(format, args...)}
}

func rejectLarge(format string, args ...any) error {
	return Rejection{Reason: fmt.Sprintf(format, args...), Large: true}
}

// Attachment is the stored record. SHA256 is lowercase hex of the exact bytes.
type Attachment struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	// Owners are the operators who stored these bytes. The field is not part
	// of the HTTP body. Empty means the row predates owner records.
	Owners []string `json:"-"`
}

// Stored is an attachment with its bytes. It is read-only context.
type Stored struct {
	Attachment
	Content string `json:"content"`
}

// Digest is the lowercase sha256 hex of the exact bytes.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidDigest reports whether value is 64 lowercase hex characters.
func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Inspect checks the name, type, size, encoding, and secret scan of one file.
// Nothing is stored when it returns an error.
func Inspect(name string, content []byte) (Stored, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Stored{}, reject("file name is required")
	}
	if len(name) > MaxNameBytes {
		return Stored{}, reject("file name is longer than %d bytes", MaxNameBytes)
	}
	if strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." || strings.Contains(name, "..") {
		return Stored{}, reject("file name must not contain a path")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return Stored{}, reject("file name has a control character")
		}
	}
	kind, limit, ok := classify(name)
	if !ok {
		return Stored{}, reject("%s is not an allowed type; use Dockerfile, .yaml, .yml, .json, .txt, or a short .md spec", name)
	}
	if len(content) == 0 {
		return Stored{}, reject("%s is empty", name)
	}
	if len(content) > limit {
		return Stored{}, rejectLarge("%s is %d bytes and the limit for this type is %d", name, len(content), limit)
	}
	if !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return Stored{}, reject("%s is not text", name)
	}
	if reason, blocked := SecretReason(string(content)); blocked {
		return Stored{}, reject("%s was not stored: %s", name, reason)
	}
	return Stored{
		Attachment: Attachment{Name: name, Kind: kind, Size: len(content), SHA256: Digest(content)},
		Content:    string(content),
	}, nil
}

func classify(name string) (kind string, limit int, ok bool) {
	if name == "Dockerfile" {
		return KindDockerfile, MaxFileBytes, true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml":
		switch name {
		case "Chart.yaml", "Chart.yml":
			return KindChart, MaxFileBytes, true
		case "values.yaml", "values.yml":
			return KindValues, MaxFileBytes, true
		}
		return KindYAML, MaxFileBytes, true
	case ".json":
		return KindJSON, MaxFileBytes, true
	case ".txt":
		return KindText, MaxFileBytes, true
	case ".md":
		return KindSpec, MaxSpecBytes, true
	}
	return "", 0, false
}

// SecretReason applies the same checks the app generation gate applies to source:
// key or certificate material and credential assignments.
func SecretReason(text string) (string, bool) {
	folded := strings.ToLower(text)
	switch {
	case strings.Contains(text, "BEGIN PRIVATE KEY"), strings.Contains(text, "BEGIN CERTIFICATE"):
		return "it contains key or certificate material", true
	case strings.Contains(folded, "password="), strings.Contains(folded, "secret="), strings.Contains(folded, "api_key"), strings.Contains(folded, "token="):
		return "it contains a credential assignment", true
	default:
		return "", false
	}
}
