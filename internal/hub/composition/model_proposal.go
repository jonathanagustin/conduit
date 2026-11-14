package composition

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/runfacts"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func modelProposer(getenv func(string) string) (packagingdomain.ChangeProposer, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return recorded.Unavailable(), nil
	}
	argv, err := modelcli.ParseArgv(raw)
	if err != nil {
		return nil, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
	}
	timeout := 45 * time.Second
	if text := strings.TrimSpace(getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("CONDUIT_MODEL_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	return modelcli.Runner{Argv: argv, Timeout: timeout}, nil
}

func modelService(
	repository controlplane.Repository, governanceService *controlplane.GovernanceService, proposer packagingdomain.ChangeProposer,
) application.Service {
	service := application.Service{
		Proposer: proposer,
		Open:     humanOpener{service: governanceService},
		Audit:    modelAudit{repository: repository},
	}
	if dir := strings.TrimSpace(os.Getenv("PACKAGING_FIXTURE_DIR")); dir != "" {
		service.Facts = runfacts.Reader{Dir: dir}
	}
	return service
}

type humanOpener struct {
	service *controlplane.GovernanceService
}

func (opener humanOpener) OpenPending(ctx context.Context, change application.PendingChange) (string, error) {
	request, err := opener.service.ProposeHumanChange(ctx, "PLAN", change.TargetID, change.Environment, []controlplane.EntityChange{{
		ID: "memory-limit", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: "sample-app",
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + change.Environment, Name: change.Environment},
			"installation": {RID: "installation/" + change.Environment + "/sample-app", Name: "sample-app"},
			"product":      {RID: "product/app.example:sample-app", Name: "sample-app"},
		},
	}}, controlplane.Principal{Username: change.Author}, change.Title, change.Description)
	if err != nil {
		return "", err
	}
	if request.Status != controlplane.ChangePendingApprovals {
		return "", fmt.Errorf("change request %s is %s", request.ID, request.Status)
	}
	return request.ID, nil
}

type modelAudit struct {
	repository controlplane.Repository
}

func (audit modelAudit) Record(ctx context.Context, runID string, provenance packagingdomain.Provenance, outcome, reason string) error {
	decision := governance.DecisionDeny
	if outcome == string(packagingdomain.OutcomePass) {
		decision = governance.DecisionAllow
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), "model-proposal", "packaging.model-proposal", "packaging-run/"+runID, decision, reason,
		map[string]any{
			"adapter": provenance.Adapter, "promptSha256": provenance.PromptSHA256,
			"responseSha256": provenance.ResponseSHA256, "gateResult": outcome,
		},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}
