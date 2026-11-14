// Package domain owns immutable discussion comments, independently of reviews.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid   = errors.New("invalid Change Request discussion")
	ErrNotFound  = errors.New("change request discussion not found")
	ErrForbidden = errors.New("change request discussion forbidden")
	ErrConflict  = errors.New("change request discussion conflicts")
	ErrCorrupt   = errors.New("change request discussion is inconsistent")
)

const (
	MaxTextBytes     = 16 << 10
	MaxDocumentBytes = 128 << 10
	MaxPageSize      = 8
)

// ValueTier distinguishes Conduit's discussion record and native transport.
// CONDUIT-INVENTED: changeactivity.Discussion
// Displaced-by: a published or sanitized Change Request discussion contract establishing record fields, comment authority, transport routes and pagination semantics.
const ValueTier = "CONDUIT_NATIVE"

var (
	requestPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	commentPattern = regexp.MustCompile(`^cmt_[a-f0-9]{32}$`)
)

func ValidRequestID(id string) bool { return requestPattern.MatchString(id) }
func ValidCommentID(id string) bool { return commentPattern.MatchString(id) }

type Comment struct {
	ID        string    `json:"id"`
	RequestID string    `json:"changeRequestId"`
	Author    string    `json:"author"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"createdAt"`
	Text      string    `json:"text"`
	ValueTier string    `json:"valueTier"`
}

func ValidText(text string) bool {
	return utf8.ValidString(text) && len(text) <= MaxTextBytes && strings.TrimSpace(text) != "" && !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' })
}
func (c Comment) Validate() error {
	if c.Actor == "" || !utf8.ValidString(c.Actor) || c.Actor != strings.TrimSpace(c.Actor) || len(c.Actor) > 512 || strings.ContainsFunc(c.Actor, unicode.IsControl) || !commentPattern.MatchString(c.ID) || !ValidRequestID(c.RequestID) || !utf8.ValidString(c.Author) || c.Author == "" || c.Author != strings.TrimSpace(c.Author) || len(c.Author) > 512 || strings.ContainsFunc(c.Author, unicode.IsControl) || c.CreatedAt.IsZero() || !ValidText(c.Text) || c.ValueTier != ValueTier {
		return ErrInvalid
	}
	return nil
}
