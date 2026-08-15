// Package schema carries the Apollo-shaped GraphQL interface Conduit serves,
// as bytes, so the server and the provenance gate cannot describe different
// interfaces.
//
// The canonical artifact is api/graphql/apollo.interface.graphql: it is what
// scripts/apollo-graphql/build-interface-schema.mjs derives from the recovery
// record plus the Conduit-native declarations, and what
// scripts/apollo-graphql/verify-apollo-interface.mjs governs. Go's embed cannot
// reach outside its own package directory, so the file is copied here and
// schema_test.go byte-compares the copy against the canonical one. A drifted
// copy fails `go test`, which is the only reason a second copy is tolerable.
//
// Nothing in this package interprets the SDL. Reading provenance out of it is
// the domain's job, and answering a query against it is the application's.
package schema

import _ "embed"

//go:embed apollo.interface.graphql
var sdl string

// Name is the path a reader should open to see this interface in the
// repository, rather than the copy this package embeds.
const Name = "api/graphql/apollo.interface.graphql"

// SDL is the served interface, provenance annotations included. The annotations
// are part of the served schema on purpose: a client introspecting Conduit can
// see which coordinates were recovered from an Apollo Hub and which Conduit
// invented, without consulting the repository.
func SDL() string { return sdl }
