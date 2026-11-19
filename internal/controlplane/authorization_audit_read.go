package controlplane

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// AuthorizeAuthorizationAuditRead uses the same administrator decision as REST.
// It accepts the original resolved carrier, never a reconstructed represented user.
func AuthorizeAuthorizationAuditRead(ctx context.Context, service *GovernanceService, principal Principal, options HandlerOptions) error {
	if strings.TrimSpace(principal.Username) == "" {
		return ErrUnauthorized
	}
	if service == nil || !isGovernanceAdministrator(ctx, service, principal, options) {
		return ErrForbidden
	}
	return nil
}

// ReadAuthorizationAuditEvents shares the native event-view parser, filter-bound
// cursor and bounded repository query without re-entering the HTTP facade.
func ReadAuthorizationAuditEvents(ctx context.Context, repository GovernanceRepository, service *GovernanceService, principal Principal, options HandlerOptions, query governanceapp.AuditEventReadQuery) (governanceapp.AuditEventReadPage, error) {
	if err := AuthorizeAuthorizationAuditRead(ctx, service, principal, options); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return governanceapp.AuditEventReadPage{}, governanceapp.ErrAuditReadUnauthenticated
		}
		return governanceapp.AuditEventReadPage{}, governanceapp.ErrAuditReadForbidden
	}
	values := url.Values{"view": {"events"}, "resourceRid": {query.ResourceRID}, "cursor": {query.Cursor}}
	if query.Limit != nil {
		values.Set("limit", strconv.Itoa(*query.Limit))
	}
	if query.Since != nil {
		values.Set("since", query.Since.Format(time.RFC3339Nano))
	}
	if query.Until != nil {
		values.Set("until", query.Until.Format(time.RFC3339Nano))
	}
	if query.Descending {
		values.Set("order", "desc")
	}
	read, err := parseAuthorizationAuditRead(values)
	if err != nil {
		return governanceapp.AuditEventReadPage{}, governanceapp.ErrInvalidAuditRead
	}
	body, err := readAuthorizationAuditPage(ctx, repository, read)
	if errors.Is(err, errInvalidAuthorizationAuditCursor) {
		return governanceapp.AuditEventReadPage{}, governanceapp.ErrInvalidAuditRead
	}
	if err != nil {
		return governanceapp.AuditEventReadPage{}, err
	}
	page := governanceapp.AuditEventReadPage{Total: body["total"].(int), PageSize: body["pageSize"].(int), Limit: body["limit"].(int), HasPrevious: body["hasPrevious"].(bool)}
	page.Events, _ = body["events"].([]AuthorizationAuditEvent)
	page.NextCursor, _ = body["nextCursor"].(string)
	return page, nil
}

// ReadAuthorizationAuditChain shares the native chain projection parser and reader.
// Projection sequence and digests are not raw-ledger custody or trust anchors.
func ReadAuthorizationAuditChain(ctx context.Context, repository GovernanceRepository, service *GovernanceService, principal Principal, options HandlerOptions, query governanceapp.AuditChainReadQuery) (governanceapp.AuditChainReadPage, error) {
	if err := AuthorizeAuthorizationAuditRead(ctx, service, principal, options); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return governanceapp.AuditChainReadPage{}, governanceapp.ErrAuditReadUnauthenticated
		}
		return governanceapp.AuditChainReadPage{}, governanceapp.ErrAuditReadForbidden
	}
	values := url.Values{"view": {"chain"}, "resourceRid": {query.ResourceRID}, "cursor": {query.Cursor}}
	if query.Limit != nil {
		values.Set("limit", strconv.Itoa(*query.Limit))
	}
	if query.Since != nil {
		values.Set("since", query.Since.Format(time.RFC3339Nano))
	}
	if query.Until != nil {
		values.Set("until", query.Until.Format(time.RFC3339Nano))
	}
	if query.Descending {
		values.Set("order", "desc")
	}
	read, err := parseAuthorizationAuditRead(values)
	if err != nil {
		return governanceapp.AuditChainReadPage{}, governanceapp.ErrInvalidAuditRead
	}
	body, err := readAuthorizationAuditPage(ctx, repository, read)
	if errors.Is(err, errInvalidAuthorizationAuditCursor) {
		return governanceapp.AuditChainReadPage{}, governanceapp.ErrInvalidAuditRead
	}
	if err != nil {
		return governanceapp.AuditChainReadPage{}, err
	}
	page := governanceapp.AuditChainReadPage{Total: body["total"].(int), PageSize: body["pageSize"].(int), Limit: body["limit"].(int), HasPrevious: body["hasPrevious"].(bool)}
	page.Entries, _ = body["entries"].([]domain.AuthorizationAuditChainEntry)
	page.NextCursor, _ = body["nextCursor"].(string)
	return page, nil
}
