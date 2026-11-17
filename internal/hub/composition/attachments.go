package composition

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	appdomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/attachments/adapters/recorded"
	attachapp "github.com/jonathanagustin/conduit/internal/attachments/application"
	attachhttp "github.com/jonathanagustin/conduit/internal/attachments/httpapi"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// newAttachmentService is the one store that the intake routes, app generation,
// and packaging proposals all read, so a digest means the same bytes everywhere.
func newAttachmentService(repository controlplane.Repository) *attachapp.Service {
	return &attachapp.Service{Store: recorded.New(), Audit: attachmentAudit{repository: repository}}
}

func mountAttachments(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, service *attachapp.Service,
) http.Handler {
	governanceService := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	attachhttp.Register(mux, attachhttp.Config{
		Service: service,
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Operator: func(request *http.Request) (string, error) {
			principal, ok := packagingPrincipal(request, governanceService, options)
			if !ok || strings.TrimSpace(principal.Username) == "" {
				return "", errors.New("unauthorized")
			}
			return principal.Username, nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}

// appAttachments binds stored files into a generation request as read-only context.
type appAttachments struct{ stored *attachapp.Service }

func (bound appAttachments) Resolve(ctx context.Context, actor string, digests []string) ([]appdomain.Attachment, error) {
	items, err := bound.stored.Resolve(ctx, actor, digests)
	if err != nil {
		return nil, err
	}
	out := make([]appdomain.Attachment, 0, len(items))
	for _, item := range items {
		out = append(out, appdomain.Attachment{Name: item.Name, SHA256: item.SHA256, Content: item.Content})
	}
	return out, nil
}

// packagingAttachments binds stored files into a packaging proposal as read-only context.
type packagingAttachments struct{ stored *attachapp.Service }

func (bound packagingAttachments) Resolve(ctx context.Context, actor string, digests []string) ([]packagingdomain.Attachment, error) {
	items, err := bound.stored.Resolve(ctx, actor, digests)
	if err != nil {
		return nil, err
	}
	out := make([]packagingdomain.Attachment, 0, len(items))
	for _, item := range items {
		out = append(out, packagingdomain.Attachment{Name: item.Name, SHA256: item.SHA256, Content: item.Content})
	}
	return out, nil
}

func modelWithAttachments(service packagingapp.Service, stored *attachapp.Service) packagingapp.Service {
	if stored != nil {
		service.Attachments = packagingAttachments{stored: stored}
	}
	return service
}

type attachmentAudit struct {
	repository controlplane.Repository
}

func (audit attachmentAudit) Record(ctx context.Context, event attachapp.Audit) error {
	decision := governance.DecisionAllow
	if event.Outcome != attachapp.OutcomeStored {
		decision = governance.DecisionDeny
	}
	subject := "intake-attachment"
	if name := strings.TrimSpace(event.Actor); name != "" && event.Outcome == attachapp.OutcomeDenied {
		subject = name
	}
	record := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), subject, event.Action, "intake-attachment/"+event.SHA256, decision, event.Reason,
		map[string]any{"name": event.Name, "kind": event.Kind, "size": event.Size, "sha256": event.SHA256},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, record)
	return err
}
