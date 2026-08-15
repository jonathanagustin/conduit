package domain

import "fmt"

// RefuseUnserved is what the resolver says when a client selects a coordinate
// the interface declares and Conduit cannot answer.
//
// It is a refusal and not a null on purpose. A GraphQL null is a value: it says
// the field was resolved and had nothing in it. Conduit resolving
// ApolloBundle.rid to null would state that this bundle has no resource
// identifier, which Conduit has never observed and has no way to know. Refusing
// says the true thing instead -- that nothing here answers the question -- and
// leaves the client to ask an Apollo Hub, which does.
func RefuseUnserved(coordinate Coordinate, tier Tier) string {
	if tier == ConduitNative {
		// Reconcile makes this a build failure, so reaching it at request time
		// means the resolver shipped a declaration nothing backs. Say so plainly
		// rather than blaming the client's query.
		return fmt.Sprintf(
			"%s is declared by Conduit and backed by nothing. This is a defect in Conduit, not in the query.",
			coordinate,
		)
	}
	return fmt.Sprintf(
		"%s was recovered from an Apollo Hub and is declared here so this interface keeps Apollo's shape, "+
			"but Conduit holds no data behind it. Conduit refuses rather than answering null, because null "+
			"would assert an absence Conduit never observed. Ask an Apollo Hub for this field.",
		coordinate,
	)
}
