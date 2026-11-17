package main

import (
	"context"
	"time"

	agentcredentials "github.com/jonathanagustin/conduit/internal/identity/adapters/revocationpostgres"
	identityports "github.com/jonathanagustin/conduit/internal/identity/ports"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
)

// agentCredentialsFor returns the durable Agent deny-list when one was opened.
// A typed nil pointer stored in the interface would read as configured and
// then fail every Agent request closed, so the file repository returns an
// untyped nil and the Hub keeps the deny-list in memory.
func agentCredentialsFor(component notificationComponent) identityports.AgentCredentialStore {
	if component.AgentCredentials == nil {
		return nil
	}
	return component.AgentCredentials
}

// agentCertificateMetrics narrows the Agent credential store to the one
// aggregate the metrics endpoint publishes: the soonest expiry and a count,
// and no Agent or Environment.
func agentCertificateMetrics(store identityports.AgentCredentialStore) metricsapplication.ReadAgentCertificates {
	if store == nil {
		return nil
	}
	return func(ctx context.Context, seenSince time.Time) (metricsapplication.AgentCertificates, error) {
		earliest, agents, err := store.EarliestExpiry(ctx, seenSince)
		if err != nil {
			return metricsapplication.AgentCertificates{}, err
		}
		return metricsapplication.AgentCertificates{EarliestNotAfter: earliest, Agents: agents}, nil
	}
}

// agentCertificateExtension adds the Agent certificate gauges to the metrics
// gather. A nil store, which is what the file repository has, adds nothing: the
// pointer is checked here because a nil pointer stored in the port's interface
// would not compare equal to nil inside agentCertificateMetrics.
func agentCertificateExtension(store *agentcredentials.Store) metricsExtension {
	if store == nil {
		return nil
	}
	read := agentCertificateMetrics(store)
	return func(gather metricsGather) metricsGather {
		return metricsapplication.WithAgentCertificates(gather, read, time.Now)
	}
}
