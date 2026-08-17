package pgmigrate_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

func list(names ...string) []pgmigrate.Migration {
	migrations := make([]pgmigrate.Migration, 0, len(names))
	for _, name := range names {
		migrations = append(migrations, pgmigrate.Migration{Name: name, Statements: "SELECT 1;"})
	}
	return migrations
}

func TestAWellFormedListIsAccepted(t *testing.T) {
	t.Parallel()
	if err := pgmigrate.Validate("reconciliation", list("0001-a", "0002-b")); err != nil {
		t.Fatal(err)
	}
}

// The one that matters. Two migrations with the same name mean the ledger
// records the first and skips the second forever -- the DDL simply never runs,
// on every database, silently. Nothing downstream reports it, because from the
// runner's point of view the work is done.
func TestADuplicateNameIsRefused(t *testing.T) {
	t.Parallel()
	err := pgmigrate.Validate("reconciliation", list("0001-a", "0002-b", "0001-a"))
	if err == nil {
		t.Fatal("a duplicated migration name was accepted; its second occurrence would never run")
	}
	// The message has to name both positions, because "duplicate name" sends a
	// reader looking through a list where every entry looks plausible.
	for _, expected := range []string{"0", "2", "0001-a"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the error does not locate the duplicate (%q): %v", expected, err)
		}
	}
}

// An empty list is the shape a refactor produces when a build tag or a filter
// goes wrong. Applying it would leave a database with no schema and report
// success, so the Hub would come up and fail on its first query instead.
func TestAnEmptyListIsRefused(t *testing.T) {
	t.Parallel()
	if err := pgmigrate.Validate("reconciliation", nil); err == nil {
		t.Fatal("an empty migration list was accepted")
	}
}

// The component is the ledger key and the advisory-lock key. Empty means every
// caller shares one namespace and one lock, so two unrelated stores would
// serialise against each other and read each other's history.
func TestAnEmptyComponentIsRefused(t *testing.T) {
	t.Parallel()
	if err := pgmigrate.Validate("   ", list("0001-a")); err == nil {
		t.Fatal("an empty component name was accepted")
	}
}

// Recording a migration that ran no statements writes a ledger row claiming
// work that never happened -- and because the ledger is what later starts trust,
// that claim is permanent.
func TestAMigrationWithNoStatementsIsRefused(t *testing.T) {
	t.Parallel()
	err := pgmigrate.Validate("reconciliation", []pgmigrate.Migration{{Name: "0001-a", Statements: "  \n "}})
	if err == nil {
		t.Fatal("a migration with no statements was accepted")
	}
	if !strings.Contains(err.Error(), "0001-a") {
		t.Errorf("the error does not name the migration: %v", err)
	}
}

// Apply must validate before it touches anything. Passing a nil database is how
// that gets asserted without one: if validation runs first the call returns the
// validation error, and if it does not the nil dereferences. A mutation
// disabling the check inside Apply survived until this existed, because every
// other test called Validate directly and none went through Apply at all.
func TestApplyValidatesBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	err := pgmigrate.Apply(t.Context(), nil, "reconciliation", list("0001-a", "0001-a"))
	if err == nil {
		t.Fatal("Apply accepted a duplicated migration name")
	}
	if !strings.Contains(err.Error(), "0001-a") {
		t.Errorf("Apply did not report the validation failure: %v", err)
	}
}

func TestAMigrationWithNoNameIsRefused(t *testing.T) {
	t.Parallel()
	if err := pgmigrate.Validate("reconciliation", []pgmigrate.Migration{{Statements: "SELECT 1;"}}); err == nil {
		t.Fatal("a migration with no name was accepted")
	}
}
