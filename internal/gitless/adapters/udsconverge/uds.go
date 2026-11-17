// Package udsconverge realises Gitless GitOps and bundle mapping for UDS Core
// and Zarf environments (#427).
//
// Like fluxconverge, admission stays in internal/gitless/domain: this adapter
// receives an already-admitted domain.Admission and renders a digest-pinned
// UDSBundle and a UDS Core Package carrying the admitted publisher policy.
//
// Both documents are validated against the pinned upstream schemas in
// internal/udsschema before they are returned, and a document that does not
// validate is not returned at all. This adapter retains the legacy YAML
// bundle shape that uds-cli v0.41.0 reads: `kind: UDSBundle` with no apiVersion, `packages`, and
// `optionalComponents`.
//
// How a bundle pins a package (uds-cli v0.41.0, pkg/legacy/bundle/common.go,
// read 2026-11-17): the package is fetched from "<repository>:<ref>", and a
// ref of the form "<tag>@sha256:<hex>" is taken as already resolved to that
// manifest. A bare digest is not a ref -- it would be read as a tag -- so a
// pinned entry needs the tag as well as the digest, and this adapter refuses
// to render one without it. A local `path` entry has nowhere to carry a
// checksum, so it cannot be pinned and is refused.
package udsconverge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	yaml "go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/ports"
	"github.com/jonathanagustin/conduit/internal/udsschema"
)

// ManifestStore publishes rendered UDS bundle and Package manifests for an
// environment and reports the currently converged digest.
type ManifestStore interface {
	Put(ctx context.Context, environment string, manifest []byte) error
	CurrentDigest(ctx context.Context, environment string) (string, error)
}

// Options configure the UDS bundle and Package manifests rendered for an
// admitted source coordinate.
type Options struct {
	Namespace          string
	ServiceAccountName string
	PullSecretName     string
	Architecture       string
	Version            string
	// PackageTag is the tag the admitted Zarf package is published under. A
	// uds-cli bundle addresses a package by tag and pins it by appending the
	// digest, so the tag is required; it selects nothing the digest does not.
	PackageTag    string
	ExposePort    int
	ExposeService string
	ExposeHost    string
}

func (options Options) withDefaults() Options {
	if options.Namespace == "" {
		options.Namespace = "uds-package"
	}
	if options.Architecture == "" {
		options.Architecture = "amd64"
	}
	if options.Version == "" {
		options.Version = "0.1.0"
	}
	return options
}

func (options Options) validate() error {
	if strings.TrimSpace(options.ServiceAccountName) == "" {
		return errors.New("UDS convergence requires the bounded ServiceAccount it reconciles as")
	}
	if strings.TrimSpace(options.PullSecretName) == "" {
		return errors.New("UDS convergence requires the read-only pull Secret name")
	}
	if !packageTagPattern.MatchString(options.PackageTag) {
		return errors.New("UDS convergence requires the tag the Zarf package is published under; a bundle pins a package as <tag>@<digest>")
	}
	return nil
}

// An OCI tag, which is also what keeps "@" and ":" out of the rendered ref.
var packageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// pinnedRef is the uds-cli reference for one manifest of a tagged package.
func pinnedRef(tag, digest string) string { return tag + "@" + digest }

// Converger implements ports.Converger for UDS Core and Zarf environments.
type Converger struct {
	store   ManifestStore
	options Options
}

var _ ports.Converger = (*Converger)(nil)

