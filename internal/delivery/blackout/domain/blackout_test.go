package domain

import (
	"testing"
	"time"
)

func TestReasonIsTheOperatorsTextInsideTheInterval(t *testing.T) {
	start := time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	blackout, err := New("east", "ada", "change freeze for the rehearsal", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if Reason([]Blackout{blackout}, "east", start) != "change freeze for the rehearsal" {
		t.Fatal("start should be inside")
	}
	if Reason([]Blackout{blackout}, "east", end) != "" {
		t.Fatal("end is outside")
	}
	if Reason([]Blackout{blackout}, "west", start.Add(time.Minute)) != "" {
		t.Fatal("other environment")
	}
	if _, err := New("east", "ada", "  ", start, end); err == nil {
		t.Fatal("missing reason")
	}
}
