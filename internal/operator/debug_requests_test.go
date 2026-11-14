package operator

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	requests "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
)

func debugRecord(t *testing.T) requests.Event {
	t.Helper()
	record, err := requests.New("dsr_abcdefghijklmnopqrstuvwxyz", requests.Target{Environment: "env", Namespace: "app", Profile: "requested"}, requests.Identity{Actor: "actor", Owner: "owner"}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
func TestDebugClientRejectsChangedIdentityTargetAndLifetimeReceipts(t *testing.T) {
	for _, kind := range []string{"id", "target", "lifetime"} {
		t.Run(kind, func(t *testing.T) {
			expected := debugRecord(t)
			response := expected
			switch kind {
			case "id":
				response.SessionID = "dsr_bcdefghijklmnopqrstuvwxyz2"
			case "target":
				response.Target.Namespace = "other"
			case "lifetime":
				response.ExpiresAt = response.ExpiresAt.Add(time.Hour)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(response) }))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.CreateDebugRequest(t.Context(), DebugRequestDraft{SessionID: expected.SessionID, Target: expected.Target, LifetimeSeconds: 3600})
			if !errors.Is(err, requests.ErrCorrupt) {
				t.Fatal(err)
			}
		})
	}
}
func TestDebugClientRejectsForeignPagesAndInvisibleCursors(t *testing.T) {
	for _, kind := range []string{"scope", "cursor", "tier"} {
		t.Run(kind, func(t *testing.T) {
			record := debugRecord(t)
			page := DebugRequestPage{Requests: []requests.Event{record}, ValueTier: requests.ValueTier}
			switch kind {
			case "scope":
				page.Requests[0].Target.Environment = "other"
			case "cursor":
				page.NextCursor = "dsr_bcdefghijklmnopqrstuvwxyz2"
			case "tier":
				page.ValueTier = "OBSERVED"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(page) }))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.ListDebugRequests(t.Context(), record.Target.Scope(), "", 10); !errors.Is(err, requests.ErrCorrupt) {
				t.Fatal(err)
			}
		})
	}
}
func TestDebugClientDoesNotSilentlyDropSpaceSelection(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	adapter.spaceID = "spc_selected"
	if _, err := adapter.GetDebugRequest(t.Context(), debugRecord(t).SessionID); err == nil || !strings.Contains(err.Error(), "Space selection") {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("unsupported scope contacted the server")
	}
}
