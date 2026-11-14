package application

import "testing"

func TestBoardShowsTheVersionOnEachSubscriptionChannel(t *testing.T) {
	view := boardFrom([]environmentRef{{
		id: "east", defaultChannel: "RELEASE",
		releases: []releaseRef{
			{product: "ledger", version: "1.0.0", channels: []string{"DEV"}},
			{product: "ledger", version: "1.1.0", channels: []string{"RELEASE_CANDIDATE"}},
			{product: "ledger", version: "1.2.0", channels: []string{"RELEASE"}},
		},
		entities: []entityRef{{product: "ledger", version: "1.2.0", channel: "RELEASE"}},
	}}, map[string]string{"east\x00ledger": "1.1.0"})
	if len(view.Rows) != 1 {
		t.Fatalf("rows = %#v", view.Rows)
	}
	row := view.Rows[0]
	if row.Subscribed != "stable" || row.Running != "1.1.0" {
		t.Fatalf("subscription = %#v", row)
	}
	if row.Versions["dev"] != "1.0.0" || row.Versions["candidate"] != "1.1.0" || row.Versions["stable"] != "1.2.0" {
		t.Fatalf("versions = %#v", row.Versions)
	}
}

func TestBoardFollowsAPassedPromotionAndIgnoresAnUnsubscribedName(t *testing.T) {
	view := boardFrom([]environmentRef{{
		id: "west", defaultChannel: "STAGING",
		releases: []releaseRef{{product: "ledger", version: "1.0.0", channels: []string{"DEV"}}},
		promotions: []promotionRef{{
			product: "ledger", version: "1.0.0", from: "DEV", to: "RELEASE_CANDIDATE", passed: true,
		}},
	}}, nil)
	row := view.Rows[0]
	if row.Subscribed != "" || row.Versions["candidate"] != "1.0.0" || row.Versions["stable"] != "" || row.Running != "" {
		t.Fatalf("row = %#v", row)
	}
}
