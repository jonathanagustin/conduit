package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Spec is the only document a generator may return.
type Spec struct {
	Name      string       `json:"name"`
	Port      int          `json:"port"`
	Routes    []Route      `json:"routes"`
	Page      Page         `json:"page"`
	Resources Resources    `json:"resources"`
	Files     []SourceFile `json:"files"`
}

// Route is one HTTP route the application serves.
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// Page is the text shown at the root path.
type Page struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Resources is the ceiling the container must stay under.
type Resources struct {
	Memory string `json:"memory"`
	CPU    string `json:"cpu"`
}

// SourceFile is one allowlisted source file.
type SourceFile struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Content  string `json:"content"`
}

// Gate is one pre-pipeline check and the reason it passed or failed.
type Gate struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

const (
	OutcomePass = "pass"
	OutcomeFail = "fail"
	OutcomeSkip = "skip"
)

var allowedFiles = map[string]string{
	"main.go":    "go",
	"main.py":    "python",
	"index.html": "html",
}

// ParseSpec accepts one JSON object and nothing else.
func ParseSpec(raw []byte) (Spec, error) {
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var spec Spec
	if err := decoder.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("%w: %v", ErrSpecInvalid, err)
	}
	if decoder.More() {
		return Spec{}, fmt.Errorf("%w: extra data after the object", ErrSpecInvalid)
	}
	if err := validateSchema(spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func validateSchema(spec Spec) error {
	if !namePattern(spec.Name) {
		return fmt.Errorf("%w: name must be a lowercase identifier", ErrSpecInvalid)
	}
	if spec.Port < 1024 || spec.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1024 and 65535", ErrSpecInvalid)
	}
	if len(spec.Routes) < 1 || len(spec.Routes) > 8 {
		return fmt.Errorf("%w: routes must contain 1 to 8 entries", ErrSpecInvalid)
	}
	health := false
	seen := map[string]bool{}
	for _, route := range spec.Routes {
		method := strings.ToUpper(strings.TrimSpace(route.Method))
		if method != "GET" && method != "POST" {
			return fmt.Errorf("%w: route method must be GET or POST", ErrSpecInvalid)
		}
		if !pathPattern(route.Path) {
			return fmt.Errorf("%w: route path is not allowed", ErrSpecInvalid)
		}
		if route.Status != 200 && route.Status != 201 && route.Status != 204 && route.Status != 400 && route.Status != 404 {
			return fmt.Errorf("%w: route status is not allowed", ErrSpecInvalid)
		}
		if err := textOK(route.Body, 512, true); err != nil {
			return fmt.Errorf("%w: route body %v", ErrSpecInvalid, err)
		}
		key := method + " " + route.Path
		if seen[key] {
			return fmt.Errorf("%w: duplicate route %s", ErrSpecInvalid, key)
		}
		seen[key] = true
		if method == "GET" && route.Path == "/health" && route.Status == 200 {
			health = true
		}
	}
	if !health {
		return fmt.Errorf("%w: a GET /health route with status 200 is required", ErrSpecInvalid)
	}
	if err := textOK(spec.Page.Title, 80, false); err != nil {
		return fmt.Errorf("%w: page title %v", ErrSpecInvalid, err)
	}
	if err := textOK(spec.Page.Body, 2000, true); err != nil {
		return fmt.Errorf("%w: page body %v", ErrSpecInvalid, err)
	}
	if _, ok := parseMiB(spec.Resources.Memory); !ok {
		return fmt.Errorf("%w: memory must be a size in Mi or Gi", ErrSpecInvalid)
	}
	if _, ok := parseCPU(spec.Resources.CPU); !ok {
		return fmt.Errorf("%w: cpu must be millicores such as 100m or a whole core such as 1", ErrSpecInvalid)
	}
	if len(spec.Files) > MaxFiles {
		return fmt.Errorf("%w: at most %d source files", ErrSpecInvalid, MaxFiles)
	}
	for _, file := range spec.Files {
		if strings.TrimSpace(file.Path) == "" || strings.TrimSpace(file.Language) == "" {
			return fmt.Errorf("%w: each file needs a path and a language", ErrSpecInvalid)
		}
		if !utf8.ValidString(file.Content) {
			return fmt.Errorf("%w: file %s is not utf-8", ErrSpecInvalid, file.Path)
		}
	}
	return nil
}

