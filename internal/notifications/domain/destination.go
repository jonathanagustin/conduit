package domain

import "sort"

// Destination is where one delivery goes: the kind of endpoint, and the address
// that endpoint is reached at.
//
// It is a pair rather than a bare string because the kind is what tells a
// transport how to read the address. "platform-alerts" is a Slack channel and
// "ops@example.test" is a mailbox, and a receiver handed the second without the
// first has to guess by looking for an at-sign.
//
// The zero value means "this delivery names no destination of its own", which
// is not an error: it is the deployment that has configured one endpoint and no
// contacts, and it is the behaviour every deployment had before contacts
// existed.
type Destination struct {
	Kind    ContactKind `json:"kind"`
	Address string      `json:"address"`
}

// Named reports whether this destination names anywhere.
func (destination Destination) Named() bool {
	return destination.Kind != "" && destination.Address != ""
}

// ResolveDestination picks the destination a recipient's own contacts give, and
// reports whether they gave one.
//
// Three rules, in this order, and each of them is a decision rather than a
// recovery.
//
// An in-app entry resolves to nothing. The inbox row is the delivery, so an
// address would send it somewhere its owner never chose -- NeedsTransport
// already says this and this agrees with it rather than restating it.
//
// A contact whose kind Apollo does not deliver to is skipped. That is the
// PagerDuty contradiction made operational: the dialog creates the contact and
// the team-notifications page says PagerDuty is not supported, so a PagerDuty
// contact is stored, is readable by its owner, and is never a destination. An
// owner holding nothing else falls back, which is the honest outcome -- silently
// posting a PagerDuty service key to a mail relay would be worse than either.
//
// When an owner holds more than one deliverable contact, the kind whose name
// matches the channel wins and the rest are ordered by kind name. Apollo
// configures a resource on EMAIL and IN_APP while a contact may be EMAIL or
// SLACK, so the two vocabularies overlap in one place and nothing published
// says what happens outside it: a team whose only contact is Slack still asked
// to be told, and telling them where they asked to be reached is better than
// telling nobody. The tie-break is by name only so that two replicas resolving
// the same recipient at the same moment agree.
//
// A pure function of its inputs, like Fan: the caller supplies the contacts it
// read, so nothing here can widen what a reader may see.
// CONDUIT-INVENTED: notifications.DestinationResolution
// Displaced-by: a capture of how Apollo chooses which of a recipient's contacts
// a notification is delivered to, and what it does when there is none
func ResolveDestination(recipient Recipient, contacts []Contact) (Destination, bool) {
	if !NeedsTransport(InboxEntry{Recipient: recipient}) {
		return Destination{}, false
	}
	candidates := make([]Contact, 0, len(contacts))
	for _, contact := range contacts {
		if contact.Owner != recipient.Owner {
			continue
		}
		if !contact.Kind.Deliverable() {
			continue
		}
		if contact.Address == "" {
			continue
		}
		candidates = append(candidates, contact)
	}
	if len(candidates) == 0 {
		return Destination{}, false
	}
	matches := func(kind ContactKind) bool { return string(kind) == string(recipient.Channel) }
	sort.SliceStable(candidates, func(left, right int) bool {
		if matches(candidates[left].Kind) != matches(candidates[right].Kind) {
			return matches(candidates[left].Kind)
		}
		return candidates[left].Kind < candidates[right].Kind
	})
	return Destination{Kind: candidates[0].Kind, Address: candidates[0].Address}, true
}

// ResolveChannelDestination resolves only the selected channel's contact. Native
// delivery uses this policy; the generic webhook keeps its legacy cross-kind
// fallback so existing relay deployments retain their configured behavior.
func ResolveChannelDestination(recipient Recipient, contacts []Contact) (Destination, bool) {
	matching := make([]Contact, 0, len(contacts))
	for _, contact := range contacts {
		if string(contact.Kind) == string(recipient.Channel) {
			matching = append(matching, contact)
		}
	}
	return ResolveDestination(recipient, matching)
}
