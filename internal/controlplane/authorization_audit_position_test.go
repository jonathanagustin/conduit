package controlplane

import (
	"errors"
	"strings"
	"testing"
)

func TestAuditLegacyPositionRequiresACompletePendingFingerprint(t *testing.T) {
	position := auditPosition{timed: true, at: auditPageEpoch, id: "event", pending: strings.Repeat("a", 64)}
	parsed, err := parseAuditPosition(position.String())
	if err != nil || parsed != position {
		t.Fatalf("bound legacy position = %#v, %v", parsed, err)
	}
	for _, raw := range []string{
		"l:", "l:short:" + auditPageEpoch.Format("2006-01-02T15:04:05Z07:00") + "|event",
		"l:" + strings.Repeat("g", 64) + ":2020-01-01T00:00:00Z|event",
		"l:" + strings.Repeat("a", 64) + ":not-a-time|event",
		"l:" + strings.Repeat("a", 64) + ":2020-01-01T00:00:00Z",
	} {
		if _, err := parseAuditPosition(raw); !errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
			t.Fatalf("invalid legacy position %q accepted: %v", raw, err)
		}
	}
}

func TestUnchainedAuditStoresRefusePostgresLegacyPositions(t *testing.T) {
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			position := auditPosition{timed: true, at: auditPageEpoch, pending: strings.Repeat("a", 64)}
			if _, err := repository.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: 1, After: position.String()}); !errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
				t.Fatalf("PostgreSQL compatibility position accepted: %v", err)
			}
		})
	}
}
