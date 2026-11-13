package domain

import "testing"

func alice() Owner { return Owner{Kind: PrincipalUser, ID: "alice"} }
func platform() Owner {
	return Owner{Kind: PrincipalTeam, ID: "platform"}
}

func contact(owner Owner, kind ContactKind, address string) Contact {
	return Contact{
		ID: "c-" + owner.ID + "-" + string(kind), Owner: owner,
		Kind: kind, Address: address, CreatedAt: at(),
	}
}

// The property the whole address book exists for: a delivery aimed at a person
// resolves to that person's own address rather than to whatever one endpoint
// was configured with.
func TestADeliveryResolvesToTheRecipientsOwnAddress(t *testing.T) {
	t.Parallel()
	contacts := []Contact{
		contact(alice(), ContactEmail, "alice@example.test"),
		contact(platform(), ContactEmail, "platform@example.test"),
	}
	destination, found := ResolveDestination(
		Recipient{Owner: alice(), Channel: ChannelEmail}, contacts)
	if !found {
		t.Fatal("a recipient holding an email contact resolved to nothing")
	}
	if destination.Address != "alice@example.test" || destination.Kind != ContactEmail {
		t.Fatalf("resolved to %+v, want alice's own email contact", destination)
	}
}

// The refusal that matters as much as the resolution: one owner's contacts are
// not another's, and a resolver that ignored the owner would deliver alice's
// notification to the team address that happened to sort first.
func TestAnotherOwnersContactIsNeverTheDestination(t *testing.T) {
	t.Parallel()
	contacts := []Contact{
		contact(platform(), ContactEmail, "platform@example.test"),
		contact(Owner{Kind: PrincipalUser, ID: "bob"}, ContactEmail, "bob@example.test"),
	}
	if destination, found := ResolveDestination(
		Recipient{Owner: alice(), Channel: ChannelEmail}, contacts); found {
		t.Fatalf("alice's delivery resolved to %+v, which belongs to somebody else", destination)
	}
}

// Apollo contradicts itself about PagerDuty and this is where the contradiction
// becomes behaviour: the contact exists, and it is never a destination.
func TestAPagerDutyContactIsNeverADestination(t *testing.T) {
	t.Parallel()
	only := []Contact{contact(platform(), ContactPagerDuty, "PD-SERVICE-KEY")}
	destination, found := ResolveDestination(
		Recipient{Owner: platform(), Channel: ChannelEmail}, only)
	if found {
		t.Fatalf("a PagerDuty contact was chosen as a destination: %+v", destination)
	}
	// And it does not shadow a kind that does deliver.
	both := append(only, contact(platform(), ContactEmail, "platform@example.test"))
	destination, found = ResolveDestination(
		Recipient{Owner: platform(), Channel: ChannelEmail}, both)
	if !found || destination.Kind != ContactEmail {
		t.Fatalf("resolved to %+v with a PagerDuty contact present, want the email one", destination)
	}
}

// An in-app entry is delivered by being in the inbox. Giving it an address
// would send it somewhere its owner never chose.
func TestAnInAppRecipientResolvesToNoAddress(t *testing.T) {
	t.Parallel()
	if destination, found := ResolveDestination(
		Recipient{Owner: alice(), Channel: ChannelInApp},
		[]Contact{contact(alice(), ContactEmail, "alice@example.test")},
	); found {
		t.Fatalf("an in-app entry resolved to %+v, which the inbox already delivered", destination)
	}
}

// Two deliverable contacts, one answer, and the same answer whichever order the
// store hands them over in -- two replicas resolving the same recipient at the
// same moment must not send to two different places.
func TestTheChannelsOwnKindWinsAndTheOrderDoesNot(t *testing.T) {
	t.Parallel()
	email := contact(platform(), ContactEmail, "platform@example.test")
	slack := contact(platform(), ContactSlack, "#platform-alerts")
	for name, contacts := range map[string][]Contact{
		"email first": {email, slack},
		"slack first": {slack, email},
	} {
		destination, found := ResolveDestination(
			Recipient{Owner: platform(), Channel: ChannelEmail}, contacts)
		if !found || destination.Kind != ContactEmail {
			t.Errorf("%s resolved to %+v, want the kind the channel names", name, destination)
		}
	}
	// With no email contact the Slack one is still where that team asked to be
	// reached, and telling them there beats telling nobody.
	destination, found := ResolveDestination(
		Recipient{Owner: platform(), Channel: ChannelEmail}, []Contact{slack})
	if !found || destination.Kind != ContactSlack {
		t.Fatalf("a team whose only contact is Slack resolved to %+v", destination)
	}
}

