package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Identity subjects are opaque and compared exactly. They are never filenames
// or resource paths; do not impose resource-coordinate syntax on OIDC subjects.
func validIdentity(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
