package main

import (
	"log/slog"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/composition"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// gitlessObserver reports what each reconcile pass did.
//
// Refusals log at warn and failures at error, which is the right way round even
// though it looks backwards. A refusal means the machinery worked: something was
// published that this environment does not trust, and the deployment correctly
// did not happen. A failure means Conduit could not find out, which is the state
// that needs somebody. Logging both at error would bury the second in the first.
type gitlessObserver struct{}

func (gitlessObserver) Deployed(environment string, outcome application.Outcome) {
	pin := outcome.Admission.Pin()
	_, publisher := outcome.Admission.Publisher()
	slog.Info("Gitless GitOps deployed",
		"environment", environment, "repository", pin.Repository, "digest", pin.Digest,
		"publisher", publisher, "superseded", outcome.Superseded, "moved", outcome.Moved)
}

func (gitlessObserver) Unchanged(environment string, outcome application.Outcome) {
	// Debug, not info. This is the common case on a short interval, and an
	// operator reading the log wants the passes that changed something.
	slog.Debug("Gitless GitOps unchanged",
		"environment", environment, "digest", outcome.Admission.Pin().Digest)
}

func (gitlessObserver) Refused(environment string, refusals []domain.Refusal) {
	reasons := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		reasons = append(reasons, string(refusal.Reason))
	}
	slog.Warn("Gitless GitOps refused a source",
		"environment", environment, "reasons", strings.Join(reasons, ","), "detail", refusals[0].Detail)
}

func (gitlessObserver) Failed(environment string, err error) {
	slog.Error("Gitless GitOps could not reconcile", "environment", environment, "error", err)
}

// configureGitless reads the Hub's Gitless GitOps configuration.
//
// It returns a nil scheduler when nothing is configured, which is the normal
// state: this is off unless somebody turned it on.
//
// Convergence deliberately has no default, and the Conduit-owned path is not
// wired here yet. Its ports need an artifact fetcher and a desired-state store
// that reach into Delivery, and offering the option before it can be honoured
// would be advertising a capability that does not exist -- exactly what
// domain.Reconcile refuses to let the GraphQL interface do.
func configureGitless(getenv func(string) string) (*application.Scheduler, error) {
	interval := 5 * time.Minute
	if raw := strings.TrimSpace(getenv("GITLESS_INTERVAL")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, err
		}
		interval = parsed
	}
	return composition.Configure(composition.Config{
		Bindings:          splitCommaSeparated(getenv("GITLESS_BINDINGS")),
		PolicyFile:        strings.TrimSpace(getenv("GITLESS_PUBLISHER_POLICY_FILE")),
		ResolutionLogFile: strings.TrimSpace(getenv("GITLESS_RESOLUTION_LOG_FILE")),
		Interval:          interval,
		CosignBinary:      strings.TrimSpace(getenv("GITLESS_COSIGN_BINARY")),
		Convergence:       strings.TrimSpace(getenv("GITLESS_CONVERGENCE")),
		Flux: composition.FluxConfig{
			Namespace:          strings.TrimSpace(getenv("GITLESS_FLUX_NAMESPACE")),
			ServiceAccountName: strings.TrimSpace(getenv("GITLESS_FLUX_SERVICE_ACCOUNT")),
			PullSecretName:     strings.TrimSpace(getenv("GITLESS_FLUX_PULL_SECRET")),
			Path:               strings.TrimSpace(getenv("GITLESS_FLUX_PATH")),
			// Published as an OCI artifact, one tag per environment, which is
			// what a cluster's Flux pulls. Without this the Flux path could not
			// start at all: the store was described as supplied by the
			// deployment and no deployment supplied one.
			ManifestRepository: strings.TrimSpace(getenv("GITLESS_FLUX_MANIFEST_REPOSITORY")),
			InsecureRegistry:   getenv("GITLESS_FLUX_INSECURE_REGISTRY") == "true",
		},
	}, composition.ConduitConfig{}, gitlessObserver{})
}
