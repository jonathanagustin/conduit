// Package packagingskill is the packaging skill file a proposer is given.
package packagingskill

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// Version is the packaging skill version recorded in a planning binding.
// Bump it whenever SKILL.md changes in a way an operator should notice.
const Version = "1.0.0"

// Source is the exact bytes of SKILL.md.
//
//go:embed SKILL.md
var Source string

// SHA256 is the lowercase hex digest of Source.
func SHA256() string {
	sum := sha256.Sum256([]byte(Source))
	return hex.EncodeToString(sum[:])
}
