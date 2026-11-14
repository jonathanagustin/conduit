package domain

import "testing"

func TestSubscriptionNames(t *testing.T) {
	if SubscriptionOf("release_candidate") != "candidate" || CatalogChannel("stable") != "RELEASE" || SubscriptionOf("edge") != "" {
		t.Fatal("subscription names")
	}
	if len(Channels()) != 3 || Channels()[0] != "dev" || Channels()[2] != "stable" {
		t.Fatalf("channels = %#v", Channels())
	}
}
