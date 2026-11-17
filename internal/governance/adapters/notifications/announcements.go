// Package notificationsadapter translates two things Governance commits -- a
// change request that starts waiting for an approver, and a resource grant that
// changed -- into notification events. It is the only place where Governance's
// vocabulary and the notification vocabulary meet.
package notificationsadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/governance/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// ErrNoEnvironment reports a change request that names no Environment, and so
// has no resource anybody could have subscribed to. It is an error rather than
// a silent skip so that the composition root can log it; it is a sentinel so
// that it can log it quietly, because a catalogue-wide request is routine.
var ErrNoEnvironment = errors.New("the change request names no Environment to announce it on")

// Announcements raises Governance's two notification events.
//
// Both type names are Apollo's. Which Conduit write raises which, and on what
// resource, is Conduit's: no capture shows what Apollo does when a change
// request starts waiting or a role assignment changes.
// CONDUIT-INVENTED: governance.NotificationEventSelection
// Displaced-by: a capture of which resource Apollo raises CHANGE_REQUEST_PENDING_APPROVAL and ROLE_CHANGE on, who receives each, and what separates ROLE_CHANGE from RBAC_CHANGE
type Announcements struct {
	emitter notificationports.Emitter
	now     func() time.Time
}

var (
	_ ports.ChangeRequestOpenings = (*Announcements)(nil)
	_ ports.GrantChanges          = (*Announcements)(nil)
)

// NewAnnouncements refuses an announcer that could not announce. The clock is
// injected for the one case below that has no moment of its own.
func NewAnnouncements(emitter notificationports.Emitter, now func() time.Time) (*Announcements, error) {
	if emitter == nil || now == nil {
		return nil, errors.New("a notification emitter and a clock are required to announce Governance changes")
	}
	return &Announcements{emitter: emitter, now: now}, nil
}

// AwaitingApproval tells whoever subscribed to the Environment that a change
// request is waiting for an approver.
//
// The event is about the Environment, and that is a limitation stated rather
// than hidden. The people who can approve are named by the request's reviewer
// requirements, but a notification reaches a subscriber, not a role: nothing in
// the notification model addresses "the teams that could satisfy this
// requirement", and resolving one here would be a second fan-out rule. So an
// approver hears about this by subscribing to the Environment on the
// CHANGE_REQUEST_PENDING_APPROVAL type, which is the shape Apollo's own
// settings page gives it. A request that names no Environment has no resource
// to join on and is reported as ErrNoEnvironment rather than announced on an
// identifier nobody could have chosen.
func (announcements *Announcements) AwaitingApproval(
	ctx context.Context, opening ports.ChangeRequestOpening,
) error {
	if strings.TrimSpace(opening.ID) == "" {
		return errors.New("announce change request: the request has no id")
	}
	environment := strings.TrimSpace(opening.Environment)
	if environment == "" {
		return fmt.Errorf("announce change request %s: %w", opening.ID, ErrNoEnvironment)
	}
	at := opening.At
	if at.IsZero() {
		at = announcements.now()
	}
	title := strings.TrimSpace(opening.Title)
	if title == "" {
		title = strings.ToLower(opening.TargetKind) + " " + opening.TargetID
	}
	return announcements.emitter.Emit(ctx, notifications.Event{
		// The revision is part of the identity and the moment is not. A
		// proposal restated against a request that already exists hands back
		// the stored record at the stored revision, which is the same wait and
		// one event; a request rejected and reopened comes back at a later
		// revision, which is a second wait and a second event.
		ID:          changeRequestEventID(opening.ID, opening.Revision),
		Type:        notifications.EventChangeRequestPendingApproval,
		ResourceRID: apolloapplication.EnvironmentRID(environment),
		Environment: environment,
		Summary: fmt.Sprintf("Change request %s (%s) in Environment %s is waiting for approval.",
			opening.ID, bounded(title), environment),
		Actor:      opening.Author,
		OccurredAt: at.UTC(),
	})
}

// GrantChanged tells whoever subscribed to a resource that who may act on it
// changed.
//
// The type is ROLE_CHANGE and not RBAC_CHANGE. Apollo lists the first on a
// resource's own settings page and the second in the cross-resource dialog,
// and notifications/domain records that nothing establishes they are one event
// at two scopes. A grant is a write against exactly one resource, which is the
// first; raising both for one write would assert the identity the domain
// declines to. RBAC_CHANGE therefore has no emit site.
//
// The event is about the resource the grant names, verbatim: that is the
// identifier the write was authorized against, so it is one a subscriber can
// be authorized on too. A write that left the grant's authority as it was is
// not announced -- re-saving a grant is not a role change.
func (announcements *Announcements) GrantChanged(ctx context.Context, change ports.GrantChange) error {
	after := change.After
	if strings.TrimSpace(after.ID) == "" || strings.TrimSpace(after.ResourceRID) == "" {
		return errors.New("announce grant change: the grant names no id or no resource")
	}
	if change.Before != nil && sameAuthority(*change.Before, after) {
		return nil
	}
	at := after.UpdatedAt
	if at.IsZero() {
		at = announcements.now()
	}
	return announcements.emitter.Emit(ctx, notifications.Event{
		ID:          grantEventID(after.ID, after.Revision),
		Type:        notifications.EventRoleChange,
		ResourceRID: after.ResourceRID,
		Summary: fmt.Sprintf("Team %s on %s: %s; was: %s.",
			after.Team, after.ResourceRID, authority(&after), authority(change.Before)),
		Actor:      change.Actor,
		OccurredAt: at.UTC(),
	})
}

func sameAuthority(before, after domain.ResourceGrant) bool {
	return before.Relationship == after.Relationship &&
		slices.Equal(sorted(before.Roles), sorted(after.Roles)) &&
		slices.Equal(sorted(before.Operations), sorted(after.Operations))
}

func sorted(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

// authority states what a grant confers. The before half is stated as well as
// the after, for the reason the audit record keeps both: an event saying only
// what a grant became cannot say whether authority was added or removed.
func authority(grant *domain.ResourceGrant) string {
	if grant == nil {
		return "no grant"
	}
	parts := []string{}
	if grant.Relationship != "" {
		parts = append(parts, "relationship "+grant.Relationship)
	}
	if len(grant.Roles) > 0 {
		parts = append(parts, "roles "+strings.Join(sorted(grant.Roles), ", "))
	}
	if len(grant.Operations) > 0 {
		parts = append(parts, "operations "+strings.Join(sorted(grant.Operations), ", "))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return bounded(strings.Join(parts, "; "))
}

func changeRequestEventID(id string, revision uint64) string {
	digest := sha256.Sum256([]byte(
		"change-request-awaiting-approval\x00" + id + "\x00" + strconv.FormatUint(revision, 10)))
	return "nev_" + hex.EncodeToString(digest[:16])
}

func grantEventID(id string, revision uint64) string {
	digest := sha256.Sum256([]byte("resource-grant-changed\x00" + id + "\x00" + strconv.FormatUint(revision, 10)))
	return "nev_" + hex.EncodeToString(digest[:16])
}

// bounded keeps free text to what an inbox row can hold, on a rune boundary.
func bounded(text string) string {
	const maximum = 256
	if len(text) <= maximum {
		return text
	}
	runes := []rune(text)
	for len(string(runes)) > maximum {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
