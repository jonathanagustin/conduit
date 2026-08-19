package multipassoffline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

func TestSeededDirectoryCarriesTheRecoveredAttributeShape(t *testing.T) {
	t.Parallel()
	directory := Seeded()
	user, err := directory.User(context.Background(), "offline-user-operator")
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "operator" {
		t.Fatalf("username = %q", user.Username)
	}
	// The point of the fixture is that the derived values come out of the
	// attribute map, exactly as they do from a real response. A fixture that
	// carried a GivenName field would let a test pass that a real decode fails.
	if user.FullName() != "Ops Operator" || user.EmailPrimary() != "operator@example.test" {
		t.Fatalf("derived values wrong: %q %q", user.FullName(), user.EmailPrimary())
	}
	if !user.Active() {
		t.Error("the seeded operator is not active")
	}
	if user.Attributes[domain.MultipassAttributeGivenName][0] != "Ops" {
		t.Errorf("attributes = %#v", user.Attributes)
	}
}

func TestAbsenceIsTheSentinelAndNotAnEmptyValue(t *testing.T) {
	t.Parallel()
	directory := Seeded()
	if _, err := directory.User(context.Background(), "nobody"); !errors.Is(err, domain.ErrMultipassPrincipalNotFound) {
		t.Fatalf("err = %v", err)
	}
	_, err := directory.GroupsForUser(context.Background(), "nobody", domain.MultipassGroupPage{})
	if !errors.Is(err, domain.ErrMultipassPrincipalNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// The batch read answers with what it found and says nothing about what it did
// not. An adapter that failed on a missing id would pass here and fail against
// a real service, which is the divergence a paired adapter exists to prevent.
func TestUsersByIDsReportsPartialResultsWithoutFailing(t *testing.T) {
	t.Parallel()
	users, err := Seeded().UsersByIDs(context.Background(),
		[]string{"offline-user-operator", "nobody", "offline-user-admin"})
	if err != nil {
		t.Fatalf("a missing id failed the batch: %v", err)
	}
	names := make([]string, 0, len(users))
	for _, user := range users {
		names = append(names, user.Username)
	}
	if strings.Join(names, ",") != "operator,admin" {
		t.Fatalf("users = %v, want the two that exist in the order asked for", names)
	}
}

// A user that exists and belongs to nothing is an empty page, not an absence.
func TestAUserWithNoGroupsIsAnEmptyPage(t *testing.T) {
	t.Parallel()
	directory := New([]domain.MultipassUser{{ID: "u", Username: "loner"}}, nil)
	page, err := directory.GroupsForUser(context.Background(), "u", domain.MultipassGroupPage{})
	if err != nil {
		t.Fatalf("a groupless user was reported absent: %v", err)
	}
	if len(page.Groups) != 0 || page.Next != "" {
		t.Fatalf("page = %#v", page)
	}
}

func TestGroupsForUserPagesByOffset(t *testing.T) {
	t.Parallel()
	groups := []domain.MultipassGroup{{ID: "g1", Name: "Alpha"}, {ID: "g2", Name: "Beta"}, {ID: "g3", Name: "Gamma"}}
	directory := New(
		[]domain.MultipassUser{{ID: "u", Username: "member"}},
		map[string][]domain.MultipassGroup{"u": groups},
	)
	for _, testCase := range []struct {
		name     string
		page     domain.MultipassGroupPage
		wantName []string
		wantNext string
	}{
		{name: "no size is the whole set", wantName: []string{"Alpha", "Beta", "Gamma"}},
		{name: "first page", page: domain.MultipassGroupPage{Size: 2}, wantName: []string{"Alpha", "Beta"}, wantNext: "2"},
		{name: "continuation", page: domain.MultipassGroupPage{From: 2, Size: 2}, wantName: []string{"Gamma"}},
		{name: "past the end is empty", page: domain.MultipassGroupPage{From: 9}},
		{
			name: "a negative offset reads from the start rather than panicking",
			page: domain.MultipassGroupPage{From: -1, Size: 1}, wantName: []string{"Alpha"}, wantNext: "1",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			page, err := directory.GroupsForUser(context.Background(), "u", testCase.page)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(page.Groups))
			for _, group := range page.Groups {
				names = append(names, group.Name)
			}
			if strings.Join(names, ",") != strings.Join(testCase.wantName, ",") {
				t.Fatalf("groups = %v, want %v", names, testCase.wantName)
			}
			if page.Next != testCase.wantNext {
				t.Fatalf("next = %q, want %q", page.Next, testCase.wantNext)
			}
			if page.OrderedBy != "NAME" {
				t.Fatalf("orderedBy = %q; the fixture sorts by name and must say so", page.OrderedBy)
			}
		})
	}
}

// The fixture is shared by every caller of a Directory, so a returned principal
// must not be a window onto it.
func TestReturnedPrincipalsDoNotAliasTheFixture(t *testing.T) {
	t.Parallel()
	directory := Seeded()
	first, err := directory.User(context.Background(), "offline-user-operator")
	if err != nil {
		t.Fatal(err)
	}
	first.Attributes[domain.MultipassAttributeGivenName][0] = "Tampered"
	first.Attributes["multipass:injected"] = []string{"x"}
	second, err := directory.User(context.Background(), "offline-user-operator")
	if err != nil {
		t.Fatal(err)
	}
	if second.GivenName() != "Ops" {
		t.Fatalf("the fixture's attribute values were written through: %q", second.GivenName())
	}
	if _, injected := second.Attributes["multipass:injected"]; injected {
		t.Fatal("the fixture's attribute map was written through")
	}
}

// The same applies to what was handed to New: a caller that keeps its slice and
// mutates it later must not be editing a live directory.
func TestConstructionCopiesItsInputs(t *testing.T) {
	t.Parallel()
	attributes := domain.MultipassAttributes{domain.MultipassAttributeGivenName: {"Ops"}}
	users := []domain.MultipassUser{{ID: "u", Username: "operator", Attributes: attributes}}
	directory := New(users, nil)
	attributes[domain.MultipassAttributeGivenName][0] = "Tampered"
	users[0].Username = "tampered"
	user, err := directory.User(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "operator" || user.GivenName() != "Ops" {
		t.Fatalf("user = %#v", user)
	}
}
