// Package composition is the Multipass federation's composition root.
//
// It is the only place the two directory adapters are chosen between, on the
// same shape Gitless GitOps uses: a Hub states configuration, gets back
// something satisfying the port, and nothing above it knows whether there is an
// HTTP client or a map behind it.
//
// The rule it enforces is that federation is OFF unless it was configured on. A
// nil directory with a nil error is the normal state and means "nobody asked
// for this". There is no default source, because defaulting to one would mean
// an upgrade began consulting an identity service nobody named — and, worse,
// defaulting to the offline fixture would mean a deployment that misconfigured
// its real Multipass silently federated two invented users instead of failing.
//
// NOTHING IN THIS REPOSITORY CALLS Configure. The federation is inert: no
// route, no handler and no authorization path reaches a MultipassDirectory. See
// the package's pull request for exactly what would have to change to activate
// it.
package composition

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/multipasshttp"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/multipassoffline"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// The environment variables a deployment turns federation on with. They follow
// the notification transport's convention exactly: one variable names the real
// destination, one turns on the substitute, and setting both is refused rather
// than resolved by precedence.
const (
	// MultipassBaseURLVariable names the Multipass deployment to read identity
	// from. Setting it is the whole opt-in for the real adapter.
	MultipassBaseURLVariable = "MULTIPASS_BASE_URL"
	// MultipassTokenVariable carries the bearer token those reads are made
	// with. Optional: a deployment fronting Multipass with a mesh that injects
	// credentials supplies none here.
	MultipassTokenVariable = "MULTIPASS_TOKEN"
	// MultipassTimeoutVariable bounds one directory read. Optional.
	MultipassTimeoutVariable = "MULTIPASS_TIMEOUT"
	// MultipassOfflineVariable turns on the in-memory fixture instead, for a
	// demonstration or a workstation with no Multipass to reach.
	MultipassOfflineVariable = "MULTIPASS_OFFLINE"
	// MultipassUsernamePrefixVariable is the namespace federated usernames are
	// minted under. See domain.FederationPolicy for why it exists.
	MultipassUsernamePrefixVariable = "MULTIPASS_USERNAME_PREFIX"
	// MultipassGroupPrefixVariable is the same for group names.
	MultipassGroupPrefixVariable = "MULTIPASS_GROUP_PREFIX"
)

// Configure builds the directory an operator asked for, or reports that none
// was asked for.
//
// A nil directory with a nil error is the unconfigured case and is normal. A
// misconfiguration is an error rather than a silent fallback, on the
// notification transport's reasoning: a Hub that started cleanly and then
// resolved every identity against a fixture is worse than one that refused to
// start.
func Configure(getenv func(string) string) (ports.MultipassDirectory, error) {
	if getenv == nil {
		return nil, errors.New("an environment reader is required to configure Multipass federation")
	}
	baseURL := strings.TrimSpace(getenv(MultipassBaseURLVariable))
	offline := strings.TrimSpace(getenv(MultipassOfflineVariable)) == "true"
	timeoutValue := strings.TrimSpace(getenv(MultipassTimeoutVariable))
	token := getenv(MultipassTokenVariable)

	if baseURL != "" && offline {
		// Refused rather than resolved by precedence. Both are identity sources,
		// and a Hub that quietly picked one would federate against the loser's
		// directory exactly never while its configuration says otherwise.
		return nil, fmt.Errorf("%s and %s are both set; choose one Multipass identity source",
			MultipassBaseURLVariable, MultipassOfflineVariable)
	}
	if baseURL == "" && strings.TrimSpace(token) != "" {
		return nil, fmt.Errorf("%s requires %s", MultipassTokenVariable, MultipassBaseURLVariable)
	}
	if baseURL == "" && timeoutValue != "" {
		return nil, fmt.Errorf("%s requires %s", MultipassTimeoutVariable, MultipassBaseURLVariable)
	}
	switch {
	case baseURL != "":
		options := []multipasshttp.Option{multipasshttp.WithBearerToken(token)}
		if timeoutValue != "" {
			timeout, err := time.ParseDuration(timeoutValue)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", MultipassTimeoutVariable, err)
			}
			if timeout <= 0 {
				return nil, fmt.Errorf("%s must be positive", MultipassTimeoutVariable)
			}
			options = append(options, multipasshttp.WithHTTPClient(&http.Client{Timeout: timeout}))
		}
		// Returned through an explicit nil rather than as `return
		// multipasshttp.New(...)`. That form hands back a typed nil pointer
		// inside a non-nil interface, so a caller checking "directory == nil" to
		// decide whether federation is on would find it on, holding an adapter
		// whose every method dereferences nothing.
		directory, err := multipasshttp.New(baseURL, options...)
		if err != nil {
			return nil, err
		}
		return directory, nil
	case offline:
		return multipassoffline.Seeded(), nil
	default:
		return nil, nil
	}
}

// FederationPolicy reads the naming policy federated identities are minted
// under.
//
// It is separate from Configure because the two answer different questions and
// a deployment can get one right and the other wrong. Configure answers "which
// directory"; this answers "what are its principals called here", which is the
// half that decides whether a Multipass account can land on a name this
// deployment already means something by.
//
// bootstrapAdministrators is passed in rather than read from the environment.
// The control plane already owns that list, and a second reader of it is the
// mechanism of the defect ResourceDecision's own comment records: two copies of
// "who is an administrator" that both look right and disagree.
func FederationPolicy(getenv func(string) string, bootstrapAdministrators []string) (domain.FederationPolicy, error) {
	if getenv == nil {
		return domain.FederationPolicy{},
			errors.New("an environment reader is required to configure Multipass federation")
	}
	reserved := make([]string, 0, len(bootstrapAdministrators))
	for _, administrator := range bootstrapAdministrators {
		if administrator = strings.TrimSpace(administrator); administrator != "" {
			reserved = append(reserved, administrator)
		}
	}
	return domain.FederationPolicy{
		UsernamePrefix:    strings.TrimSpace(getenv(MultipassUsernamePrefixVariable)),
		GroupPrefix:       strings.TrimSpace(getenv(MultipassGroupPrefixVariable)),
		ReservedUsernames: reserved,
	}, nil
}
