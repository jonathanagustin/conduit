// Package domain owns Conduit's native Space identity. A Space is neither a
// Foundry namespace nor an Apollo-issued identity, and conveys no resource grant.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalid   = errors.New("invalid Space")
	ErrNotFound  = errors.New("Space not found")
	ErrConflict  = errors.New("Space identity or assignment already exists")
	ErrForbidden = errors.New("Space operation forbidden")
	idPattern    = regexp.MustCompile(`^space_[0-9a-f]{32}$`)
)

// Space is a persisted, Conduit-owned grouping, not a synthesized field value.
// CONDUIT-INVENTED: spaces.NativeGrouping
// Displaced-by: a sanitized Apollo Space record establishing identity, grouping metadata and ownership semantics, including guest-user publication.
type Space struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	ValueTier   string    `json:"valueTier"`
}

func New(id, name, description, subject string, now time.Time) (Space, error) {
	value := Space{ID: id, Name: strings.TrimSpace(name), Description: description, CreatedBy: subject, CreatedAt: now.UTC(), ValueTier: "CONDUIT_NATIVE"}
	return value, value.Validate()
}

func ValidID(id string) bool { return idPattern.MatchString(id) }

func (space Space) Validate() error {
	if !ValidID(space.ID) {
		return fmt.Errorf("%w: expected a Conduit Space identity", ErrInvalid)
	}
	if space.Name == "" || space.Name != strings.TrimSpace(space.Name) || len(space.Name) > 256 || strings.ContainsFunc(space.Name, unicode.IsControl) {
		return fmt.Errorf("%w: name must be nonempty text of at most 256 bytes", ErrInvalid)
	}
	if len(space.Description) > 4096 {
		return fmt.Errorf("%w: description exceeds 4096 bytes", ErrInvalid)
	}
	if space.CreatedBy == "" || space.CreatedBy != strings.TrimSpace(space.CreatedBy) || strings.ContainsFunc(space.CreatedBy, unicode.IsControl) || space.CreatedAt.IsZero() {
		return fmt.Errorf("%w: creation provenance is required", ErrInvalid)
	}
	if space.ValueTier != "CONDUIT_NATIVE" {
		return fmt.Errorf("%w: native provenance is required", ErrInvalid)
	}
	return nil
}
