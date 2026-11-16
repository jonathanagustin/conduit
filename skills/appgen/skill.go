// Package appgenskill is the problem-statement application generation skill file.
package appgenskill

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// Version is the appgen skill version recorded in a planning binding.
// Bump it whenever SKILL.md changes in a way an operator should notice.
const Version = "1.0.0"

// Source is the exact bytes of SKILL.md.
//
//go:embed SKILL.md
var Source string

// SHA256 is the lowercase hex digest of Source.
func SHA256() string {
	return SHA256From(Source)
}

// SHA256From returns the lowercase hex digest of a skill source string.
func SHA256From(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
