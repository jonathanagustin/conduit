package domain

import "testing"

func TestCapacityThresholdsAndUnknownMeasurements(t *testing.T) {
	for _, test := range []struct {
		used, total int64
		status      string
	}{{79, 100, "OK"}, {80, 100, "WARNING"}, {94, 100, "WARNING"}, {95, 100, "CRITICAL"}, {100, 100, "CRITICAL"}, {0, 100, "OK"}, {0, 0, Unavailable}, {-1, 100, Unavailable}, {101, 100, Unavailable}} {
		got := Capacity("data", "Data filesystem", test.used, test.total)
		if got.Status != test.status {
			t.Fatalf("%d/%d: %+v", test.used, test.total, got)
		}
		if test.status == Unavailable && (got.UsedBytes != nil || got.CapacityBytes != nil || got.UsedPercent != nil) {
			t.Fatalf("unknown measurement invented values: %+v", got)
		}
	}
	missing := MissingCount("runs", "Runs", "Reader unavailable", "packaging")
	zero := Count("runs", "Runs", 0, "No active runs", "packaging")
	if missing.Value != nil || missing.Status != Unavailable || zero.Value == nil || *zero.Value != 0 || zero.Status != Available {
		t.Fatal("zero was confused with unavailable")
	}
}
