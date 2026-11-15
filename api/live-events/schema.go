// Package liveevents owns the versioned general-event schema and resume fixtures.
// It does not mount a stream or establish a producer's authority.
package liveevents

import _ "embed"

//go:embed event.schema.json
var eventSchema []byte

// EventSchema returns a private copy of the committed transport contract.
func EventSchema() []byte { return append([]byte(nil), eventSchema...) }
