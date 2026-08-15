// Package fluxconverge realises Gitless GitOps by delegating convergence to
// Flux.
//
// Conduit admits the bytes; Flux makes the cluster match them. What this
// adapter produces is the pair of manifests Flux reads -- an OCIRepository
// pinned to the admitted digest, and a Kustomization that prunes and waits --
// carrying the same publisher policy the admission satisfied.
//
// The policy is written into the manifest deliberately, even though Conduit has
// already checked it. Flux verifies again in-cluster, and it must verify the
// same thing: a manifest that named a laxer identity than the admission used
// would mean the cluster would accept bytes Conduit refused, which is precisely
// the divergence putting admission in the domain was meant to prevent.
//
// Nothing here selects by tag. `spec.ref.digest` is the only selector emitted,
// so moving a channel cannot change what a cluster is already running -- a new
// digest has to be admitted, recorded and published first.
package fluxconverge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// ManifestStore is where rendered manifests are published for a cluster's Flux
// to pull, and what reports back which digest a cluster is currently on.
//
// It is declared here rather than in ports because it is this adapter's own
// need: the Conduit converger has no manifests to publish.
type ManifestStore interface {
	Put(ctx context.Context, environment string, manifest []byte) error
	CurrentDigest(ctx context.Context, environment string) (string, error)
}

// Options are the cluster-side facts a manifest needs that an admission does
// not carry: which namespace Flux reads from, which identity it reconciles as,
// and which Secret lets it pull.
type Options struct {
	Namespace          string
	ServiceAccountName string
	PullSecretName     string
	Path               string
	Interval           string
	RetryInterval      string
	Timeout            string
}

func (options Options) withDefaults() Options {
	if options.Namespace == "" {
		options.Namespace = "flux-system"
	}
	if options.Path == "" {
		options.Path = "./runtime"
	}
	if options.Interval == "" {
		options.Interval = "5m"
	}
	if options.RetryInterval == "" {
		options.RetryInterval = "30s"
	}
	if options.Timeout == "" {
		options.Timeout = "10m"
	}
	return options
}

func (options Options) validate() error {
	// These two have no safe default. Reconciling as whatever identity Flux
	// happens to have, or pulling with whatever credential is lying around, are
	// the two ways this adapter could quietly widen authority, so it refuses to
	// guess at either.
	if strings.TrimSpace(options.ServiceAccountName) == "" {
		return errors.New("Flux convergence requires the bounded ServiceAccount it reconciles as")
	}
	if strings.TrimSpace(options.PullSecretName) == "" {
		return errors.New("Flux convergence requires the read-only pull Secret name")
	}
	return nil
}

// Converger is the ports.Converger backed by Flux.
type Converger struct {
	store   ManifestStore
	options Options
}

func New(store ManifestStore, options Options) (*Converger, error) {
	if store == nil {
		return nil, errors.New("Flux convergence requires somewhere to publish manifests")
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &Converger{store: store, options: options.withDefaults()}, nil
}

func (converger *Converger) Converge(ctx context.Context, environment string, admission domain.Admission) error {
	manifest, err := Render(environment, admission, converger.options)
	if err != nil {
		return err
	}
	return converger.store.Put(ctx, environment, manifest)
}

func (converger *Converger) Deployed(ctx context.Context, environment string) (string, error) {
	return converger.store.CurrentDigest(ctx, environment)
}

// Render produces the manifests for one admission. It is exported and pure so
// the shape can be tested, and gated, without a registry or a cluster.
func Render(environment string, admission domain.Admission, options Options) ([]byte, error) {
	if strings.TrimSpace(environment) == "" {
		return nil, errors.New("Flux manifests belong to a named environment")
	}
	options = options.withDefaults()
	if err := options.validate(); err != nil {
		return nil, err
	}
	pin := admission.Pin()
	if err := pin.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to render manifests for an invalid pin: %w", err)
	}
	policy := admission.Policy()
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to render manifests without a usable publisher policy: %w", err)
	}

	name := "conduit-" + environment
	repository := map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "OCIRepository",
		"metadata":   map[string]any{"name": name, "namespace": options.Namespace},
		"spec": map[string]any{
			"interval":  options.Interval,
			"url":       "oci://" + pin.Repository,
			"secretRef": map[string]any{"name": options.PullSecretName},
			// Digest only. No tag, no semver range: what a cluster runs changes
			// when a new digest is admitted, never when a name is moved.
			"ref": map[string]any{"digest": pin.Digest},
			"verify": map[string]any{
				"provider": "cosign",
				"matchOIDCIdentity": []any{map[string]any{
					"issuer":  policy.CertificateIssuer,
					"subject": policy.CertificateIdentity,
				}},
			},
		},
	}
	kustomization := map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": name, "namespace": options.Namespace},
		"spec": map[string]any{
			"interval":      options.Interval,
			"retryInterval": options.RetryInterval,
			"timeout":       options.Timeout,
			"path":          options.Path,
			"prune":         true,
			"wait":          true,
			// Impersonation is not optional. Without it the Kustomization
			// reconciles with the controller's own authority, which is
			// cluster-wide.
			"serviceAccountName": options.ServiceAccountName,
			"sourceRef":          map[string]any{"kind": "OCIRepository", "name": name},
		},
	}

	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	for _, document := range []map[string]any{repository, kustomization} {
		if err := encoder.Encode(document); err != nil {
			return nil, fmt.Errorf("render Flux manifests: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("render Flux manifests: %w", err)
	}
	return rendered.Bytes(), nil
}
