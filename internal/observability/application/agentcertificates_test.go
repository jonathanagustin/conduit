package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/application"
	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

var certificateInstant = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func withCertificates(read application.ReadAgentCertificates) func(context.Context) (*domain.Registry, error) {
	return application.WithAgentCertificates(
		application.Gather("1.4.2", func(context.Context) (application.ReconciliationStats, error) { return stats(), nil }),
		read, func() time.Time { return certificateInstant },
	)
}

func TestTheSoonestAgentCertificateExpiryIsExportedAsOneUnlabelledGauge(t *testing.T) {
	t.Parallel()
	var asked time.Time
	registry, err := withCertificates(func(_ context.Context, seenSince time.Time) (application.AgentCertificates, error) {
		asked = seenSince
		return application.AgentCertificates{EarliestNotAfter: certificateInstant.Add(36 * time.Hour), Agents: 4}, nil
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the certificate metrics would be refused at publish time: %v", err)
	}
	if !asked.Equal(certificateInstant.Add(-application.AgentCertificateWindow)) {
		t.Fatalf("asked for Agents seen since %s, want the 24 hour window", asked)
	}
	text := registry.Render()
	for _, required := range []string{
		"conduit_agent_certificate_min_seconds_until_expiry 129600\n",
		"conduit_agent_certificates_tracked 4\n",
		"# TYPE conduit_agent_certificate_min_seconds_until_expiry gauge",
		`conduit_reconciliation_pending{queue="reconciliation"} 3`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %q in:\n%s", required, text)
		}
	}
	// No label at all on either series: nothing here can name a tenant.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "conduit_agent_certificate") && strings.Contains(line, "{") {
			t.Errorf("an Agent certificate series carries a label: %s", line)
		}
	}
}

// A lapsed certificate is a negative number, not a clamp to zero: how long ago
// it lapsed is the first thing somebody asks.
func TestALapsedCertificateIsReportedAsNegativeSeconds(t *testing.T) {
	t.Parallel()
	registry, err := withCertificates(func(context.Context, time.Time) (application.AgentCertificates, error) {
		return application.AgentCertificates{EarliestNotAfter: certificateInstant.Add(-time.Minute), Agents: 1}, nil
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if text := registry.Render(); !strings.Contains(text, "conduit_agent_certificate_min_seconds_until_expiry -60\n") {
		t.Fatalf("lapsed certificate rendered as:\n%s", text)
	}
}

// With no Agent seen there is no expiry to report. Zero would say "a
// certificate expires now" and page somebody on a Hub with no Agents.
func TestWithNoAgentsTheExpiryGaugeIsAbsentRatherThanZero(t *testing.T) {
	t.Parallel()
	registry, err := withCertificates(func(context.Context, time.Time) (application.AgentCertificates, error) {
		return application.AgentCertificates{}, nil
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	text := registry.Render()
	if strings.Contains(text, "conduit_agent_certificate_min_seconds_until_expiry") {
		t.Fatalf("an expiry was exported with no Agents:\n%s", text)
	}
	if !strings.Contains(text, "conduit_agent_certificates_tracked 0\n") {
		t.Fatalf("the tracked count is missing:\n%s", text)
	}
}

func TestAgentCertificateMetricsAreOptionalAndDoNotMaskAFailure(t *testing.T) {
	t.Parallel()
	registry, err := withCertificates(nil)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(registry.Render(), "conduit_agent_certificate") {
		t.Fatal("certificate metrics were exported with no reader")
	}
	if _, err := withCertificates(func(context.Context, time.Time) (application.AgentCertificates, error) {
		return application.AgentCertificates{}, errors.New("database is down")
	})(t.Context()); err == nil {
		t.Fatal("a failed read was published as a registry")
	}
}
