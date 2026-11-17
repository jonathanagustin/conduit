package composition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
	governorhttp "github.com/jonathanagustin/conduit/internal/modelgovernor/httpapi"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

// ModelGovernance is the durable half of model-call governance. Both stores
// are nil with the file repository, which keeps usage and transcripts in this
// process: a budget is then per replica and nothing survives a restart.
type ModelGovernance struct {
	Usage       modelgovernor.UsageStore
	Transcripts modelgovernor.TranscriptStore
	// Governor, when set, is the governor NewHub uses instead of building one.
	// A process that also runs a planning worker builds one with
	// NewModelGovernor and gives it to both, so the worker's model calls are
	// bounded, metered and audited by the same governor as a request's.
	Governor *modelgovernor.Governor
}

// NewModelGovernor builds the governor of a Hub process ahead of NewHub and
// returns it with the stores it uses. ctx stops the transcript sweeper.
func NewModelGovernor(
	ctx context.Context, repository controlplane.Repository, stores ModelGovernance, getenv func(string) string,
) (ModelGovernance, error) {
	governor, stores, err := newModelGovernor(repository, Options{ModelGovernance: stores, LiveEventContext: ctx}, getenv)
	if err != nil {
		return ModelGovernance{}, err
	}
	stores.Governor = governor
	return stores, nil
}

// buildingGovernor is the governor of the Hub being assembled. NewHub sets it
// for the length of its own run, under buildMu, and every model transport
// built in that window keeps it: a model call whose context carries no
// governor -- one made off a request -- is then governed and audited by the
// Hub that built its transport. A transport built outside NewHub keeps nil
// and such a call is not governed, which is what a unit test of one adapter
// wants.
var (
	buildMu          sync.Mutex
	buildingGovernor atomic.Pointer[modelgovernor.Governor]
)

// boundGovernor captures the governor in force now, not when it is called.
func boundGovernor() modelgovernor.Resolver {
	governor := buildingGovernor.Load()
	return func() *modelgovernor.Governor { return governor }
}

// assemblingWith marks governor as the one transports bind to until the
// returned function is called.
func assemblingWith(governor *modelgovernor.Governor) func() {
	buildMu.Lock()
	buildingGovernor.Store(governor)
	return func() {
		buildingGovernor.Store(nil)
		buildMu.Unlock()
	}
}

// governedModelHTTPClient is modelHTTPClient with every completion passed
// through the governor as one capability's call.
func governedModelHTTPClient(getenv func(string) string, capability modelgovernor.Capability) (*modelhttp.Client, error) {
	client, err := modelHTTPClient(getenv)
	if err != nil || client == nil {
		return client, err
	}
	return client.With(modelgovernor.HTTPMiddleware(capability, boundGovernor())), nil
}

// governedExec runs one capability's model command through the governor.
func governedExec(getenv func(string) string, capability modelgovernor.Capability) localcmd.ExecFunc {
	return modelgovernor.CLIExec(capability, configuredModelName(getenv), boundGovernor())
}

// packagingModelHTTPClient and packagingExec are the two packaging transports.
func packagingModelHTTPClient(getenv func(string) string) (*modelhttp.Client, error) {
	return governedModelHTTPClient(getenv, modelgovernor.CapabilityPackaging)
}

func packagingExec(getenv func(string) string) localcmd.ExecFunc {
	return governedExec(getenv, modelgovernor.CapabilityPackaging)
}