// CheckAllowlist rejects paths and languages outside the fixed set.
// The set is exactly one of main.go, main.py, or index.html.
func CheckAllowlist(spec Spec) (string, bool) {
	if len(spec.Files) == 0 {
		return AllowedSourceShapes, false
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(spec.Files))
	for _, file := range spec.Files {
		language, ok := allowedFiles[file.Path]
		if !ok {
			return fmt.Sprintf("path %s is not on the allowlist; %s", file.Path, AllowedSourceShapes), false
		}
		if file.Language != language {
			return fmt.Sprintf("path %s must use language %s; %s", file.Path, language, AllowedSourceShapes), false
		}
		if seen[file.Path] {
			return fmt.Sprintf("path %s is repeated", file.Path), false
		}
		seen[file.Path] = true
		paths = append(paths, file.Path)
	}
	if _, reason, ok := shapeFromPaths(paths); !ok {
		return reason, false
	}
	return "source paths and languages are on the allowlist", true
}

// ScanSecretsAndSize rejects credentials and files over the size ceiling.
func ScanSecretsAndSize(spec Spec) (string, bool) {
	var total int
	texts := []string{spec.Name, spec.Page.Title, spec.Page.Body}
	for _, route := range spec.Routes {
		texts = append(texts, route.Path, route.Body)
	}
	for _, file := range spec.Files {
		size := len(file.Content)
		total += size
		if size > MaxFileBytes {
			return fmt.Sprintf("file %s is %d bytes and the limit is %d", file.Path, size, MaxFileBytes), false
		}
		texts = append(texts, file.Content)
	}
	if total > MaxTotalBytes {
		return fmt.Sprintf("source files are %d bytes and the limit is %d", total, MaxTotalBytes), false
	}
	for _, text := range texts {
		if reason, blocked := secretReason(text); blocked {
			return reason, false
		}
	}
	return "no secret material and source files are within the size limit", true
}

// AssessResources applies the configured memory and cpu ceilings.
func AssessResources(spec Spec) (string, bool) {
	memory, ok := parseMiB(strings.TrimSpace(spec.Resources.Memory))
	if !ok {
		return "memory limit is not a size", false
	}
	if memory > MemoryCeilingMiB {
		return fmt.Sprintf("memory limit %s exceeds the 1Gi ceiling", strings.TrimSpace(spec.Resources.Memory)), false
	}
	cpu, ok := parseCPU(strings.TrimSpace(spec.Resources.CPU))
	if !ok {
		return "cpu limit is not millicores", false
	}
	if cpu > CPUCeilingMilli {
		return fmt.Sprintf("cpu limit %s exceeds the 1 core ceiling", strings.TrimSpace(spec.Resources.CPU)), false
	}
	return "memory and cpu are within the configured ceiling", true
}

func secretReason(text string) (string, bool) {
	folded := strings.ToLower(text)
	switch {
	case strings.Contains(text, "BEGIN PRIVATE KEY"), strings.Contains(text, "BEGIN CERTIFICATE"):
		return "source contains key or certificate material", true
	case strings.Contains(folded, "password="), strings.Contains(folded, "secret="), strings.Contains(folded, "api_key"), strings.Contains(folded, "token="):
		return "source contains a credential assignment", true
	default:
		return "", false
	}
}

func textOK(value string, limit int, allowBreaks bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("is required")
	}
	if len(value) > limit {
		return fmt.Errorf("is longer than %d bytes", limit)
	}
	for _, r := range value {
		if r == '\n' || r == '\t' {
			if allowBreaks {
				continue
			}
			return fmt.Errorf("has a control character")
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("has a control character")
		}
	}
	return nil
}

func namePattern(value string) bool {
	if len(value) < 2 || len(value) > 40 {
		return false
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(value)-1:
		default:
			return false
		}
	}
	return value[0] >= 'a' && value[0] <= 'z'
}

func pathPattern(value string) bool {
	if value == "" || len(value) > 64 || !strings.HasPrefix(value, "/") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '.' || r == '_' || r == '~' || r == '-':
		default:
			return false
		}
	}
	return true
}

func parseMiB(value string) (int, bool) {
	if strings.HasSuffix(value, "Mi") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "Mi"))
		if err != nil || n < 1 {
			return 0, false
		}
		return n, true
	}
	if strings.HasSuffix(value, "Gi") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "Gi"))
		if err != nil || n < 1 {
			return 0, false
		}
		return n * 1024, true
	}
	return 0, false
}

func parseCPU(value string) (int, bool) {
	if strings.HasSuffix(value, "m") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "m"))
		if err != nil || n < 1 {
			return 0, false
		}
		return n, true
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, false
	}
	return n * 1000, true
}
