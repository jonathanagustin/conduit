// Package domain is the Knative interface's model of what Conduit can honestly
// present as a Knative resource, and from where.
//
// It is the sibling of internal/apollographql/domain, and it answers the same
// question that package does -- "whether, and on whose authority" -- but the
// authority is different in kind. The Apollo interface tiers a field by who
// established it (a Hub capture versus Apollo's published docs), because Apollo
// was recovered rather than specified. Knative is the opposite: its API is a
// public, versioned specification, so nothing here is uncertain about Knative.
// What is uncertain is whether CONDUIT holds data behind a given Knative field.
//
// So a Knative coordinate is either MAPPED -- Conduit has a real source for it,
// named here -- or REFUSED -- Knative's spec declares the field but Conduit has
// nothing that honestly fills it. The distinction is the whole point, and it is
// the same discipline the Apollo interface enforces: presenting a field Conduit
// cannot back would assert a correspondence that does not exist. Knative's
// Route.spec.traffic[].percent is the standing example -- Conduit promotes a
// Release through a channel all-or-nothing per Environment and has no
// percentage HTTP traffic split, so serving a percent would invent one.
package domain

import (
	"fmt"
	"sort"
)

// Coordinate names one field of one Knative kind, e.g. {Kind: "Revision",
// Field: "spec.containers[].image"}. Kinds are the serving.knative.dev/v1 core
// resources; fields are dotted paths into them.
type Coordinate struct {
	Kind, Field string
}

func (coordinate Coordinate) String() string {
	return coordinate.Kind + "." + coordinate.Field
}

// Mapping is the single statement of which Knative coordinates Conduit fills and
// from where. A coordinate present here is MAPPED and the value is a short
// citation of the Conduit source (the domain field or projection it reads).
// A coordinate absent here that a served kind declares is REFUSED at projection
// time -- the default on purpose, so forgetting to map a field makes it refuse
// rather than silently emit a zero value that reads as fact.
type Mapping struct {
	sources map[Coordinate]string
}

// NewMapping builds the table from a coordinate-to-source map.
func NewMapping(sources map[Coordinate]string) Mapping {
	copied := make(map[Coordinate]string, len(sources))
	for coordinate, source := range sources {
		copied[coordinate] = source
	}
	return Mapping{sources: copied}
}

// SourceOf returns the Conduit source for a coordinate and whether it is mapped.
func (mapping Mapping) SourceOf(coordinate Coordinate) (string, bool) {
	source, mapped := mapping.sources[coordinate]
	return source, mapped
}

// Coordinates returns every mapped coordinate, sorted, so a report or a test
// reads the same order every time.
func (mapping Mapping) Coordinates() []Coordinate {
	coordinates := make([]Coordinate, 0, len(mapping.sources))
	for coordinate := range mapping.sources {
		coordinates = append(coordinates, coordinate)
	}
	sort.Slice(coordinates, func(left, right int) bool {
		return coordinates[left].String() < coordinates[right].String()
	})
	return coordinates
}

// Refuse is what the projection says when a client reads a coordinate Knative
// declares and Conduit cannot fill. It is a refusal and not a zero value on
// purpose: a Knative Revision serving spec.containerConcurrency: 0 would state
// that this Revision permits unlimited concurrency, which Conduit has never
// observed and has no way to know. Refusing states the true thing -- that
// nothing here answers the question -- rather than dressing an absence as a
// setting.
func Refuse(coordinate Coordinate) string {
	return fmt.Sprintf(
		"%s is a Knative field Conduit does not back. Conduit projects Knative's shape over its own "+
			"delivery data, and holds no value for this field; emitting a zero value would assert a "+
			"setting Conduit never established.",
		coordinate,
	)
}