// New constructs a UDS Converger backed by store.
func New(store ManifestStore, options Options) (*Converger, error) {
	if store == nil {
		return nil, errors.New("UDS convergence requires somewhere to publish manifests")
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &Converger{store: store, options: options.withDefaults()}, nil
}

// Converge renders the digest-pinned UDSBundle and Package manifests for
// admission and publishes them to the environment's store.
func (converger *Converger) Converge(ctx context.Context, environment string, admission domain.Admission) error {
	manifest, err := Render(environment, admission, converger.options)
	if err != nil {
		return err
	}
	return converger.store.Put(ctx, environment, manifest)
}

// Deployed reports the digest currently converged for environment.
func (converger *Converger) Deployed(ctx context.Context, environment string) (string, error) {
	return converger.store.CurrentDigest(ctx, environment)
}

// Render produces the two-document YAML stream (UDSBundle, then the
// uds.dev/v1alpha1 Package) for an admitted coordinate. The package is always
// referenced as <tag>@<digest>, never by tag alone, and each document has been
// validated against its pinned schema.
func Render(environment string, admission domain.Admission, options Options) ([]byte, error) {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return nil, errors.New("UDS manifests belong to a named environment")
	}
	options = options.withDefaults()
	if err := options.validate(); err != nil {
		return nil, err
	}
	pin := admission.Pin()
	if err := pin.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to render UDS manifests for an invalid pin: %w", err)
	}
	policy := admission.Policy()
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to render UDS manifests without a usable publisher policy: %w", err)
	}

	name := "conduit-" + environment

	bundleDoc := map[string]any{
		"kind": udsschema.BundleKind,
		"metadata": map[string]any{
			"name":         name,
			"version":      options.Version,
			"architecture": options.Architecture,
			// The bundle schema has no annotations. It does leave every object
			// open to "x-" keys, which uds-cli ignores, so what Conduit wants
			// to say about the bundle goes there.
			"x-conduit": map[string]any{
				"environment":     environment,
				"source-digest":   pin.Digest,
				"service-account": options.ServiceAccountName,
				"pull-secret":     options.PullSecretName,
			},
		},
		"packages": []any{
			map[string]any{
				"name":       name,
				"repository": pin.Repository,
				"ref":        pinnedRef(options.PackageTag, pin.Digest),
				// The admitted publisher policy is not a note beside the
				// package any more: these are the fields uds-cli verifies the
				// package signature against.
				"keylessVerification": map[string]any{
					"certificateIdentityRegexp":   policy.CertificateIdentity,
					"certificateOIDCIssuerRegexp": policy.CertificateIssuer,
				},
			},
		},
	}

	pkgSpec := map[string]any{
		"network": map[string]any{
			"expose": []any{},
			"allow": []any{
				map[string]any{
					"direction": "Ingress",
					"selector": map[string]any{
						"app.kubernetes.io/name": name,
					},
					"remoteGenerated": "IntraNamespace",
				},
			},
		},
	}
	if options.ExposePort > 0 {
		svc := options.ExposeService
		if svc == "" {
			svc = name
		}
		host := options.ExposeHost
		if host == "" {
			host = name
		}
		pkgSpec["network"].(map[string]any)["expose"] = []any{
			map[string]any{
				"service":    svc,
				"selector":   map[string]any{"app.kubernetes.io/name": name},
				"host":       host,
				"gateway":    "tenant",
				"port":       options.ExposePort,
				"targetPort": options.ExposePort,
			},
		}
	}

	packageDoc := map[string]any{
		"apiVersion": "uds.dev/v1alpha1",
		"kind":       "Package",
		"metadata": map[string]any{
			"name":      name,
			"namespace": options.Namespace,
			"labels": map[string]any{
				"app.kubernetes.io/name":       name,
				"app.kubernetes.io/managed-by": "conduit",
			},
			"annotations": map[string]any{
				"conduit.delivery/environment":          environment,
				"conduit.delivery/source-digest":        pin.Digest,
				"conduit.delivery/certificate-issuer":   policy.CertificateIssuer,
				"conduit.delivery/certificate-identity": policy.CertificateIdentity,
				"conduit.delivery/service-account":      options.ServiceAccountName,
				"conduit.delivery/pull-secret":          options.PullSecretName,
			},
		},
		"spec": pkgSpec,
	}

	bundle, err := encodeValidated("UDSBundle", bundleDoc, udsschema.ValidateBundle)
	if err != nil {
		return nil, err
	}
	pkg, err := encodeValidated("UDS Package", packageDoc, udsschema.ValidatePackage)
	if err != nil {
		return nil, err
	}
	return append(append(bundle, []byte("---\n")...), pkg...), nil
}

