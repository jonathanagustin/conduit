package application

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// AgentCertificateWindow is how recently an Agent must have been seen for its
// certificate to count. An Agent whose certificate lapsed can no longer
// authenticate, so it stops being seen, and a day later it stops holding the
// minimum at a negative number forever. A decommissioned Agent ages out the
// same way. The alert this gauge exists for fires long before either.
const AgentCertificateWindow = 24 * time.Hour

// AgentCertificates is the soonest client-certificate expiry among Agents
// seen since the window opened, and how many such Agents there are.
type AgentCertificates struct {
	EarliestNotAfter time.Time
	Agents           int
}

// ReadAgentCertificates is the Agent credential store, narrowed to the one
// aggregate metrics may publish.
type ReadAgentCertificates func(ctx context.Context, seenSince time.Time) (AgentCertificates, error)

// WithAgentCertificates adds Agent certificate expiry to a gathered registry.
//
// One number for the whole fleet, and deliberately so. The obvious design --
// a gauge per Agent -- is the one domain.Registry refuses: a label naming an
// Environment or an Agent publishes the tenant's topology to anyone who can
// scrape. The minimum is what an alert needs ("something expires within a
// week"); which Agent it is belongs behind authorization, and
// GET /v1/environments/{id}/agent-credentials answers it there.
//
// With no Agent seen in the window the expiry gauge is absent rather than
// zero. Zero would read as "a certificate expires now" on a Hub that has no
// Agents, and page somebody for it.
func WithAgentCertificates(
	gather func(context.Context) (*domain.Registry, error), read ReadAgentCertificates, now func() time.Time,
) func(context.Context) (*domain.Registry, error) {
	if read == nil {
		return gather
	}
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context) (*domain.Registry, error) {
		registry, err := gather(ctx)
		if err != nil {
			return nil, err
		}
		instant := now()
		certificates, err := read(ctx, instant.Add(-AgentCertificateWindow))
		if err != nil {
			return nil, err
		}
		registry.Add(domain.Metric{
			Name: "conduit_agent_certificates_tracked", Kind: domain.KindGauge, Value: float64(certificates.Agents),
			Help: "Agent identities that presented a client certificate to this Hub in the last 24 hours",
		})
		if certificates.Agents > 0 {
			registry.Add(domain.Metric{
				Name: "conduit_agent_certificate_min_seconds_until_expiry", Kind: domain.KindGauge,
				Value: certificates.EarliestNotAfter.Sub(instant).Seconds(),
				Help: "seconds until the soonest-expiring Agent client certificate seen in the last 24 hours lapses; " +
					"absent when no Agent has been seen",
			})
		}
		return registry, nil
	}
}
