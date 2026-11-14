package composition

import (
	"context"
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

func newIntelligenceService(repository controlplane.Repository) *intelapp.Service {
	service := intelapp.New(intelligenceAdapter(), intelligenceTimeout())
	service.Opener = intelligenceOpener{service: controlplane.NewGovernanceService(repository)}
	service.Audit = intelligenceAudit{repository: repository}
	return service
}

func mountIntelligence(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options, service *intelapp.Service) http.Handler {
	governanceService := controlplane.NewGovernanceService(repository)
	if service == nil {
		service = newIntelligenceService(repository)
	}
	mux := http.NewServeMux()
	intelhttp.Register(mux, intelhttp.Config{
		API:    service,
		Events: intelligenceEvents{reader: hub.PackagingStatus},
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return err
			}
			return nil
		},
		Caller: func(request *http.Request) string {
			principal, ok := packagingPrincipal(request, governanceService, options)
			if !ok {
				return "operator"
			}
			return principal.Username
		},
	})
	mux.Handle("/", legacy)
	return mux
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
	service *controlplane.GovernanceService
}

func (opener intelligenceOpener) Open(ctx context.Context, value, rationale, author, environment, installation, product string) (string, string, error) {
	request, err := opener.service.ProposeHumanChange(ctx, "PLAN", "memory:"+value, environment, []controlplane.EntityChange{{
		ID: "memory-limit", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: installation,
		Request: map[string]any{"memory": value},
		Diff:    map[string]controlplane.ChangeDiffEntry{"limits.memory": {After: value}},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + environment, Name: environment},
			"installation": {RID: "installation/" + environment + "/" + installation, Name: installation},
			"product":      {RID: "product/" + product, Name: product},
		},
	}}, controlplane.Principal{Username: author}, "Proposed memory limit "+value, rationale)
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
	reader interface {
		List(ctx context.Context) ([]packagingdomain.RunStatus, error)
	}
}

func (events intelligenceEvents) Observed(ctx context.Context) []map[string]string {
	if events.reader == nil {
		return nil
	}
	runs, err := events.reader.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(runs))
	for _, run := range runs {
		out = append(out, map[string]string{"runId": run.RunID, "phase": run.Phase, "outcome": string(run.Outcome)})
	}
	return out
}
