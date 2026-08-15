package domain

import (
	"fmt"
	"sort"
)

// Coverage is Conduit's own answer to "can I serve this coordinate, and from
// where?", stated once so that the resolver, the tests and the operator all
// read the same list.
//
// The value is a source note like "reporting: PlanSummary.Phase". It is not
// used to resolve anything -- the resolver holds the code that does -- but it
// is what an unserved refusal and a native declaration are checked against, so
// a claim that Conduit can answer a field is a claim something has to back.
type Coverage struct {
	sources map[Coordinate]string
}

// NewCoverage takes ownership of nothing: the map is copied, so a caller cannot
// widen coverage after the resolver has been built from it.
func NewCoverage(sources map[Coordinate]string) Coverage {
	copied := make(map[Coordinate]string, len(sources))
	for coordinate, source := range sources {
		copied[coordinate] = source
	}
	return Coverage{sources: copied}
}

// SourceOf returns where Conduit gets a coordinate. The second result is false
// when Conduit holds nothing behind it, which is the case the interface refuses
// rather than nulls.
func (coverage Coverage) SourceOf(coordinate Coordinate) (string, bool) {
	source, found := coverage.sources[coordinate]
	return source, found
}

// IssueKind separates the findings that are defects from the one that is not.
type IssueKind string

const (
	// UnservedObserved is a coordinate an Apollo Hub answers for and Conduit
	// does not hold. It is expected, not a defect: Conduit has no bundles and
	// no space identifiers, and declaring them is how the interface stays
	// Apollo-shaped. The resolver refuses these at request time.
	UnservedObserved IssueKind = "unserved_observed"

	// UnservedNative is a defect. conduit-native.json exists to add coordinates
	// Conduit's own domains answer; an invented field nothing can serve is
	// worse than an absent one, because it advertises a capability that does
	// not exist and cannot be displaced by any capture.
	UnservedNative IssueKind = "unserved_native"

	// Undeclared is a defect. Conduit claims a source for a coordinate the
	// served interface does not declare, so either the schema was rebuilt
	// without the resolver or the resolver would answer outside its contract.
	Undeclared IssueKind = "undeclared"
)

// Issue is a finding about the interface, not a verdict on it. Whether an
// UnservedObserved is acceptable is the caller's judgement; whether it exists
// is this package's.
type Issue struct {
	Kind       IssueKind
	Coordinate Coordinate
	Detail     string
}

func (issue Issue) String() string {
	return fmt.Sprintf("%s: %s -- %s", issue.Kind, issue.Coordinate, issue.Detail)
}

// Reconcile compares what the interface declares against what Conduit can
// serve, and returns every difference in a stable order.
//
// It reports all three kinds rather than stopping at the first, because the
// caller decides which are tolerable: a test fails on the two defect kinds, and
// the operator-facing report counts the unserved observed surface so the gap
// between "Apollo-shaped" and "Conduit-answerable" is a published number rather
// than a discovery made one query at a time.
func Reconcile(declared Interface, coverage Coverage) []Issue {
	var issues []Issue
	for _, coordinate := range declared.Coordinates() {
		if _, served := coverage.SourceOf(coordinate); served {
			continue
		}
		tier, _ := declared.TierOf(coordinate)
		if tier == ConduitNative {
			issues = append(issues, Issue{
				Kind:       UnservedNative,
				Coordinate: coordinate,
				Detail: "invented by Conduit but backed by no source; remove it from " +
					"scripts/apollo-graphql/conduit-native.json or give the resolver a source for it",
			})
			continue
		}
		issues = append(issues, Issue{
			Kind:       UnservedObserved,
			Coordinate: coordinate,
			Detail:     "recovered from an Apollo Hub; Conduit holds no data behind it and refuses it at request time",
		})
	}

	claimed := make([]Coordinate, 0, len(coverage.sources))
	for coordinate := range coverage.sources {
		if _, found := declared.TierOf(coordinate); !found {
			claimed = append(claimed, coordinate)
		}
	}
	sort.Slice(claimed, func(left, right int) bool { return claimed[left].String() < claimed[right].String() })
	for _, coordinate := range claimed {
		issues = append(issues, Issue{
			Kind:       Undeclared,
			Coordinate: coordinate,
			Detail:     "the resolver claims a source for a coordinate the served interface does not declare",
		})
	}
	return issues
}

// Defects are the findings that must not survive a build. Separating them here
// rather than at each call site keeps one answer to "which of these is a bug?".
func Defects(issues []Issue) []Issue {
	var defects []Issue
	for _, issue := range issues {
		if issue.Kind == UnservedObserved {
			continue
		}
		defects = append(defects, issue)
	}
	return defects
}
