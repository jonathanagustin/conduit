package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MediaType = "application/vnd.conduit.plan-proposal.v1+json"
const Native = "CONDUIT_NATIVE"

// CONDUIT-INVENTED: planreview.ProposalEnvelope
// Displaced-by: a published or sanitized proposal contract establishing transport and persistence of review context.
type Review struct {
	Title                  string `json:"title"`
	Rationale              string `json:"rationale"`
	ValueTier              string `json:"valueTier"`
	BaseDesiredStateDigest string `json:"baseDesiredStateDigest"`
}

func (review Review) Validate() error {
	if review.ValueTier != Native || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(review.BaseDesiredStateDigest) || !validText(review.Title, 1024) || !validText(review.Rationale, 16384) {
		return errors.New("invalid Conduit-native Plan review context")
	}
	return nil
}

func validText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	})
}