// encodeValidated renders one document and checks the rendered bytes -- not
// the map they came from -- against its pinned schema. What is validated is
// exactly what is returned.
func encodeValidated(what string, doc map[string]any, validate func([]byte) []udsschema.Violation) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("render %s: %w", what, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("render %s: %w", what, err)
	}
	if violations := validate(out.Bytes()); len(violations) > 0 {
		reasons := make([]string, 0, len(violations))
		for _, violation := range violations {
			reasons = append(reasons, violation.String())
		}
		return nil, fmt.Errorf("refusing to emit a %s the pinned schema rejects: %s", what, strings.Join(reasons, "; "))
	}
	return out.Bytes(), nil
}

// BundlePackageRef describes one Zarf package entry inside a Conduit-mapped
// UDSBundle: an OCI Repository, the Tag it is published under, and the Digest
// that pins it.
//
// Path and SHA256 describe a local archive. They are still accepted as input
// so that a caller gets a reason rather than a compile error, but such an
// entry is refused: the uds-cli bundle schema has no field for an archive
// checksum, so a local path cannot be pinned.
type BundlePackageRef struct {
	Name               string   `json:"name"`
	Repository         string   `json:"repository,omitempty"`
	Tag                string   `json:"tag,omitempty"`
	Digest             string   `json:"digest,omitempty"`
	Path               string   `json:"path,omitempty"`
	SHA256             string   `json:"sha256,omitempty"`
	OptionalComponents []string `json:"optionalComponents,omitempty"`
}

// RenderBundleFromPackages maps Conduit bundle/package coordinates into a
// digest-pinned UDSBundle YAML document, validated against the pinned uds-cli
// schema. Every package must be an OCI repository pinned as <tag>@<digest>.
func RenderBundleFromPackages(name, version, architecture string, packages []BundlePackageRef) ([]byte, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	architecture = strings.TrimSpace(architecture)
	if name == "" {
		return nil, errors.New("UDSBundle name is required")
	}
	if version == "" {
		return nil, errors.New("UDSBundle version is required")
	}
	if architecture == "" {
		architecture = "amd64"
	}
	if len(packages) == 0 {
		return nil, errors.New("UDSBundle requires at least one pinned Zarf package")
	}
	entries := make([]any, 0, len(packages))
	for _, pkg := range packages {
		pkgName := strings.TrimSpace(pkg.Name)
		if pkgName == "" {
			return nil, errors.New("every Zarf package in a UDSBundle requires a name")
		}
		entry := map[string]any{"name": pkgName}
		switch {
		case strings.TrimSpace(pkg.Repository) != "":
			digest := strings.TrimSpace(pkg.Digest)
			if !bundleDigestPattern.MatchString(digest) {
				return nil, fmt.Errorf("package %q repository reference requires a 64-hex sha256 digest", pkgName)
			}
			if !packageTagPattern.MatchString(pkg.Tag) {
				return nil, fmt.Errorf("package %q requires the tag it is published under; a bundle pins a package as <tag>@<digest>", pkgName)
			}
			repository := strings.TrimPrefix(strings.TrimSpace(pkg.Repository), "oci://")
			if strings.Contains(repository, "@") {
				return nil, fmt.Errorf("package %q repository must not carry a digest; the ref does", pkgName)
			}
			entry["repository"] = repository
			entry["ref"] = pinnedRef(pkg.Tag, digest)
		case strings.TrimSpace(pkg.Path) != "":
			return nil, fmt.Errorf("package %q is a local path, which a uds-cli bundle cannot pin by checksum; publish it and reference it by repository, tag and digest", pkgName)
		default:
			return nil, fmt.Errorf("package %q requires an OCI repository pinned by tag and digest", pkgName)
		}
		if len(pkg.OptionalComponents) > 0 {
			entry["optionalComponents"] = pkg.OptionalComponents
		}
		entries = append(entries, entry)
	}
	doc := map[string]any{
		"kind":     udsschema.BundleKind,
		"metadata": map[string]any{"name": name, "version": version, "architecture": architecture},
		"packages": entries,
	}
	return encodeValidated("UDSBundle", doc, udsschema.ValidateBundle)
}

var bundleDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// PackageStatusObservation holds the normalized state read back from a UDS
// Core `uds.dev/v1alpha1` Package CR in-cluster.
type PackageStatusObservation struct {
	Name               string   `json:"name"`
	Namespace          string   `json:"namespace"`
	Phase              string   `json:"phase"`
	Generation         int64    `json:"generation"`
	ObservedGeneration int64    `json:"observedGeneration"`
	SourceDigest       string   `json:"sourceDigest,omitempty"`
	Endpoints          []string `json:"endpoints,omitempty"`
	SSOClients         []string `json:"ssoClients,omitempty"`
	Healthy            bool     `json:"healthy"`
	Converged          bool     `json:"converged"`
	Message            string   `json:"message,omitempty"`
}

// ParsePackageStatus parses a UDS `uds.dev/v1alpha1` Package resource (YAML or
// JSON) and projects its reconciliation phase, generation convergence, exposed
// endpoints, and SSO clients.
func ParsePackageStatus(raw []byte) (PackageStatusObservation, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return PackageStatusObservation{}, errors.New("empty UDS Package document")
	}
	var doc struct {
		APIVersion string `json:"apiVersion" yaml:"apiVersion"`
		Kind       string `json:"kind" yaml:"kind"`
		Metadata   struct {
			Name        string            `json:"name" yaml:"name"`
			Namespace   string            `json:"namespace" yaml:"namespace"`
			Generation  int64             `json:"generation" yaml:"generation"`
			Annotations map[string]string `json:"annotations" yaml:"annotations"`
		} `json:"metadata" yaml:"metadata"`
		Status struct {
			Phase              string   `json:"phase" yaml:"phase"`
			ObservedGeneration int64    `json:"observedGeneration" yaml:"observedGeneration"`
			Endpoints          []string `json:"endpoints" yaml:"endpoints"`
			SSOClients         []string `json:"ssoClients" yaml:"ssoClients"`
			Message            string   `json:"message" yaml:"message"`
		} `json:"status" yaml:"status"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		if jsonErr := json.Unmarshal(raw, &doc); jsonErr != nil {
			return PackageStatusObservation{}, fmt.Errorf("parse UDS Package document: %w", err)
		}
	}
	if doc.Kind != "Package" || !strings.HasPrefix(doc.APIVersion, "uds.dev/") {
		return PackageStatusObservation{}, fmt.Errorf("expected uds.dev Package resource, got %s/%s", doc.APIVersion, doc.Kind)
	}
	phase := strings.TrimSpace(doc.Status.Phase)
	generationMatch := doc.Metadata.Generation == 0 || doc.Status.ObservedGeneration >= doc.Metadata.Generation
	ready := strings.EqualFold(phase, "Ready") && generationMatch
	return PackageStatusObservation{
		Name:               doc.Metadata.Name,
		Namespace:          doc.Metadata.Namespace,
		Phase:              phase,
		Generation:         doc.Metadata.Generation,
		ObservedGeneration: doc.Status.ObservedGeneration,
		SourceDigest:       doc.Metadata.Annotations["conduit.delivery/source-digest"],
		Endpoints:          doc.Status.Endpoints,
		SSOClients:         doc.Status.SSOClients,
		Healthy:            ready,
		Converged:          ready,
		Message:            doc.Status.Message,
	}, nil
}
