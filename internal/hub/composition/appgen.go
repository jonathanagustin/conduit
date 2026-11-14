package composition

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/registry"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sandbox"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sourcefile"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	appdomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	apphttp "github.com/jonathanagustin/conduit/internal/appgen/httpapi"
	attachapp "github.com/jonathanagustin/conduit/internal/attachments/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
)

func appGenerator(getenv func(string) string) (appdomain.AppGenerator, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return recorded.Adapter{}, nil
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
	return modelcli.Runner{Argv: argv, Timeout: timeout}, nil
}

func mountAppGeneration(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, generator appdomain.AppGenerator,
	stored *attachapp.Service,
) http.Handler {
	registryAddress := strings.TrimSpace(os.Getenv("CONDUIT_LOCAL_REGISTRY"))
	if registryAddress == "" {
		registryAddress = "127.0.0.1:5000"
	}
	service := application.Service{
		Generator: generator,
		Sandbox:   sandbox.Docker{},
		Publish:   registry.DockerPublisher{Registry: registryAddress},
		Handoff:   sourcefile.Writer{Dir: strings.TrimSpace(os.Getenv("PACKAGING_FIXTURE_DIR"))},
		Audit:     appAudit{repository: repository},
		Store:     &application.Memory{},
	}
	if stored != nil {
		service.Attachments = appAttachments{stored: stored}
	}
	mux := http.NewServeMux()
	apphttp.Register(mux, apphttp.Config{
		Service: service,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return err
			}
			return nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}

type appAudit struct {
	repository controlplane.Repository
}

func (audit appAudit) Record(ctx context.Context, event application.Audit) error {
	decision := governance.DecisionDeny
	if event.Outcome == appdomain.OutcomePass {
		decision = governance.DecisionAllow
	}
	record := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), "app-generation", "app.generate", "app-generation/"+event.ID, decision, event.Reason,
		map[string]any{
			"adapter": event.Adapter, "promptSha256": event.PromptSHA256, "responseSha256": event.ResponseSHA256,
			"files": event.Files, "buildPassed": event.BuildPassed, "buildReason": event.BuildReason,
			"healthPassed": event.HealthPassed, "healthReason": event.HealthReason,
			"attachmentSha256": event.AttachmentSHA256,
		},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, record)
	return err
}
