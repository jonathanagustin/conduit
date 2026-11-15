package composition

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	devchange "github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/cluster"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/process"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	devhttp "github.com/jonathanagustin/conduit/internal/devworkspace/httpapi"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	ontologydomain "github.com/jonathanagustin/conduit/internal/ontology/domain"
	ontologyports "github.com/jonathanagustin/conduit/internal/ontology/ports"
)

func assembleDevWorkspace(
	repository controlplane.Repository, options controlplane.HandlerOptions, getenv func(string) string,
) *application.Service {
	store := recorded.New()
	service := controlplane.NewGovernanceService(repository)
	var runtime ports.Runtime
	switch getenv("DEVWORKSPACE_RUNTIME") {
	case "process":
		runtime = process.New(nil, nil)
	case "recorded":
		runtime = store
	}
	var pods ports.Provisioner
	podsOn := getenv("DEVWORKSPACE_PODS") == "1"
	if podsOn {
		pods = cluster.Fallback{Primary: cluster.New(getenv("DEVWORKSPACE_IMAGE")), Secondary: store.Pods()}
	}
	return application.New(application.Dependencies{
		Records: store, Files: store.Files(), Runtime: runtime, Pods: pods,
		Submitter: devchange.New(service), Auditor: devAudit{repository: repository},
		PodsEnabled: podsOn,
	})
}

type devAudit struct{ repository controlplane.Repository }

func (audit devAudit) Record(ctx context.Context, subject, action, resource, decision, reason string) error {
	event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), subject, action, resource, decision, reason, nil)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func mountDevWorkspace(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, spaces *application.Service,
) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	devhttp.Register(mux, devhttp.Config{
		Service: spaces,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return devhttp.StatusError{Status: statusForIdentityError(err), Message: "unauthorized"}
			}
			return nil
		},
		Caller: func(request *http.Request) (devdomain.Caller, error) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return devdomain.Caller{}, devhttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return devWorkspaceCaller(principal), nil
		},
		Visible: func(request *http.Request, environmentID string) bool {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || environmentID == "" {
				return false
			}
			return service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource,
			)
		},
		Permit: func(request *http.Request, environmentID string) error {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return devhttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			if strings.TrimSpace(environmentID) == "" {
				if !devStanding(request.Context(), service, options, principal) {
					return devhttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
				}
				return nil
			}
			if service.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationProposeChange) ||
				service.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationOperate) {
				return nil
			}
			return devhttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
		},
	})
	mux.Handle("/", legacy)
	return mux
}

func devWorkspaceCaller(principal controlplane.Principal) devdomain.Caller {
	caller := devdomain.Caller{Username: principal.Username, Teams: principal.Teams, Groups: principal.Groups, ServiceAccount: principal.ServiceAccount}
	if represented := principal.OnBehalfOf; represented != nil {
		caller.OnBehalfOf = &devdomain.Caller{Username: represented.Username, Teams: represented.Teams, Groups: represented.Groups, ServiceAccount: represented.ServiceAccount}
	}
	return caller
}

func devStanding(ctx context.Context, service *controlplane.GovernanceService, options controlplane.HandlerOptions, principal controlplane.Principal) bool {
	if service.AuthorizeResourceOperation(ctx, principal, options, "platform/root", controlplane.OperationOperate) ||
		service.AuthorizeResourceOperation(ctx, principal, options, "platform/root", controlplane.OperationProposeChange) {
		return true
	}
	return len(principal.Teams) > 0 || len(principal.Groups) > 0
}

type workspaceCatalog struct {
	inner  ontologyports.Source
	spaces *application.Service
}

func (catalog workspaceCatalog) Catalog(ctx context.Context, allow func(string) bool) (ontologydomain.Catalog, error) {
	loaded, err := catalog.inner.Catalog(ctx, allow)
	if err != nil || catalog.spaces == nil {
		return loaded, err
	}
	rows, err := catalog.spaces.Records(ctx)
	if err != nil {
		return ontologydomain.Catalog{}, err
	}
	for _, row := range rows {
		if allow != nil && !allow(row.EnvironmentID) {
			continue
		}
		loaded.Objects = append(loaded.Objects, ontologydomain.Object{
			Type: "Workspace", PrimaryKey: row.ID,
			Properties: map[string]string{
				"workspaceId": row.ID, "title": row.Title, "status": row.Status, "owner": row.Owner,
				"profileId": row.ProfileID, "environmentId": row.EnvironmentID,
				"sourceDigest": row.SourceDigest, "proposalRevision": row.ProposalRevision,
				"parentId": row.ParentID, "valueTier": devdomain.ValueTier,
			},
		})
		if row.EnvironmentID != "" {
			loaded.Links = append(loaded.Links, ontologydomain.Link{
				APIName: "workspaces", FromType: "Environment", FromKey: row.EnvironmentID,
				ToType: "Workspace", ToKey: row.ID,
			})
		}
	}
	return loaded, nil
}

func (catalog workspaceCatalog) Apply(ctx context.Context, caller ontologyports.Caller, request ontologydomain.ApplyRequest) (ontologydomain.ApplyResult, error) {
	return catalog.inner.Apply(ctx, caller, request)
}
