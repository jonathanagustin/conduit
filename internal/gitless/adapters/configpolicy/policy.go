// Package configpolicy answers which publisher an environment trusts, from a
// reviewed configuration file.
//
// The rule that shapes it: an environment with no policy is refused, not given
// a default. A default publisher policy is a policy nobody reviewed, applied to
// the environment somebody forgot — which is reliably the one that mattered.
// Adding an environment here is therefore a deliberate act with a diff.
//
// A policy this package returns still has to satisfy domain.PublisherPolicy's
// own rules, notably that both patterns are anchored. Validating on load rather
// than at first use means a malformed file fails when it is read, not during
// the deployment that needed it.
package configpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

type file struct {
	// Environments maps an environment name to the publisher it trusts. There
	// is deliberately no "default" key.
	Environments map[string]struct {
		CertificateIssuer   string `json:"certificateIssuer"`
		CertificateIdentity string `json:"certificateIdentity"`
	} `json:"environments"`
}

// Source answers PolicyFor from an immutable snapshot read at construction.
type Source struct {
	policies map[string]domain.PublisherPolicy
}

// Load reads and validates every policy in the file.
func Load(path string) (*Source, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read publisher policies: %w", err)
	}
	var decoded file
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	// An unknown field is a typo in a security policy. Silently ignoring
	// `certificateIdentiy` would leave the environment trusting an empty
	// pattern, which is the failure this file exists to prevent.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("parse publisher policies in %s: %w", path, err)
	}
	if len(decoded.Environments) == 0 {
		return nil, fmt.Errorf("%s declares no environment, so nothing could ever be deployed", path)
	}

	policies := make(map[string]domain.PublisherPolicy, len(decoded.Environments))
	names := make([]string, 0, len(decoded.Environments))
	for name := range decoded.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := decoded.Environments[name]
		policy := domain.PublisherPolicy{
			CertificateIssuer:   entry.CertificateIssuer,
			CertificateIdentity: entry.CertificateIdentity,
		}
		if err := policy.Validate(); err != nil {
			return nil, fmt.Errorf("publisher policy for %s in %s: %w", name, path, err)
		}
		policies[name] = policy
	}
	return &Source{policies: policies}, nil
}

func (source *Source) PolicyFor(_ context.Context, environment string) (domain.PublisherPolicy, error) {
	policy, known := source.policies[environment]
	if !known {
		return domain.PublisherPolicy{}, fmt.Errorf(
			"no publisher policy is declared for environment %q, so nothing may be deployed to it. "+
				"Add one rather than relying on a default: a default policy is one nobody reviewed",
			environment)
	}
	return policy, nil
}

// Environments lists what this source knows, so a startup log can state the
// scope rather than leaving it to be discovered one failed deployment at a time.
func (source *Source) Environments() []string {
	names := make([]string, 0, len(source.policies))
	for name := range source.policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
