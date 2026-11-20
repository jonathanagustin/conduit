package application

import (
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// ErrInvalidAuditRead distinguishes public query refusal from storage failure.
var ErrInvalidAuditRead = errors.New("authorization audit query is invalid")

// These refusals distinguish carrier authorization from repository failures.
var (
	ErrAuditReadUnauthenticated = errors.New("authorization audit authentication required")
	ErrAuditReadForbidden       = errors.New("authorization audit administrator required")
)

// AuditEventReadQuery is a bounded event-view request, not chain authority.
type AuditEventReadQuery struct {
	Limit               *int
	Cursor, ResourceRID string
	Since, Until        *time.Time
	Descending          bool
}

// AuditEventReadPage retains native forward-seek metadata and exact events.
type AuditEventReadPage struct {
	Events                 []domain.AuthorizationAuditEvent
	Total, PageSize, Limit int
	NextCursor             string
	HasPrevious            bool
}

// AuditChainReadQuery selects the native filtered projection, not the raw ledger.
type AuditChainReadQuery AuditEventReadQuery

// AuditChainReadPage retains exact projection digest material and seek metadata.
type AuditChainReadPage struct {
	Entries                []domain.AuthorizationAuditChainEntry
	Total, PageSize, Limit int
	NextCursor             string
	HasPrevious            bool
}
