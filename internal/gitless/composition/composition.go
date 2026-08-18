// Package composition is the Gitless GitOps composition root.
//
// It is the only place concrete adapters are chosen. The Hub's main function
// hands it configuration and gets back something it can run; nothing above it
// knows there is a registry, a cosign binary or a Flux manifest involved, and
// nothing below it knows there is a Hub.
//
// The rule it enforces is that Gitless GitOps is off unless it was configured
// on. `Configure` returns a nil scheduler and no error when no bindings are
// declared, so a Hub that says nothing about it starts exactly as before. The
// alternative -- defaulting to some source -- would mean an upgrade could begin
// deploying things nobody asked for.
package composition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/conduitconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/configpolicy"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/cosignverify"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/fluxconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ocipublish"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlog"
	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/ports"
)

// Config is what a Hub must state to turn Gitless GitOps on.
//
// Every field is required when Bindings is non-empty. There are no defaults for
// the security-relevant ones: a publisher policy file, a resolution log and a
// convergence choice are decisions, and inferring any of them would be
// inventing a security posture on an operator's behalf.
type Config struct {
	// Bindings is "environment=repository:channel" or
	// "environment=repository@sha256:...", one per entry. Empty means off.
	Bindings []string

	// PolicyFile declares which publisher each environment trusts.
	PolicyFile string

	// ResolutionLogFile is where resolutions are appended before deployment.
	ResolutionLogFile string
	// ResolutionLog supplies a shared durable implementation. Production
	// multi-replica Hubs use PostgreSQL; nil retains the file adapter for the
	// explicit single-replica profile.
	ResolutionLog ports.ResolutionLog

	// Interval is how often desired state is restated.
	Interval time.Duration

	// CosignBinary is the verifier. Empty resolves "cosign" on PATH.
	CosignBinary string
	// CosignArguments are appended before the immutable artifact reference, for
	// a non-default trust root or registry TLS material. Callers must never put
	// credential contents in arguments: argv is world-readable.
	CosignArguments []string
	// CosignRegistryConfigDirectory is the directory holding the Docker
	// config.json cosign authenticates private registries with, exported to it
	// as DOCKER_CONFIG. cosign has no flag for this -- see the cosignverify
	// package -- so it travels by environment or not at all.
	CosignRegistryConfigDirectory string

	// RegistryCredentials authenticate source resolution. Nil means anonymous,
	// which remains the right profile for a public source registry.
	RegistryCredentials ociresolve.Credentials

	// TokenRealms declares, per registry host, which other hosts that registry
	// may direct a credential to when it answers with a Bearer challenge. Nil
	// means each registry may only direct credentials at itself. It reaches both
	// the resolver and the publisher, because the question is the same one on
	// both sides and answering it twice is how one side gets fixed.
	TokenRealms registrychallenge.Allowlist

	// Convergence is "flux" or "conduit". There is no default: which one is in
	// use changes who owns approval, which is not a thing to fall back into.
	Convergence string

	// Flux is required when Convergence is "flux".
	Flux FluxConfig
}

// FluxConfig carries the cluster-side facts a rendered manifest needs.
type FluxConfig struct {
	Namespace          string
	ServiceAccountName string
	PullSecretName     string
	Path               string

	// ManifestRepository is where rendered manifests are published for a
	// cluster's Flux to pull, one tag per environment.
	ManifestRepository string
	// InsecureRegistry sends http rather than https. Explicit rather than
	// inferred from the host, so it can never be switched on by a hostname
	// somebody chose.
	InsecureRegistry bool
	// RegistryCredentials authenticates the push. Nil means anonymous.
	RegistryCredentials ocipublish.Credentials

	// ManifestStore overrides the publisher entirely. It exists for a
	// deployment that publishes some other way, and for the acceptance harness,
	// which applies straight to a cluster instead.
	ManifestStore fluxconverge.ManifestStore
}

// ConduitConfig carries what the Conduit-owned convergence path needs.
type ConduitConfig struct {
	Fetcher      conduitconverge.ArtifactFetcher
	DesiredState conduitconverge.DesiredStateStore
}

