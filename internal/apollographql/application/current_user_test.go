package application

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

func TestCurrentUserSnapshotIsRequestBoundAndAbsenceIsAnError(t *testing.T) {
	resolver, _, _ := newFixture(t)
	query := Request{Query: `query GetCurrentUser { me { id fullName } }`}
	for _, id := range []string{"first-owner", "second-owner"} {
		result := resolver.Resolver.Execute(t.Context(), ports.Sources{CurrentUser: &ports.CurrentUser{ID: id}}, query)
		if len(result.Errors) > 0 {
			t.Fatal(result.Errors)
		}
		me := result.Data["me"].(map[string]any)
		if me["id"] != id || me["fullName"] != nil {
			t.Fatalf("wrong snapshot: %+v", me)
		}
	}
	missing := resolver.Resolver.Execute(t.Context(), ports.Sources{}, query)
	if len(missing.Errors) != 1 || !strings.Contains(missing.Errors[0].Message, "no source") {
		t.Fatalf("unconfigured identity invented absence: %+v", missing)
	}
}
