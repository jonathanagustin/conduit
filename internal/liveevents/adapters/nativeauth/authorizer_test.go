package nativeauth

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

func nativeState() State {
	return State{Allowed: true, Principal: governance.Principal{Username: "carrier", ServiceAccount: true, Teams: []string{"delegators", "operators"}, Groups: []string{"b", "a"}, OnBehalfOf: &governance.Principal{Username: "alice", Teams: []string{"readers"}}}, Grants: []governance.ResourceGrant{
		{ID: "grant-b", ResourceRID: "environment/dev", Team: "readers", Roles: []string{"environment:viewer", "entity:viewer"}, Operations: []string{"resource:read", "resource:operate"}, Revision: 3},
		{ID: "grant-a", ResourceRID: "platform/root", Team: "delegators", Revision: 1},
	}}
}

func TestNativeAuthorizationStampUsesCurrentOwnerDelegateAndGrants(t *testing.T) {
	for _, tc := range []struct {
		name               string
		mutate             func(*State)
		owner, represented bool
	}{
		{"carrier", func(s *State) { s.Principal.Username = "other-carrier" }, true, false},
		{"delegate", func(s *State) { s.Principal.OnBehalfOf.Username = "bob" }, false, true},
		{"membership", func(s *State) { s.Principal.OnBehalfOf.Teams = nil }, false, false},
		{"grant-revision", func(s *State) { s.Grants[0].Revision++ }, false, false},
		{"grant-scope", func(s *State) { s.Grants[0].ResourceRID = "environment/prod" }, false, false},
		{"grant-permission", func(s *State) { s.Grants[0].Roles = nil }, false, false},
		{"delegation-grant", func(s *State) { s.Grants = s.Grants[:1] }, false, false},
		{"administrator", func(s *State) { s.Administrator = true }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := nativeState()
			visibleCalls := 0
			auth, _ := New(Config{Resolve: func(context.Context) (State, error) { return state, nil }, Visible: func(_ context.Context, p governance.Principal, _ live.Subject) (bool, error) {
				visibleCalls++
				if p.Username != "carrier" || p.OnBehalfOf.Username != "alice" {
					t.Fatal("lost native carrier delegation")
				}
				return true, nil
			}})
			before, err := auth.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if visible, err := auth.Visible(context.Background(), before, live.Subject{Type: "plan", ID: "plan"}); err != nil || !visible {
				t.Fatalf("visible=%v err=%v", visible, err)
			}
			tc.mutate(&state)
			after, err := auth.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if (before.Owner != after.Owner) != tc.owner || (before.Represented != after.Represented) != tc.represented || before.Authorization == after.Authorization {
				t.Fatal("authorization change not bound to expected identity components")
			}
			if visible, err := auth.Visible(context.Background(), before, live.Subject{}); visible || !errors.Is(err, live.ErrReset) || visibleCalls != 1 {
				t.Fatalf("stale identity reached visibility policy: %v %v calls=%d", visible, err, visibleCalls)
			}
		})
	}
}

func TestNativeStampIgnoresStorageOrderAndTimestampsWithoutMutatingInputs(t *testing.T) {
	state := nativeState()
	original := nativeState()
	auth, _ := New(Config{Resolve: func(context.Context) (State, error) { return state, nil }, Visible: func(context.Context, governance.Principal, live.Subject) (bool, error) { return true, nil }})
	before, err := auth.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, original) {
		t.Fatal("snapshot mutated repository-owned values")
	}
	state.Principal.Groups = []string{"a", "b"}
	state.Principal.Teams = []string{"operators", "delegators"}
	state.Grants[0].Roles = []string{"entity:viewer", "environment:viewer"}
	state.Grants[0].Operations = []string{"resource:operate", "resource:read"}
	state.Grants[0].UpdatedAt = time.Date(99999, 1, 1, 0, 0, 0, 0, time.UTC)
	state.Grants[0], state.Grants[1] = state.Grants[1], state.Grants[0]
	after, err := auth.Snapshot(context.Background())
	if err != nil || before != after {
		t.Fatalf("storage-only change invented epoch: %v", err)
	}
}

func TestNativeAuthorizationFailsClosedBeforeVisibility(t *testing.T) {
	for _, tc := range []struct {
		name             string
		mutate           func(*State)
		resolveErr, want error
	}{
		{"anonymous", func(s *State) { s.Principal.Username = "" }, nil, live.ErrUnauthorized},
		{"revoked", func(s *State) { s.Allowed = false }, nil, live.ErrForbidden},
		{"unavailable", func(*State) {}, live.ErrUnavailable, live.ErrUnavailable},
		{"invalid-user", func(s *State) { s.Principal.Username = string([]byte{255}) }, nil, live.ErrInvalid},
		{"invalid-team", func(s *State) { s.Principal.Teams = []string{string([]byte{255})} }, nil, live.ErrInvalid},
		{"invalid-grant", func(s *State) { s.Grants[0].Team = string([]byte{255}) }, nil, live.ErrInvalid},
		{"cyclic-delegation", func(s *State) { s.Principal.OnBehalfOf = &s.Principal }, nil, live.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := nativeState()
			tc.mutate(&state)
			auth, _ := New(Config{Resolve: func(context.Context) (State, error) { return state, tc.resolveErr }, Visible: func(context.Context, governance.Principal, live.Subject) (bool, error) {
				t.Fatal("invalid state reached resource policy")
				return true, nil
			}})
			if _, err := auth.Snapshot(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("snapshot err=%v want %v", err, tc.want)
			}
			if visible, err := auth.Visible(context.Background(), live.Identity{}, live.Subject{}); visible || !errors.Is(err, tc.want) {
				t.Fatalf("visible=%v err=%v", visible, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auth, _ := New(Config{Resolve: func(context.Context) (State, error) { t.Fatal("resolved cancelled request"); return State{}, nil }, Visible: func(context.Context, governance.Principal, live.Subject) (bool, error) { return false, nil }})
	if _, err := auth.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := New(Config{}); !errors.Is(err, live.ErrInvalid) {
		t.Fatal("missing native callbacks accepted")
	}
}