// Configure assembles the scheduler, or reports that Gitless GitOps is off.
//
// A nil scheduler with a nil error means "not configured", which the caller
// should treat as a normal state and not as a failure to start.
func Configure(config Config, conduit ConduitConfig, observer application.Observer) (*application.Scheduler, error) {
	bindings, err := ParseBindings(config.Bindings)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}

	if strings.TrimSpace(config.PolicyFile) == "" {
		return nil, fmt.Errorf("Gitless GitOps needs a publisher policy file; there is no safe default for who may publish")
	}
	if config.ResolutionLog != nil && strings.TrimSpace(config.ResolutionLogFile) != "" {
		return nil, fmt.Errorf("Gitless GitOps resolution log must be either injected or file-backed, not both")
	}
	if config.ResolutionLog == nil && strings.TrimSpace(config.ResolutionLogFile) == "" {
		return nil, fmt.Errorf("Gitless GitOps needs a resolution log; a deployment nobody can attribute is what it exists to prevent")
	}

	policies, err := configpolicy.Load(config.PolicyFile)
	if err != nil {
		return nil, err
	}
	// Every bound environment must have a policy before anything starts. Left
	// to the first pass, a missing one would be a Hub that ran cleanly and
	// silently never deployed that environment.
	for _, binding := range bindings {
		if _, err := policies.PolicyFor(context.Background(), binding.Environment); err != nil {
			return nil, err
		}
	}

	log := config.ResolutionLog
	if log == nil {
		log, err = resolutionlog.Open(config.ResolutionLogFile)
		if err != nil {
			return nil, err
		}
	}

	converger, err := converger(config, conduit)
	if err != nil {
		return nil, err
	}

	source, err := application.New(
		ociresolve.New(nil, config.RegistryCredentials, ociresolve.WithTokenRealms(config.TokenRealms)),
		cosignverify.New(config.CosignBinary,
			cosignverify.WithArguments(config.CosignArguments...),
			cosignverify.WithRegistryConfigDirectory(config.CosignRegistryConfigDirectory)),
		policies,
		log,
		converger,
	)
	if err != nil {
		return nil, err
	}
	return application.NewScheduler(source, bindings, config.Interval, observer)
}

func converger(config Config, conduit ConduitConfig) (ports.Converger, error) {
	switch strings.TrimSpace(config.Convergence) {
	case "flux":
		store := config.Flux.ManifestStore
		if store == nil {
			if strings.TrimSpace(config.Flux.ManifestRepository) == "" {
				return nil, fmt.Errorf(
					"Flux convergence needs a manifest repository to publish to, or an explicit store")
			}
			published, err := ocipublish.New(
				config.Flux.ManifestRepository, nil, config.Flux.RegistryCredentials, config.Flux.InsecureRegistry,
				ocipublish.WithTokenRealms(config.TokenRealms))
			if err != nil {
				return nil, err
			}
			store = published
		}
		return fluxconverge.New(store, fluxconverge.Options{
			Namespace:          config.Flux.Namespace,
			ServiceAccountName: config.Flux.ServiceAccountName,
			PullSecretName:     config.Flux.PullSecretName,
			Path:               config.Flux.Path,
		})
	case "conduit":
		return conduitconverge.New(conduit.Fetcher, conduit.DesiredState, 0)
	case "":
		return nil, fmt.Errorf(
			"Gitless GitOps needs a convergence choice of \"flux\" or \"conduit\"; which one is in use decides " +
				"whether a source change passes through Conduit's approval semantics, so there is no default")
	}
	return nil, fmt.Errorf("unknown convergence %q; expected \"flux\" or \"conduit\"", config.Convergence)
}

// ParseBindings reads "environment=repository:channel" or
// "environment=repository@digest" entries.
//
// It is exported so a Hub can validate its configuration without building
// anything, and so the error a misconfiguration produces names the entry that
// caused it rather than the list it was in.
func ParseBindings(entries []string) ([]application.Binding, error) {
	bindings := make([]application.Binding, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		environment, source, found := strings.Cut(entry, "=")
		if !found {
			return nil, fmt.Errorf("binding %q must be environment=repository:channel", entry)
		}
		selector, err := ParseSelector(strings.TrimSpace(source))
		if err != nil {
			return nil, fmt.Errorf("binding %q: %w", entry, err)
		}
		bindings = append(bindings, application.Binding{
			Environment: strings.TrimSpace(environment), Selector: selector,
		})
	}
	return bindings, nil
}

// ParseSelector reads one source reference. The digest form is checked first,
// because a reference carrying "@sha256:" is never a channel however it is
// punctuated.
func ParseSelector(reference string) (domain.Selector, error) {
	if repository, digest, found := strings.Cut(reference, "@"); found {
		return domain.Selector{
			Kind: domain.ByDigest, Repository: strings.TrimSpace(repository), Digest: strings.TrimSpace(digest),
		}, nil
	}
	// A registry host may carry a port, so the channel separator is the last
	// colon after the last slash rather than the first colon in the string.
	slash := strings.LastIndex(reference, "/")
	colon := strings.LastIndex(reference, ":")
	if colon < 0 || colon < slash {
		return domain.Selector{}, fmt.Errorf(
			"%q names no channel or digest; write repository:channel or repository@sha256:...", reference)
	}
	return domain.Selector{
		Kind:       domain.ByChannel,
		Repository: strings.TrimSpace(reference[:colon]),
		Channel:    strings.TrimSpace(reference[colon+1:]),
	}, nil
}
