// Package udsconverge realises Gitless GitOps and bundle mapping for UDS Core
// and Zarf environments (#427).
//
// Like fluxconverge, admission stays in internal/gitless/domain: this adapter
// receives an already-admitted domain.Admission, refuses mutable tags, and
// renders digest-pinned UDSBundle (uds.dev/v1alpha1) and UDS Package
// (uds.dev/v1alpha1) manifests carrying the admitted publisher policy.
package udsconverge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/ports"
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
	ExposePort         int
	ExposeService      string
	ExposeHost         string
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
	return nil
}

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

// Render produces the two-document YAML stream (UDSBundle + uds.dev/v1alpha1
// Package) for an admitted coordinate. It never emits a mutable tag.
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
	ociRef := fmt.Sprintf("oci://%s@%s", pin.Repository, pin.Digest)

	bundleDoc := map[string]any{
		"kind":       "UDSBundle",
		"apiVersion": "uds.dev/v1alpha1",
		"metadata": map[string]any{
			"name":         name,
			"version":      options.Version,
			"architecture": options.Architecture,
			"annotations": map[string]any{
				"conduit.delivery/environment":          environment,
				"conduit.delivery/source-digest":        pin.Digest,
				"conduit.delivery/certificate-issuer":   policy.CertificateIssuer,
				"conduit.delivery/certificate-identity": policy.CertificateIdentity,
				"conduit.delivery/service-account":      options.ServiceAccountName,
				"conduit.delivery/pull-secret":          options.PullSecretName,
			},
		},
		"zarf-packages": []any{
			map[string]any{
				"name":       name,
				"repository": ociRef,
				"ref":        pin.Digest,
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

	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	for _, doc := range []map[string]any{bundleDoc, packageDoc} {
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("render UDS manifests: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("render UDS manifests: %w", err)
	}
	return rendered.Bytes(), nil
}

// BundlePackageRef describes one Zarf package entry inside a Conduit-mapped
// UDSBundle. Either Repository+Digest (OCI) or Path+SHA256 (local archive)
// must be supplied.
type BundlePackageRef struct {
	Name               string   `json:"name"`
	Repository         string   `json:"repository,omitempty"`
	Digest             string   `json:"digest,omitempty"`
	Path               string   `json:"path,omitempty"`
	SHA256             string   `json:"sha256,omitempty"`
	OptionalComponents []string `json:"optionalComponents,omitempty"`
}

// RenderBundleFromPackages maps Conduit bundle/package coordinates into a
// digest-pinned UDSBundle YAML document. Every package must be pinned by
// sha256 digest (OCI) or sha256 archive checksum (local path).
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
			if !strings.HasPrefix(strings.TrimSpace(pkg.Digest), "sha256:") || len(strings.TrimSpace(pkg.Digest)) != 71 {
				return nil, fmt.Errorf("package %q repository reference requires a 64-hex sha256 digest", pkgName)
			}
			entry["repository"] = fmt.Sprintf("oci://%s@%s", strings.TrimPrefix(strings.TrimSpace(pkg.Repository), "oci://"), strings.TrimSpace(pkg.Digest))
			entry["ref"] = strings.TrimSpace(pkg.Digest)
		case strings.TrimSpace(pkg.Path) != "":
			sum := strings.TrimPrefix(strings.TrimSpace(pkg.SHA256), "sha256:")
			if len(sum) != 64 {
				return nil, fmt.Errorf("package %q local path requires a 64-hex sha256 checksum", pkgName)
			}
			entry["path"] = strings.TrimSpace(pkg.Path)
			entry["ref"] = "sha256:" + sum
		default:
			return nil, fmt.Errorf("package %q requires either a pinned OCI repository+digest or local path+sha256", pkgName)
		}
		if len(pkg.OptionalComponents) > 0 {
			entry["optional-components"] = pkg.OptionalComponents
		}
		entries = append(entries, entry)
	}
	doc := map[string]any{
		"kind":          "UDSBundle",
		"apiVersion":    "uds.dev/v1alpha1",
		"metadata":      map[string]any{"name": name, "version": version, "architecture": architecture},
		"zarf-packages": entries,
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

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