// newModelGovernor reads the configuration and builds this Hub's governor.
// A value that does not parse stops startup.
func newModelGovernor(repository controlplane.Repository, hub Options, getenv func(string) string) (*modelgovernor.Governor, ModelGovernance, error) {
	if hub.ModelGovernance.Governor != nil {
		return hub.ModelGovernance.Governor, hub.ModelGovernance, nil
	}
	config, err := modelgovernor.FromEnv(getenv)
	if err != nil {
		return nil, ModelGovernance{}, err
	}
	// Every model call the Hub makes is audited or it is not made.
	config.AuditRequired = true
	stores := hub.ModelGovernance
	if stores.Usage == nil {
		stores.Usage = memory.NewUsage()
	}
	if stores.Transcripts == nil {
		stores.Transcripts = memory.NewTranscripts()
	}
	governor := modelgovernor.New(config, modelgovernor.Dependencies{
		Usage: stores.Usage, Transcripts: stores.Transcripts, Audit: modelCallAudit{repository: repository},
		// The same filter repository content passes through on its way into
		// a prompt.
		Redact: func(data []byte) []byte { return []byte(packagingtools.RedactSecrets(string(data))) },
	})
	if config.Transcripts {
		ctx := hub.LiveEventContext
		if ctx == nil {
			ctx = context.Background()
		}
		go modelgovernor.Sweeper{
			Store: stores.Transcripts, Interval: sweepInterval(config.TranscriptRetention),
			OnError: func(err error) { slog.Error("sweep expired AI transcripts", "error", err) },
		}.Run(ctx)
	}
	return governor, stores, nil
}

// sweepInterval is a tenth of the retention, between a minute and an hour, so
// a record outlives its retention by at most that much on disk. A read never
// returns an expired record whether or not it has been swept.
func sweepInterval(retention time.Duration) time.Duration {
	return min(max(retention/10, time.Minute), time.Hour)
}

// modelCallAudit writes a model call to the same audit log every other AI
// record goes to.
type modelCallAudit struct {
	repository controlplane.Repository
}

