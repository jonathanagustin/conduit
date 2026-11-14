// Package packagingskill is the packaging skill file a proposer is given.
package packagingskill

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// Source is the exact bytes of SKILL.md.
//
//go:embed SKILL.md
var Source string

// SHA256 is the lowercase hex digest of Source.
func SHA256() string {
	sum := sha256.Sum256([]byte(Source))
	return hex.EncodeToString(sum[:])
}
