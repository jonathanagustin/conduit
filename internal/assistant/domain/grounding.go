package domain

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// CONDUIT-INVENTED: assistant.GroundedSources
// Displaced-by: An authoritative assistant contract for documentation retrieval, authorized ontology citations and retained source evidence.

const (
	// GroundingSchema identifies the optional native source metadata.
	GroundingSchema = "conduit.assistant-grounding/v1"
	// SourceDocumentation identifies selected native product references.
	SourceDocumentation = "operator-documentation"
	// SourceOntology identifies a permission-filtered ontology observation.
	SourceOntology = "ontology-observation"
	// MaxCitations bounds the retained source set of one turn.
	MaxCitations = 8
	// MaxCitationsPerRead bounds excerpts attached to one tool result.
	MaxCitationsPerRead = 3
	// MaxCitationQuoteBytes bounds one exact source excerpt.
	MaxCitationQuoteBytes = 512
	// MaxDocumentationBytes bounds one admitted embedded reference.
	MaxDocumentationBytes = 512 << 10
)

// Citation identifies an exact retrieved excerpt or authorized object summary.
// It is made by server read adapters, never accepted from a model reply.
type Citation struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	ValueTier      string    `json:"valueTier"`
	Title          string    `json:"title"`
	Source         string    `json:"source"`
	Quote          string    `json:"quote"`
	ExcerptSHA256  string    `json:"excerptSha256"`
	DocumentSHA256 string    `json:"documentSha256,omitempty"`
	FirstLine      int       `json:"firstLine,omitempty"`
	LastLine       int       `json:"lastLine,omitempty"`
	StartByte      int       `json:"startByte,omitempty"`
	EndByte        int       `json:"endByte,omitempty"`
	RetrievedAt    time.Time `json:"retrievedAt"`
	AuditEventID   string    `json:"auditEventId"`
}

// NewCitation binds the excerpt's content and coordinates, without inventing
// a retrieval time or audit receipt. The application adds those after its read.
func NewCitation(source Citation) (Citation, error) {
	source.ValueTier = "CONDUIT_NATIVE"
	source.ExcerptSHA256 = Digest([]byte(source.Quote))
	source.ID = source.identity()
	if !source.Valid() {
		return Citation{}, ErrToolInvalid
	}
	return source, nil
}

func (source Citation) identity() string {
	encoded, _ := json.Marshal([]any{source.Kind, source.Title, source.Source, source.ExcerptSHA256, source.DocumentSHA256, source.FirstLine, source.LastLine, source.StartByte, source.EndByte})
	return "csrc_" + Digest(encoded)
}

// Valid checks source structure and the exact excerpt identity. Receipt/time
// checks are added by Grounding.Valid after the application completes auditing.
func (source Citation) Valid() bool {
	bounded := func(value string, max int) bool { return value != "" && len(value) <= max && utf8.ValidString(value) }
	if source.ValueTier != "CONDUIT_NATIVE" || !bounded(source.Title, 256) || !bounded(source.Source, 512) || !bounded(source.Quote, MaxCitationQuoteBytes) || source.ExcerptSHA256 != Digest([]byte(source.Quote)) || source.ID != source.identity() {
		return false
	}
	switch source.Kind {
	case SourceDocumentation:
		return len(source.DocumentSHA256) == 64 && strings.Trim(source.DocumentSHA256, "0123456789abcdef") == "" && source.FirstLine > 0 && source.LastLine >= source.FirstLine && source.LastLine <= MaxDocumentationBytes && source.StartByte >= 0 && source.StartByte <= MaxDocumentationBytes && source.EndByte >= source.StartByte && source.EndByte <= MaxDocumentationBytes && source.EndByte-source.StartByte == len(source.Quote)
	case SourceOntology:
		return source.DocumentSHA256 == "" && source.FirstLine == 0 && source.LastLine == 0 && source.StartByte == 0 && source.EndByte == 0
	}
	return false
}

// Grounding records retrieved source excerpts, not a claim that every answer
// statement is supported. A nil field on older messages means no metadata.
type Grounding struct {
	SchemaVersion string     `json:"schemaVersion"`
	Sources       []Citation `json:"sources"`
	Truncated     bool       `json:"truncated"`
}

// NewGrounding distinguishes an observed empty source set from old documents.
func NewGrounding() *Grounding {
	return &Grounding{SchemaVersion: GroundingSchema, Sources: []Citation{}}
}

// Add retains bounded, unique, audited source records in retrieval order.
func (grounding *Grounding) Add(sources []Citation, limited bool) {
	grounding.Truncated = grounding.Truncated || limited
	for _, source := range sources {
		if !source.Valid() || source.AuditEventID == "" || len(source.AuditEventID) > 256 || source.RetrievedAt.IsZero() {
			grounding.Truncated = true
			continue
		}
		found := false
		for _, old := range grounding.Sources {
			if old.ID == source.ID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if len(grounding.Sources) >= MaxCitations {
			grounding.Truncated = true
			continue
		}
		grounding.Sources = append(grounding.Sources, source)
	}
}

// Clone gives stored and returned messages independent source slices.
func (grounding *Grounding) Clone() *Grounding {
	if grounding == nil {
		return nil
	}
	out := *grounding
	out.Sources = append([]Citation{}, grounding.Sources...)
	return &out
}

// Valid refuses malformed retained source evidence; legacy nil remains valid.
func (grounding *Grounding) Valid() bool {
	if grounding == nil {
		return true
	}
	if grounding.SchemaVersion != GroundingSchema || len(grounding.Sources) > MaxCitations {
		return false
	}
	seen := map[string]bool{}
	for _, source := range grounding.Sources {
		if !source.Valid() || source.AuditEventID == "" || len(source.AuditEventID) > 256 || source.RetrievedAt.IsZero() || seen[source.ID] {
			return false
		}
		seen[source.ID] = true
	}
	return true
}
