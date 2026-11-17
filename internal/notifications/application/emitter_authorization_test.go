package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type recipientCheck func(context.Context, domain.Owner, string) (bool, error)

func (check recipientCheck) Check(ctx context.Context, owner domain.Owner, resource string) (bool, error) {
	return check(ctx, owner, resource)
}

func TestAnEmitterRequiresCurrentRecipientEligibility(t *testing.T) {
	if _, err := NewEmitter(&fakeStore{}, nil); err == nil {
		t.Fatal("an emitter without recipient authorization was accepted")
	}
}

func TestOnlyCurrentlyEligibleMatchingRecipientsReachTheInbox(t *testing.T) {
	event := planFailed()
	store := &fakeStore{preferences: map[string][]domain.Preference{}}
	for _, owner := range []domain.Owner{{Kind: domain.PrincipalUser, ID: "alice"}, {Kind: domain.PrincipalTeam, ID: "former-readers"}} {
		for _, channel := range []domain.Channel{domain.ChannelEmail, domain.ChannelInApp} {
			store.preferences[event.ResourceRID] = append(store.preferences[event.ResourceRID], domain.Preference{
				ID: owner.ID + string(channel), Owner: owner, ResourceRID: event.ResourceRID,
				Channel: channel, AllFutureTypes: true,
			})
		}
	}
	calls := map[domain.Owner]int{}
	emitter, err := NewEmitter(store, recipientCheck(func(_ context.Context, owner domain.Owner, resource string) (bool, error) {
		if resource != event.ResourceRID {
			t.Fatalf("authorization checked another resource: %q", resource)
		}
		calls[owner]++
		return owner.ID == "alice", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || len(store.entries) != 2 {
		t.Fatalf("fan-out did not exclude the revoked team: events=%#v entries=%#v", store.events, store.entries)
	}
	for _, entry := range store.entries {
		if entry.Recipient.Owner.ID != "alice" {
			t.Fatalf("unauthorized recipient reached the inbox: %#v", entry)
		}
	}
	for owner, count := range calls {
		if count != 1 {
			t.Errorf("%s's channels used %d separate authority snapshots", owner.ID, count)
		}
	}
}

func TestAnUnknownRecipientAuthorityRecordsNeitherEventNorPartialAudience(t *testing.T) {
	event := planFailed()
	store := &fakeStore{preferences: map[string][]domain.Preference{event.ResourceRID: {
		{ID: "a", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, ResourceRID: event.ResourceRID, Channel: domain.ChannelEmail, AllFutureTypes: true},
		{ID: "b", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}, ResourceRID: event.ResourceRID, Channel: domain.ChannelEmail, AllFutureTypes: true},
	}}}
	unavailable := errors.New("current identity authority is unavailable")
	lookupFails := true
	emitter, err := NewEmitter(store, recipientCheck(func(_ context.Context, owner domain.Owner, _ string) (bool, error) {
		if owner.ID == "bob" && lookupFails {
			return false, unavailable
		}
		return true, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(t.Context(), event); !errors.Is(err, unavailable) {
		t.Fatalf("an uncertain audience did not report lookup failure: %v", err)
	}
	if len(store.events) != 0 || len(store.entries) != 0 {
		t.Fatalf("an uncertain lookup committed a partial audience: %#v %#v", store.events, store.entries)
	}
	lookupFails = false
	if err := emitter.Emit(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || len(store.entries) != 2 {
		t.Fatalf("retry did not record the complete audience: %#v %#v", store.events, store.entries)
	}
}

func TestAnEmitterCancelledDuringRecipientLookupRecordsNothing(t *testing.T) {
	event := planFailed()
	store := &fakeStore{preferences: map[string][]domain.Preference{event.ResourceRID: {{
		ID: "pref", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"},
		ResourceRID: event.ResourceRID, Channel: domain.ChannelInApp, AllFutureTypes: true,
	}}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	emitter, err := NewEmitter(store, recipientCheck(func(context.Context, domain.Owner, string) (bool, error) {
		cancel()
		return true, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, event); !errors.Is(err, context.Canceled) || len(store.events) != 0 {
		t.Fatalf("cancelled audience was recorded: events=%#v err=%v", store.events, err)
	}
}
