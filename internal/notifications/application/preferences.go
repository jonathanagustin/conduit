package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// ErrNoPreference is the answer to "what has this owner asked for on this
// resource and channel" when they have asked for nothing.
//
// Distinguished from an empty preference on purpose. A stored preference that
// enables no types cannot exist -- Validate refuses one -- so "absent" and
// "enables nothing" are different states, and a caller that renders a settings
// page needs to know which it is looking at: one is a page never opened, the
// other is impossible.
var ErrNoPreference = errors.New("no notification preference is stored for that owner, resource and channel")

// Preferences reads and writes one owner's settings at exactly the grain the
// store keys on: (resource, owner, channel).
//
// That grain is not an implementation detail leaking upward -- it is Apollo's.
// The settings page configures email and in-app independently over the same
// list of types, for one owner, on one resource, and saving it twice must leave
// one row. Editing at any coarser grain would make a caller read-modify-write a
// set it does not own.
type Preferences struct {
	store ports.Store
	clock ports.Clock
}

// The clock is ports.Clock rather than a bare func() time.Time because the port
// already declares itself the only source of now in this context, and the
// delivery dispatcher that landed alongside this reads it. Two spellings of the
// same dependency in one package is how a later reader ends up wondering which
// one a given component honours.
func NewPreferences(store ports.Store, clock ports.Clock) (*Preferences, error) {
	if store == nil {
		return nil, errors.New("a notification store is required to read or edit preferences")
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Preferences{store: store, clock: clock}, nil
}

// Get returns one owner's settings for one resource on one channel, or
// ErrNoPreference.
func (preferences *Preferences) Get(
	ctx context.Context, resourceRID string, owner domain.Owner, channel domain.Channel,
) (domain.Preference, error) {
	if err := validateGrain(resourceRID, owner, channel); err != nil {
		return domain.Preference{}, err
	}
	stored, err := preferences.store.PreferencesFor(ctx, resourceRID)
	if err != nil {
		return domain.Preference{}, fmt.Errorf("read notification preferences for %s: %w", resourceRID, err)
	}
	for _, preference := range stored {
		if preference.Owner == owner && preference.Channel == channel {
			return preference, nil
		}
	}
	return domain.Preference{}, ErrNoPreference
}

// Save stores one owner's settings, replacing whatever that owner had for the
// same resource and channel.
//
// The identifier, the timestamps and the revision are minted here rather than
// accepted from the caller. A caller that could choose the ID could write two
// rows for one tuple on a store keyed differently, and a caller that could
// choose CreatedAt could make a preference predate the resource it is about.
// The ID is derived from the tuple, so the same edit sent twice is the same
// identifier, and a client that lost the response does not mint a second one.
func (preferences *Preferences) Save(
	ctx context.Context, proposed domain.Preference,
) (domain.Preference, error) {
	if err := validateGrain(proposed.ResourceRID, proposed.Owner, proposed.Channel); err != nil {
		return domain.Preference{}, err
	}
	now := preferences.clock.Now().UTC()
	stored := domain.Preference{
		ID:             PreferenceID(proposed.ResourceRID, proposed.Owner, proposed.Channel),
		Owner:          proposed.Owner,
		ResourceRID:    proposed.ResourceRID,
		Channel:        proposed.Channel,
		Types:          proposed.Types,
		AllFutureTypes: proposed.AllFutureTypes,
		ContactID:      proposed.ContactID,
		CreatedAt:      now,
		UpdatedAt:      now,
		Revision:       1,
	}
	switch existing, err := preferences.Get(ctx, proposed.ResourceRID, proposed.Owner, proposed.Channel); {
	case err == nil:
		stored.CreatedAt = existing.CreatedAt
		stored.Revision = existing.Revision + 1
	case errors.Is(err, ErrNoPreference):
	default:
		return domain.Preference{}, err
	}
	if err := stored.Validate(); err != nil {
		return domain.Preference{}, err
	}
	if err := preferences.store.SavePreference(ctx, stored); err != nil {
		return domain.Preference{}, err
	}
	return stored, nil
}

// PreferenceID is the identifier the tuple always produces.
//
// A digest rather than a counter because it has to be the same on every replica
// without any of them storing it, and the same after a restart. It is opaque
// and Conduit's own: nothing published says what Apollo calls a preference.
func PreferenceID(resourceRID string, owner domain.Owner, channel domain.Channel) string {
	digest := sha256.Sum256([]byte(
		string(owner.Kind) + "\x00" + owner.ID + "\x00" + resourceRID + "\x00" + string(channel)))
	return "npref_" + hex.EncodeToString(digest[:12])
}

// validateGrain refuses a request that does not name one editable tuple. It is
// deliberately not Preference.Validate: that judges what a preference means,
// this judges whether the caller said which preference they meant at all.
func validateGrain(resourceRID string, owner domain.Owner, channel domain.Channel) error {
	if resourceRID == "" {
		return errors.New("a notification preference is about one resource, which was not named")
	}
	if owner.Kind != domain.PrincipalUser && owner.Kind != domain.PrincipalTeam {
		return errors.New("a notification preference belongs to a USER or a TEAM")
	}
	if owner.ID == "" {
		return errors.New("a notification preference requires the owner it belongs to")
	}
	if !channel.Valid() {
		return fmt.Errorf("notification channel %q is not one a resource can be configured on", channel)
	}
	return nil
}
