package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/checkin/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/delivery/checkin/ports"
)

func TestDisconnectedSiteQueuesUntilTheNextCheckIn(t *testing.T) {
	at := time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC)
	adapter := recorded.Adapter{Rows: map[string]ports.CheckIn{
		"north": {Site: "north", Environment: "north", State: ports.Disconnected, At: at},
	}}
	service := &Service{Checker: adapter, pending: map[string][]string{}}
	status, err := service.Observe(context.Background(), "north", "north", false)
	if err != nil || status.State != ports.Disconnected || status.LastCheckIn != at {
		t.Fatalf("observe = %#v %v", status, err)
	}
	queued, err := service.Submit("north", "install ledger 1.2.0")
	if err != nil || len(queued.Pending) != 1 {
		t.Fatalf("queue = %#v %v", queued, err)
	}
	adapter.Rows["north"] = ports.CheckIn{Site: "north", Environment: "north", State: ports.Online, At: at.Add(time.Hour)}
	synced, err := service.Observe(context.Background(), "north", "north", false)
	if err != nil || synced.State != ports.Online || len(synced.Pending) != 0 {
		t.Fatalf("sync = %#v %v", synced, err)
	}
}

func TestExpectedSilenceIsNotDisconnected(t *testing.T) {
	at := time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC)
	service := &Service{
		Checker: recorded.Adapter{Rows: map[string]ports.CheckIn{
			"ridge": {Site: "ridge", Environment: "ridge", State: ports.Disconnected, At: at},
		}},
		SilenceExpected: map[string]bool{"ridge": true},
		pending:         map[string][]string{},
	}
	status, err := service.Observe(context.Background(), "ridge", "ridge", true)
	if err != nil || status.State != ports.Degraded {
		t.Fatalf("silence = %#v %v", status, err)
	}
}