func (audit modelCallAudit) RecordModelCall(ctx context.Context, call modelgovernor.CallAudit) error {
	if audit.repository == nil {
		return fmt.Errorf("no audit repository")
	}
	decision := governance.DecisionDeny
	if call.Outcome == string(modelgovernor.OutcomeOK) {
		decision = governance.DecisionAllow
	}
	attributes := map[string]any{
		"capability": call.Capability, "transport": call.Transport, "runId": call.RunID, "round": call.Round,
		"promptSha256": call.PromptSHA256, "responseSha256": call.ResponseSHA256,
		"previousChainSha256": call.PreviousChainSHA256, "chainSha256": call.ChainSHA256,
		// Both ids come from configuration and provider metadata. An empty
		// value means that source had none; it is recorded, not guessed.
		"configuredModel": call.ConfiguredModel, "providerModel": call.ProviderModel,
		"outcome": call.Outcome, "attempts": call.Attempts, "transcript": call.Transcript,
	}
	if call.SessionID != "" {
		attributes["sessionId"] = call.SessionID
	}
	if call.StoredPromptSHA256 != "" {
		attributes["storedPromptSha256"] = call.StoredPromptSHA256
	}
	if call.StoredResponseSHA256 != "" {
		attributes["storedResponseSha256"] = call.StoredResponseSHA256
	}
	event := governance.NewAuthorizationCheckEvent(
		call.At, call.Owner, modelgovernor.ActionModelCall, modelgovernor.ResourcePrefix+call.RunID, decision,
		"model call "+call.Outcome, attributes,
	)
	// A clock can report the same instant for distinct exchanges. The native
	// run/round coordinate distinguishes them and remains stable when an
	// acknowledgement is lost and the exact event is retried.
	identity := sha256.Sum256([]byte(modelgovernor.ActionModelCall + "\x00" + call.RunID + "\x00" + strconv.Itoa(call.Round)))
	event.ID = "aud_" + hex.EncodeToString(identity[:16])
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

// Events reads one run's model-call events back out of the audit log.
func (audit modelCallAudit) Events(ctx context.Context, runID string) ([]modelgovernor.CallAudit, error) {
	events, err := audit.repository.ListAuthorizationAudit(ctx, modelgovernor.ResourcePrefix+runID)
	if err != nil {
		return nil, err
	}
	out := []modelgovernor.CallAudit{}
	for _, event := range events {
		if event.Action != modelgovernor.ActionModelCall {
			continue
		}
		text := func(key string) string { value, _ := event.Context[key].(string); return value }
		number := func(key string) int {
			switch value := event.Context[key].(type) {
			case int:
				return value
			case int64:
				return int(value)
			case float64:
				return int(value)
			}
			return 0
		}
		out = append(out, modelgovernor.CallAudit{
			EventID: event.ID, Owner: event.Subject, At: event.CreatedAt,
			Capability: text("capability"), Transport: text("transport"), RunID: text("runId"), SessionID: text("sessionId"),
			Round: number("round"), PromptSHA256: text("promptSha256"), ResponseSHA256: text("responseSha256"),
			PreviousChainSHA256: text("previousChainSha256"), ChainSHA256: text("chainSha256"),
			ConfiguredModel: text("configuredModel"), ProviderModel: text("providerModel"),
			Outcome: text("outcome"), Attempts: number("attempts"), Transcript: text("transcript"),
			StoredPromptSHA256: text("storedPromptSha256"), StoredResponseSHA256: text("storedResponseSha256"),
		})
	}
	return out, nil
}

// recordExport audits one transcript export, allowed or refused.
func (audit modelCallAudit) recordExport(ctx context.Context, actor, runID string, allowed bool, reason string, export *modelgovernor.Export) error {
	decision := governance.DecisionDeny
	if allowed {
		decision = governance.DecisionAllow
	}
	attributes := map[string]any{"runId": runID}
	if export != nil {
		present := 0
		for _, exchange := range export.Exchanges {
			if exchange.Present {
				present++
			}
		}
		attributes["rounds"], attributes["roundsWithText"] = len(export.Exchanges), present
		attributes["verified"], attributes["complete"] = export.Verified, export.Complete
		attributes["transcriptOwner"] = export.Owner
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), actor, modelgovernor.ActionTranscriptExport, modelgovernor.ResourcePrefix+runID, decision, reason, attributes,
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

// mountModelGovernance serves usage and transcript export.
func mountModelGovernance(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions,
	governor *modelgovernor.Governor, stores ModelGovernance,
) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	// Transcripts can hold tenant source code. The authority is the one the
	// administrator status page uses: the governance Administrator
	// predicate. No team administrator and no resource grant reaches it.
	decision := governanceapp.ResourceDecision{
		Authorizer: governanceapp.NewAuthorizer(adminGrantSource{repository}),
		Policy: governanceapp.AdministratorPolicy{
			BootstrapAdministrators: options.BootstrapAdministrators, AllowLocalDeveloperFallback: options.AllowInsecureOperatorIdentityHeaders,
		},
	}
	audit := modelCallAudit{repository: repository}
	mux := http.NewServeMux()
	governorhttp.Register(mux, governorhttp.Config{
		Governor: governor, Transcripts: stores.Transcripts, AuditEvents: audit.Events, RecordExport: audit.recordExport,
		Caller: func(request *http.Request) (string, bool) {
			principal, ok := packagingPrincipal(request, service, options)
			return principal.Username, ok
		},
		Administrator: func(request *http.Request) (string, int) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || strings.TrimSpace(principal.Username) == "" {
				return "", http.StatusUnauthorized
			}
			if !decision.Administrator(request.Context(), principal) {
				return principal.Username, http.StatusForbidden
			}
			return principal.Username, 0
		},
	})
	mux.Handle("/", legacy)
	return mux
}

// withModelGovernance puts this Hub's governor, the caller and a new run on
// every request, where a model transport finds them. The operator is resolved
// only if a model call happens. One request is one run: its model calls are
// numbered and chained together.
func withModelGovernance(
	next http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, governor *modelgovernor.Governor,
) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	next = governorhttp.RefusalAsTooManyRequests(next)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		owner := sync.OnceValue(func() string {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return ""
			}
			return principal.Username
		})
		ctx := modelgovernor.WithScope(request.Context(), modelgovernor.Scope{Governor: governor, Owner: owner})
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// ModelCallMetrics is this process's model-call counters, for the metrics
// listener.
func ModelCallMetrics() []modelgovernor.MetricSample { return modelgovernor.ProcessMetrics.Snapshot() }
