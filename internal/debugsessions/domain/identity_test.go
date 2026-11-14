package domain

import (
	"strings"
	"testing"
)

func TestOpaqueSubjectsArePreservedWithoutResourceNormalization(t *testing.T) {
	for _, subject := range []string{"alice+ops@example.org", "auth0|subject-id", "Subject/With:Case", "ümlaut-subject", strings.Repeat("a", 256)} {
		identity := Identity{Actor: subject, Owner: subject}
		if err := identity.Validate(); err != nil {
			t.Fatal(subject, err)
		}
		e := request(t)
		e.Actor = subject
		e.Owner = subject
		b, err := EncodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeEvent(e.SessionID, e.Revision, b)
		if err != nil || got.Actor != subject || got.Owner != subject {
			t.Fatal(got, err)
		}
	}
	for _, subject := range []string{"", " alice", "alice ", "ali\nce", "ali\x7fce", string([]byte{0xff}), strings.Repeat("a", 257)} {
		if (Identity{Actor: subject, Owner: subject}).Validate() == nil {
			t.Fatalf("accepted invalid subject %q", subject)
		}
	}
}
