package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

var (
	errInvalidAuthorizationAuditCursor = errors.New("invalid authorization audit cursor")
	// ErrInvalidAuthorizationAuditPosition is returned by a repository handed a
	// position it did not issue.
	ErrInvalidAuthorizationAuditPosition = errors.New("invalid authorization audit position")
)

// AuthorizationAuditPageQuery bounds one read of the authorization audit.
// Since is inclusive and Until exclusive; a zero time leaves that side open.
// After is the Next of an earlier page from the same repository and query.
type AuthorizationAuditPageQuery struct {
	ResourceRID  string
	Since, Until time.Time
	After        string
	Limit        int
}

// AuthorizationAuditPage is one bounded window, oldest first. Next is set only
// when more events remain. Total counts every event the filters match.
type AuthorizationAuditPage struct {
	Events []AuthorizationAuditEvent
	Next   string
	Total  int
}

// auditPosition is a strict seek key. PostgreSQL pages the chained log by
// sequence, which is the hash-chain order. Stores without a chain, and legacy
// records awaiting adoption, are ordered by event time and then ID: the order
// adoption will chain them in.
type auditPosition struct {
	timed    bool
	sequence uint64
	at       time.Time
	id       string
}

func (position auditPosition) String() string {
	if position.timed {
		return "t:" + position.at.UTC().Format(time.RFC3339Nano) + "|" + position.id
	}
	return "s:" + strconv.FormatUint(position.sequence, 10)
}

func parseAuditPosition(raw string) (auditPosition, error) {
	switch {
	case raw == "":
		return auditPosition{}, nil
	case strings.HasPrefix(raw, "s:"):
		sequence, err := strconv.ParseUint(raw[2:], 10, 63)
		if err != nil || sequence == 0 {
			return auditPosition{}, ErrInvalidAuthorizationAuditPosition
		}
		return auditPosition{sequence: sequence}, nil
	case strings.HasPrefix(raw, "t:"):
		stamp, id, found := strings.Cut(raw[2:], "|")
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if !found || err != nil {
			return auditPosition{}, ErrInvalidAuthorizationAuditPosition
		}
		return auditPosition{timed: true, at: at, id: id}, nil
	}
	return auditPosition{}, ErrInvalidAuthorizationAuditPosition
}

func auditEventBefore(left, right AuthorizationAuditEvent) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		return left.ID < right.ID
	}
	return left.CreatedAt.Before(right.CreatedAt)
}

func auditEventInWindow(event AuthorizationAuditEvent, query AuthorizationAuditPageQuery) bool {
	if query.ResourceRID != "" && event.ResourceRID != query.ResourceRID {
		return false
	}
	if !query.Since.IsZero() && event.CreatedAt.Before(query.Since) {
		return false
	}
	return query.Until.IsZero() || event.CreatedAt.Before(query.Until)
}

// pageTimedAuditEvents cuts one page from events that are already filtered
// and sorted by auditEventBefore. When seek is set the page starts strictly
// after the given position; otherwise it starts at the oldest event.
func pageTimedAuditEvents(matching []AuthorizationAuditEvent, after auditPosition, seek bool, limit int) ([]AuthorizationAuditEvent, string) {
	start := 0
	if seek {
		start = sort.Search(len(matching), func(index int) bool {
			return auditEventBefore(AuthorizationAuditEvent{ID: after.id, CreatedAt: after.at}, matching[index])
		})
	}
	end := min(start+limit, len(matching))
	next := ""
	if end < len(matching) && end > start {
		last := matching[end-1]
		next = auditPosition{timed: true, at: last.CreatedAt, id: last.ID}.String()
	}
	return matching[start:end], next
}

func validateAuditPageQuery(query AuthorizationAuditPageQuery) (auditPosition, error) {
	if query.Limit < 1 {
		return auditPosition{}, errors.New("authorization audit page limit must be positive")
	}
	return parseAuditPosition(query.After)
}

