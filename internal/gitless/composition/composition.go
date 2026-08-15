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
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/conduitconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/configpolicy"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/cosignverify"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/fluxconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
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

	// Interval is how often desired state is restated.
	Interval time.Duration

	// CosignBinary is the verifier. Empty resolves "cosign" on PATH.
	CosignBinary string

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
	// ManifestStore publishes rendered manifests for a cluster's Flux to pull.
	// It is supplied rather than constructed because publishing is a registry
	// operation whose credentials belong to the caller.
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


	if strings.TrimSpace(config.PolicyFile) == "" {
		return nil, fmt.Errorf("Gitless GitOps needs a publisher policy file; there is no safe default for who may publish")
	}
	if strings.TrimSpace(config.ResolutionLogFile) == "" {
		return nil, fmt.Errorf("Gitless GitOps needs a resolution log; a deployment nobody can attribute is what it exists to prevent")
	}

	policies, err := configpolicy.Load(config.PolicyFile)
	if err != nil {
		return nil, err
	}
	// Every bound environment must have a policy before anything starts. Left
	// to the first pass, a missing one would be a Hub that ran cleanly and
	// silently never deployed that environment.


	log, err := resolutionlog.Open(config.ResolutionLogFile)
	if err != nil {
		return nil, err
	}

	converger, err := converger(config, conduit)
	if err != nil {
		return nil, err
	}

	source, err := application.New(
		ociresolve.New(nil, ociresolve.Anonymous{}),
		cosignverify.New(config.CosignBinary),
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
		if config.Flux.ManifestStore == nil {
			return nil, fmt.Errorf("Flux convergence needs somewhere to publish rendered manifests")
		}
		return fluxconverge.New(config.Flux.ManifestStore, fluxconverge.Options{
			Namespace:          config.Flux.Namespace,
			ServiceAccountName: config.Flux.ServiceAccountName,
			PullSecretName:     config.Flux.PullSecretName,
			Path:               config.Flux.Path,
		})
	case "conduit":
		return conduitconverge.New(conduit.Fetcher, conduit.DesiredState, 0)
	case "":
		return fluxconverge.New(config.Flux.ManifestStore, fluxconverge.Options{
			ServiceAccountName: config.Flux.ServiceAccountName, PullSecretName: config.Flux.PullSecretName})
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
