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
//
// What refusal is no longer is the default. ADR 0007 supersedes ADR 0005: a
// coordinate with no ground truth behind it is now served from a value Conduit
// invented, disclosed on the wire as valueTier: CONDUIT_NATIVE and entered in
// docs/evidence/conduit-inventions.json. The reasoning above survives intact and
// is exactly why disclosure was the fix rather than nulls -- what changed is that
// the interface gained a way to say "this value is ours", so refusing for want of
// a way to say it stopped being the only honest option.
//
// Two callers remain. The defect case below, which Reconcile fails the build on.
// And ADR 0007 group 2 -- the five spaceId coordinates -- which stay refused for
// a reason no mechanism fixes: Palantir's own documentation puts the Space id's
// origin outside the Hub, arriving by agent report, so there is no Conduit value
// to disclose and a constant would be a synthesized value wearing a tenancy
// field.
func RefuseUnserved(coordinate Coordinate, tier Tier) string {
	if !tier.Established() {
		// Reconcile makes this a build failure, so reaching it at request time
		// means the resolver shipped a declaration nothing backs. Say so plainly
		// rather than blaming the client's query.
		//
		// A DOCUMENTED coordinate reads the same as a CONDUIT_NATIVE one here.
		// Apollo publishing the name does not put data behind it; Conduit added
		// the field, so the missing source is Conduit's defect either way.
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
