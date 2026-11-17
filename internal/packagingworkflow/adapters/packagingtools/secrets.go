package packagingtools

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrSecretPath is the read_file refusal for a path that normally holds
// credentials. The file is not opened.
var ErrSecretPath = errors.New("denied: secret-bearing path")

// redactedMarker replaces a credential found in returned file content.
const redactedMarker = "[REDACTED]"

var (
	secretBaseNames = map[string]bool{
		"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
		".netrc": true, "_netrc": true, ".npmrc": true, ".pypirc": true,
		".git-credentials": true, ".htpasswd": true, ".dockercfg": true,
		"kubeconfig": true,
	}
	secretExtensions = map[string]bool{
		".pem": true, ".key": true, ".p12": true, ".pfx": true, ".jks": true,
		".keystore": true, ".tfstate": true, ".kubeconfig": true,
	}
	// Any path under one of these directories is refused.
	secretDirNames = map[string]bool{".git": true, ".ssh": true, ".aws": true, ".gnupg": true}
)

// secretPath reports whether a repository-relative path is one of the
// well-known credential locations. Matching ignores case.
func secretPath(rel string) bool {
	segments := strings.Split(strings.ToLower(filepath.ToSlash(filepath.Clean(rel))), "/")
	base := segments[len(segments)-1]
	for _, dir := range segments[:len(segments)-1] {
		if secretDirNames[dir] {
			return true
		}
	}
	if len(segments) > 1 {
		parent := segments[len(segments)-2]
		if (parent == ".kube" && base == "config") || (parent == ".docker" && base == "config.json") {
			return true
		}
	}
	switch {
	case secretBaseNames[base], secretExtensions[filepath.Ext(base)]:
		return true
	case base == ".env", strings.HasPrefix(base, ".env."), strings.HasSuffix(base, ".env"):
		return true
	case strings.HasPrefix(base, "kubeconfig."), strings.Contains(base, ".tfstate."):
		return true
	}
	return false
}

var (
	pemPrivateKey = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)
	knownTokens   = regexp.MustCompile(`\b(?:(?:AKIA|ASIA)[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,})`)
	bearerToken   = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]{16,}`)
	urlPassword   = regexp.MustCompile(`(://[^/\s:@]+:)[^@\s/]+@`)
	// A key that ends in a credential word, then = or :, then one value.
	secretAssignment = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|secret[_-]?key|private[_-]?key)["']?[ \t]*(?::=|[:=])[ \t]*)("[^"\n]*"|'[^'\n]*'|[^\s"',;]+)`)
)

// redactSecrets replaces obvious credentials in file content with
// redactedMarker. It is a best-effort filter, not a secret scanner.
func redactSecrets(content string) string {
	content = pemPrivateKey.ReplaceAllString(content, redactedMarker)
	content = knownTokens.ReplaceAllString(content, redactedMarker)
	content = bearerToken.ReplaceAllString(content, "${1}"+redactedMarker)
	content = urlPassword.ReplaceAllString(content, "${1}"+redactedMarker+"@")
	return secretAssignment.ReplaceAllStringFunc(content, func(match string) string {
		parts := secretAssignment.FindStringSubmatch(match)
		if placeholderValue(parts[2]) {
			return match
		}
		return parts[1] + redactedMarker
	})
}

// placeholderValue is a value that names where a secret comes from rather
// than being one: empty, a variable reference, a template, or a literal.
func placeholderValue(value string) bool {
	value = strings.Trim(value, `"'`)
	switch strings.ToLower(value) {
	case "", "true", "false", "null", "nil", "none", redactedMarker:
		return true
	}
	return strings.HasPrefix(value, "$") || strings.HasPrefix(value, "<") || strings.HasPrefix(value, "{{") || strings.HasPrefix(value, "[")
}