// PageAuthorizationAudit selects, orders and cuts the page under the lock and
// copies only the events it returns.
func (repository *MemoryRepository) PageAuthorizationAudit(_ context.Context, query AuthorizationAuditPageQuery) (AuthorizationAuditPage, error) {
	after, err := validateAuditPageQuery(query)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	if query.After != "" && !after.timed {
		return AuthorizationAuditPage{}, ErrInvalidAuthorizationAuditPosition
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	matching := make([]AuthorizationAuditEvent, 0)
	for _, event := range repository.authorizationAudit {
		if auditEventInWindow(event, query) {
			matching = append(matching, event)
		}
	}
	sort.Slice(matching, func(i, j int) bool { return auditEventBefore(matching[i], matching[j]) })
	window, next := pageTimedAuditEvents(matching, after, query.After != "", query.Limit)
	page := AuthorizationAuditPage{Events: make([]AuthorizationAuditEvent, 0, len(window)), Next: next, Total: len(matching)}
	for _, event := range window {
		page.Events = append(page.Events, cloneJSON(event))
	}
	return page, nil
}

// PageAuthorizationAudit bounds what the development file store returns. The
// directory is still read in full: this store has no index to seek.
func (repository *FileRepository) PageAuthorizationAudit(_ context.Context, query AuthorizationAuditPageQuery) (AuthorizationAuditPage, error) {
	after, err := validateAuditPageQuery(query)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	if query.After != "" && !after.timed {
		return AuthorizationAuditPage{}, ErrInvalidAuthorizationAuditPosition
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	matching, err := listJSON[AuthorizationAuditEvent](
		repository.authorizationAuditDirectory(),
		func(event AuthorizationAuditEvent) bool { return auditEventInWindow(event, query) },
		auditEventBefore,
	)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	window, next := pageTimedAuditEvents(matching, after, query.After != "", query.Limit)
	return AuthorizationAuditPage{Events: append([]AuthorizationAuditEvent{}, window...), Next: next, Total: len(matching)}, nil
}

// PageAuthorizationAudit reads one page of the chained log by sequence. Legacy
// records an older Hub left in the snapshot follow the chained ones, in the
// order adoption will append them, so they stay readable during a rollout
// without the whole log being loaded to merge them.
func (view *authorizationAuditView) PageAuthorizationAudit(ctx context.Context, query AuthorizationAuditPageQuery) (AuthorizationAuditPage, error) {
	after, err := validateAuditPageQuery(query)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	bounds := auditpostgres.PageQuery{Resource: query.ResourceRID, Since: query.Since, Until: query.Until}
	total, err := view.audit.CountAuthorizationAudit(ctx, bounds)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	legacy, err := view.Repository.ListAuthorizationAudit(ctx, query.ResourceRID)
	if err != nil {
		return AuthorizationAuditPage{}, err
	}
	pending := make([]AuthorizationAuditEvent, 0, len(legacy))
	for _, event := range legacy {
		if !auditEventInWindow(event, query) {
			continue
		}
		chained, err := view.audit.HasAuthorizationAuditEvent(ctx, event.ID)
		if err != nil {
			return AuthorizationAuditPage{}, err
		}
		if !chained {
			pending = append(pending, event)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return auditEventBefore(pending[i], pending[j]) })
	page := AuthorizationAuditPage{Events: []AuthorizationAuditEvent{}, Total: total + len(pending)}
	seekPending := after.timed && (after.id != "" || !after.at.IsZero())
	if !after.timed {
		bounds.AfterSequence, bounds.Limit = after.sequence, query.Limit+1
		rows, err := view.audit.PageAuthorizationAudit(ctx, bounds)
		if err != nil {
			return AuthorizationAuditPage{}, err
		}
		if len(rows) > query.Limit {
			rows = rows[:query.Limit]
			page.Next = auditPosition{sequence: rows[len(rows)-1].Sequence}.String()
		}
		for _, row := range rows {
			page.Events = append(page.Events, row.Event)
		}
		if page.Next != "" || len(pending) == 0 {
			return page, nil
		}
		if len(page.Events) == query.Limit {
			// The chained log ended exactly on the page boundary: the next
			// page starts at the oldest pending legacy record.
			page.Next = auditPosition{timed: true}.String()
			return page, nil
		}
	}
	window, next := pageTimedAuditEvents(pending, after, seekPending, query.Limit-len(page.Events))
	page.Events, page.Next = append(page.Events, window...), next
	return page, nil
}

func (repository *PostgresRepository) PageAuthorizationAudit(ctx context.Context, query AuthorizationAuditPageQuery) (AuthorizationAuditPage, error) {
	var result AuthorizationAuditPage
	err := repository.withStateTransaction(ctx, false, func(memory *MemoryRepository, transaction *sql.Tx) error {
		var err error
		result, err = (&authorizationAuditView{Repository: memory, audit: auditpostgres.New(transaction)}).PageAuthorizationAudit(ctx, query)
		return err
	})
	return result, err
}

// authorizationAuditRead is the parsed, validated query of the HTTP read.
type authorizationAuditRead struct {
	query AuthorizationAuditPageQuery
	chain bool
	// previous is the projection checkpoint the requested page continues
	// from. It is meaningful only for the chain view.
	previous    governance.AuditCheckpoint
	hasPrevious bool
	fingerprint string
}

type authorizationAuditFilter struct {
	ResourceRID string `json:"resourceRid"`
	Since       string `json:"since"`
	Until       string `json:"until"`
	View        string `json:"view"`
}

func parseAuditBound(query url.Values, name string) (time.Time, string, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return time.Time{}, "", nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%s must be an RFC 3339 timestamp", name)
	}
	return parsed, parsed.UTC().Format(time.RFC3339Nano), nil
}

func parseAuthorizationAuditRead(values url.Values) (authorizationAuditRead, error) {
	view := strings.TrimSpace(values.Get("view"))
	if view != "" && view != "events" && view != "chain" {
		return authorizationAuditRead{}, errors.New("view must be events or chain")
	}
	rawCursor, limit, err := parsePageQuery(values, defaultPageLimit, maxPageLimit)
	if err != nil {
		return authorizationAuditRead{}, err
	}
	read := authorizationAuditRead{chain: view == "chain", previous: governance.EmptyAuditCheckpoint()}
	read.query = AuthorizationAuditPageQuery{ResourceRID: values.Get("resourceRid"), Limit: limit}
	filter := authorizationAuditFilter{ResourceRID: read.query.ResourceRID, View: "events"}
	if read.chain {
		filter.View = "chain"
	}
	if read.query.Since, filter.Since, err = parseAuditBound(values, "since"); err != nil {
		return authorizationAuditRead{}, err
	}
	if read.query.Until, filter.Until, err = parseAuditBound(values, "until"); err != nil {
		return authorizationAuditRead{}, err
	}
	if !read.query.Since.IsZero() && !read.query.Until.IsZero() && !read.query.Since.Before(read.query.Until) {
		return authorizationAuditRead{}, errors.New("since must be before until")
	}
	read.fingerprint = fingerprintJSON(filter)
	if rawCursor == "" {
		return read, nil
	}
	cursor, err := decodeOpaqueSeekCursor(rawCursor)
	if err != nil || cursor.Version != 1 || cursor.Filter != read.fingerprint || cursor.Key == "" {
		return authorizationAuditRead{}, errInvalidAuthorizationAuditCursor
	}
	read.hasPrevious, read.query.After = true, cursor.Key
	if read.chain {
		// A chain cursor also carries the projection checkpoint its page
		// continues from: "<sequence>:<digest>:<position>".
		parts := strings.SplitN(cursor.Key, ":", 3)
		if len(parts) != 3 {
			return authorizationAuditRead{}, errInvalidAuthorizationAuditCursor
		}
		sequence, err := strconv.ParseUint(parts[0], 10, 63)
		read.previous = governance.AuditCheckpoint{Version: governance.AuditChainVersion, Sequence: sequence, Digest: parts[1]}
		if err != nil || sequence == 0 || read.previous.Validate() != nil {
			return authorizationAuditRead{}, errInvalidAuthorizationAuditCursor
		}
		read.query.After = parts[2]
	}
	if _, err := parseAuditPosition(read.query.After); err != nil || read.query.After == "" {
		return authorizationAuditRead{}, errInvalidAuthorizationAuditCursor
	}
	return read, nil
}

// readAuthorizationAuditPage answers GET /v1/authorization-audit. The page is
// forward-only: hasPrevious is reported and previousCursor is never issued, so
// a client returns to the first page rather than seeking backwards.
func readAuthorizationAuditPage(ctx context.Context, repository GovernanceRepository, read authorizationAuditRead) (map[string]any, error) {
	page, err := repository.PageAuthorizationAudit(ctx, read.query)
	if errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
		return nil, errInvalidAuthorizationAuditCursor
	}
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"total": page.Total, "pageSize": len(page.Events), "limit": read.query.Limit, "hasPrevious": read.hasPrevious,
	}
	nextKey := page.Next
	if read.chain {
		entries, err := governance.ChainAuthorizationAuditFrom(read.previous, page.Events)
		if err != nil {
			return nil, err
		}
		body["entries"] = entries
		if nextKey != "" && len(entries) > 0 {
			last := entries[len(entries)-1]
			nextKey = strconv.FormatUint(last.Sequence, 10) + ":" + last.Digest + ":" + nextKey
		}
	} else {
		body["events"] = page.Events
	}
	if nextKey != "" {
		body["nextCursor"] = encodeOpaqueSeekCursor(nextKey, read.fingerprint)
	}
	return body, nil
}
