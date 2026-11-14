package composition

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
)

// mountPlanning exposes the planning-session contract. When
// CONDUIT_MODEL_CMD is set, turns inspect the intake repository through
// inspect_repo and read_file. Compute is refused until Accept.
func mountPlanning(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, error) {
	runner, err := planningPlanner(os.Getenv, repository)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	planninghttp.Register(mux, planninghttp.Config{
		Service: planningapp.NewService(runner),
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
	})
	mux.Handle("/", legacy)
	return mux, nil
}

func planningPlanner(getenv func(string) string, repository controlplane.Repository) (planningapp.Planner, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return nil, nil
	}
	argv, err := localcmd.ParseArgv(raw)
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
	return planner.Planner{Argv: argv, Timeout: timeout, Audit: planningAudit{repository: repository}}, nil
}

type planningAudit struct {
	repository controlplane.Repository
}

func (audit planningAudit) Record(ctx context.Context, record planner.Record) error {
	if audit.repository == nil {
		return nil
	}
	decision := governance.DecisionDeny
	if record.Outcome == "pass" {
		decision = governance.DecisionAllow
	}
	action := record.Action
	if action == "" {
		action = planner.ActionInspect
	}
	resource := "planning-session/" + record.SessionID
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), "planning-"+strings.TrimPrefix(action, "planning."), action, resource, decision, record.Reason,
		map[string]any{
			"adapter": record.Adapter, "promptSha256": record.PromptSHA256,
			"responseSha256": record.ResponseSHA256, "toolCalls": record.ToolCalls,
			"repoUrl": record.RepoURL, "resolvedCommit": record.ResolvedCommit,
		},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}