// The tie-break when no kind matches the channel at all.
//
// Unreachable with the two channels and three kinds this build knows -- EMAIL
// is both a channel and a kind, so a deliverable contact either matches or is
// the only candidate. It is reachable for a channel this build has never heard
// of, which is exactly the case the vocabulary being open makes possible, and
// the property that has to hold there is that two replicas resolving the same
// recipient at the same moment pick the same place rather than sending twice.
func TestAChannelMatchingNoKindStillResolvesTheSameWayTwice(t *testing.T) {
	t.Parallel()
	email := contact(platform(), ContactEmail, "platform@example.test")
	slack := contact(platform(), ContactSlack, "#platform-alerts")
	unknown := Recipient{Owner: platform(), Channel: Channel("SMS")}
	for name, contacts := range map[string][]Contact{
		"email first": {email, slack},
		"slack first": {slack, email},
	} {
		destination, found := ResolveDestination(unknown, contacts)
		if !found {
			t.Fatalf("%s resolved to nothing for a channel this build does not know", name)
		}
		if destination.Kind != ContactEmail {
			t.Errorf("%s resolved to %s; the order of the input decided the destination",
				name, destination.Kind)
		}
	}
}

// No contacts is not an error and not a half-built destination: it is the
// deployment that configured an endpoint and no address book, and the zero
// value is what the transport reads as "send where you were configured to".
func TestARecipientWithNoContactResolvesToNothingAtAll(t *testing.T) {
	t.Parallel()
	destination, found := ResolveDestination(
		Recipient{Owner: alice(), Channel: ChannelEmail}, nil)
	if found {
		t.Fatal("an owner holding no contact resolved to a destination")
	}
	if destination.Named() {
		t.Fatalf("an unresolved destination reported itself named: %+v", destination)
	}
	if (Destination{Kind: ContactEmail, Address: "alice@example.test"}).Named() != true {
		t.Fatal("a destination with a kind and an address did not report itself named")
	}
	// A stored contact with no address cannot become a destination either. It
	// should not exist -- Validate refuses one -- and a resolver that trusted
	// that would deliver to the empty string if it ever did.
	if _, found := ResolveDestination(
		Recipient{Owner: alice(), Channel: ChannelEmail},
		[]Contact{contact(alice(), ContactEmail, "")},
	); found {
		t.Fatal("an addressless contact resolved to a destination")
	}
}

func TestNativeChannelResolutionDoesNotFallBackAcrossContactKinds(t *testing.T) {
	contacts := []Contact{contact(alice(), ContactSlack, "#alice"), contact(platform(), ContactEmail, "team@example.test")}
	if destination, found := ResolveChannelDestination(Recipient{Owner: alice(), Channel: ChannelEmail}, contacts); found {
		t.Fatalf("email fell back to another contact: %#v", destination)
	}
	destination, found := ResolveChannelDestination(Recipient{Owner: alice(), Channel: ChannelSlack}, contacts)
	if !found || destination.Kind != ContactSlack || destination.Address != "#alice" {
		t.Fatalf("Slack destination=%#v, %v", destination, found)
	}
	if _, found := ResolveChannelDestination(Recipient{Owner: platform(), Channel: ChannelSlack}, contacts); found {
		t.Fatal("another owner's Slack contact was used")
	}
	if _, found := ResolveChannelDestination(Recipient{Owner: alice(), Channel: ChannelInApp}, contacts); found {
		t.Fatal("in-app notification acquired an external destination")
	}
}
