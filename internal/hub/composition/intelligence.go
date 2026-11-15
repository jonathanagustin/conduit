package composition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	intelapp "github.com/jonathanagustin/conduit/internal/intelligence/application"
	intelhttp "github.com/jonathanagustin/conduit/internal/intelligence/httpapi"
	packagingcli "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type intelCases struct{ service *intelapp.Service }

func (cases intelCases) Add(scenario, bound, value, expected string) (string, error) {
	if cases.service == nil {
		return "", fmt.Errorf("eval cases are not configured")
	}
	added, err := cases.service.AddCase(scenario, bound, value, expected)
	if err != nil {
		return "", err
	}
	return added.ID, nil
}

func newIntelligenceService(repository controlplane.Repository, options controlplane.HandlerOptions) *intelapp.Service {
	service := intelapp.New(intelligenceAdapter(), intelligenceTimeout())
	service.Opener = intelligenceOpener{service: controlplane.NewGovernanceService(repository), repository: repository, options: options}
	service.Audit = intelligenceAudit{repository: repository}
	service.Proposer = liveEvalProposer()
	return service
}

func liveEvalProposer() intelapp.Proposer {
	raw := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return nil
	}
	argv, err := packagingcli.ParseArgv(raw)
	if err != nil || len(argv) == 0 {
		return nil
	}
	timeout := 45 * time.Second
	if parsed, err := time.ParseDuration(intelligenceTimeout()); err == nil && parsed > 0 {
		timeout = parsed
	}
	return intelligenceEvalProposer{runner: packagingcli.Runner{Argv: argv, Timeout: timeout}}
}

type intelligenceEvalProposer struct {
	runner packagingcli.Runner
}

func (proposer intelligenceEvalProposer) Propose(ctx context.Context, scenario, value string) (string, error) {
	proposal, _, err := proposer.runner.Propose(ctx, packagingdomain.PackagingContext{
		RunID: "eval-live", MemoryLimit: value, MemoryRequest: value,
		ChartValues: "memory: " + value, Resources: "limits.memory=" + value,
		GateRules: packagingdomain.MemoryGateRule,
	})
	if err != nil {
		return "", err
	}
	return proposal.Value, nil
}

func mountIntelligence(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options, service *intelapp.Service) http.Handler {
	governanceService := controlplane.NewGovernanceService(repository)
	if service == nil {
		service = newIntelligenceService(repository, options)
	}
	mux := http.NewServeMux()
	intelhttp.Register(mux, intelhttp.Config{
		API:    service,
		Events: intelligenceEvents{reader: hub.PackagingStatus, service: governanceService, options: options},
		Authorize: func(request *http.Request) error {
			_, err := operatorFrom(request.Context())
			return err
		},
		Caller: func(request *http.Request) string {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				return ""
			}
			return principal.Username
		},
	})
	mux.Handle("/", legacy)
	return withOperatorCarrier(governanceService, options, mux)
}

func intelligenceAdapter() string {
	raw := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return "recorded"
	}
	argv, err := modelcli.ParseArgv(raw)
	if err != nil || len(argv) == 0 {
		return "recorded"
	}
	return filepath.Base(argv[0])
}

func intelligenceTimeout() string {
	if text := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		return text
	}
	return "45s"
}

type intelligenceOpener struct {
	service    *controlplane.GovernanceService
	repository controlplane.Repository
	options    controlplane.HandlerOptions
}

// Open stages a scoped memory course for mandatory human review.
// CONDUIT-INVENTED: intelligence.ScopedMemoryCourse
// Displaced-by: a published or sanitized intelligence course proposal identity and scoped authorization contract.
func (opener intelligenceOpener) Open(ctx context.Context, value, rationale, author, environment, installation, product string) (string, string, error) {
	principal, err := operatorFrom(ctx)
	if err != nil || principal.Username != author {
		return "", "", intelapp.ErrCourseForbidden
	}
	rid := "installation/" + environment + "/" + installation
	if !opener.service.AuthorizeResourceOperation(ctx, principal, opener.options, rid, controlplane.OperationProposeChange) {
		return "", "", intelapp.ErrCourseForbidden
	}
	projection, err := controlplane.BuildFleetProjection(ctx, opener.repository, controlplane.FleetQuery{Environment: environment}, time.Now())
	if err != nil {
		return "", "", err
	}
	found := false
	for _, row := range projection.Installations {
		if row.Environment == environment && row.InstallationID == installation && row.Product == product && row.DesiredVersion != "" {
			found = true
			break
		}
	}
	if !found {
		return "", "", intelapp.ErrCourseTargetMissing
	}
	encoded, err := json.Marshal(struct {
		Author                                    controlplane.Principal
		Environment, Installation, Product, Value string
	}{principal, environment, installation, product, value})
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(encoded)
	targetID := hex.EncodeToString(digest[:16])
	request, err := opener.service.ProposeHumanChange(ctx, "CONDUIT_NATIVE_INTELLIGENCE_COURSE", targetID, environment, []controlplane.EntityChange{{
		ID: "memory-limit", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: installation,
		Request: map[string]any{"memory": value},
		Diff:    map[string]controlplane.ChangeDiffEntry{"limits.memory": {After: value}},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + environment, Name: environment},
			"installation": {RID: "installation/" + environment + "/" + installation, Name: installation},
			"product":      {RID: "product/" + product, Name: product},
		},
	}}, principal, "Proposed memory limit "+value, rationale)
	if err != nil {
		return "", "", err
	}
	return request.ID, request.Status, nil
}

type intelligenceAudit struct {
	repository controlplane.Repository
}

func (audit intelligenceAudit) Record(ctx context.Context, actor, action, resource, decision, reason string) (string, error) {
	verdict := governance.DecisionDeny
	if decision == "ALLOW" || decision == string(packagingdomain.OutcomePass) {
		verdict = governance.DecisionAllow
	}
	event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), actor, action, resource, verdict, reason, map[string]any{"source": "intelligence"})
	saved, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}

type intelligenceEvents struct {
	service *controlplane.GovernanceService
	options controlplane.HandlerOptions
	reader  interface {
		List(ctx context.Context) ([]packagingdomain.RunStatus, error)
	}
}

func (events intelligenceEvents) Observed(ctx context.Context) []map[string]string {
	if events.reader == nil || events.service == nil {
		return nil
	}
	principal, err := operatorFrom(ctx)
	if err != nil {
		return nil
	}
	runs, err := events.reader.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(runs))
	for _, run := range runs {
		environment := packagingdomain.EnvironmentOf(run.TargetPlanRef)
		if environment == "" || !events.service.AuthorizeResourceOperation(ctx, principal, events.options, "environment/"+environment, controlplane.OperationReadResource) {
			continue
		}
		out = append(out, map[string]string{"runId": run.RunID, "phase": run.Phase, "outcome": string(run.Outcome)})
	}
	return out
}
