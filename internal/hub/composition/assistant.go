package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/modelconfig"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	assistantmodelhttp "github.com/jonathanagustin/conduit/internal/assistant/adapters/modelhttp"
	assistantdocs "github.com/jonathanagustin/conduit/internal/assistant/adapters/productdocs"
	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadmemory"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	assistantdomain "github.com/jonathanagustin/conduit/internal/assistant/domain"
	assistanthttp "github.com/jonathanagustin/conduit/internal/assistant/httpapi"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	ontologyrepo "github.com/jonathanagustin/conduit/internal/ontology/adapters/repository"
	ontologydomain "github.com/jonathanagustin/conduit/internal/ontology/domain"
)

func mountAssistant(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options, model assistantdomain.Model, cases evalAdder,
) http.Handler {
	governanceService := newGovernanceService(repository, options)
	threads := hub.AssistantThreads
	if threads == nil {
		// No durable store was handed in. Threads still work, in this process
		// only: they are lost on restart and not shared between replicas.
		threads = threadmemory.New()
	}
	api := assistantAPI{
		repository: repository, governance: governanceService, options: options, hub: hub, model: model, cases: cases,
		threads:       threads,
		documentation: assistantdocs.New(),
	}
	mux := http.NewServeMux()
	assistanthttp.Register(mux, assistanthttp.Config{
		Tools: api,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return err
			}
			return nil
		},
		Caller: func(request *http.Request) (application.Actor, error) {
			principal, ok := packagingPrincipal(request, governanceService, options)
			if !ok || strings.TrimSpace(principal.Username) == "" {
				return application.Actor{}, fmt.Errorf("unauthorized")
			}
			return application.Actor{Username: principal.Username, Teams: principal.Teams, Groups: principal.Groups}, nil
		},
	})
	mountMCP(mux, api, governanceService, options)
	mux.Handle("/", legacy)
	return mux
}

func assistantModel(getenv func(string) string) (assistantdomain.Model, error) {
	getenv = modelconfig.ForCapability(getenv, modelconfig.Assistant)
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if client, err := governedModelHTTPClient(getenv, modelgovernor.CapabilityAssistant); err != nil {
		return nil, err
	} else if client != nil {
		return assistantmodelhttp.Adapter{Client: client}, nil
	}
	return nil, nil
}

type assistantAPI struct {
	documentation application.Documentation
	repository    controlplane.Repository
	governance    *controlplane.GovernanceService
	options       controlplane.HandlerOptions
	hub           Options
	model         assistantdomain.Model
	cases         evalAdder
	threads       application.Threads
}

type evalAdder interface {
	Add(scenario, bound, value, expected string) (string, error)
}

func (api assistantAPI) service(actor application.Actor) application.Service {
	return application.Service{
		Documentation: api.documentation,
		Objects:       catalogQuery{governance: api.governance, options: api.options, repository: api.repository},
		Governor:      actionGate{repository: api.repository, hub: api.hub},
		Changes:       changeOpener{service: api.governance, actions: assistantActions{repository: api.repository, options: api.options, hub: api.hub}},
		Diffs:         changeReader{repository: api.repository},
		Audit:         toolAudit{repository: api.repository},
		Model:         api.model,
		Cases:         api.cases,
		Authors:       requestAuthor{repository: api.repository},
		Reviews:       changeReviewer{service: api.governance},
		Receipts:      assistantReceiptReader{repository: api.repository},
		Threads:       api.threads,
	}
}

func (api assistantAPI) Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error) {
	return api.service(actor).Invoke(ctx, actor, mode, name, arguments)
}

func (api assistantAPI) Converse(ctx context.Context, actor application.Actor, request application.TurnRequest, observer application.Observer) (application.Turn, error) {
	if request.ThreadID != "" {
		// Every model call of the turn is filed under its thread.
		modelgovernor.Annotate(ctx, "assistant-thread/"+request.ThreadID)
	}
	return api.service(actor).Converse(ctx, actor, request, observer)
}

func (api assistantAPI) CreateThread(ctx context.Context, actor application.Actor, title string) (assistantdomain.Thread, error) {
	return api.service(actor).CreateThread(ctx, actor, title)
}

func (api assistantAPI) ListThreads(ctx context.Context, actor application.Actor) ([]assistantdomain.Thread, error) {
	return api.service(actor).ListThreads(ctx, actor)
}

func (api assistantAPI) GetThread(ctx context.Context, actor application.Actor, id string) (assistantdomain.Thread, error) {
	return api.service(actor).GetThread(ctx, actor, id)
}

func (api assistantAPI) DeleteThread(ctx context.Context, actor application.Actor, id string) error {
	return api.service(actor).DeleteThread(ctx, actor, id)
}

type catalogQuery struct {
	repository controlplane.Repository
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
}

func (query catalogQuery) Query(ctx context.Context, actor application.Actor, args assistantdomain.QueryArgs) (application.ObjectPage, error) {
	source := ontologyrepo.New(query.repository, query.governance)
	principal := controlplane.Principal{Username: actor.Username, Teams: actor.Teams, Groups: actor.Groups}
	catalog, err := source.Catalog(ctx, func(environmentID string) bool {
		if environmentID == "" {
			return false
		}
		return query.governance.AuthorizeResourceOperation(
			ctx, principal, query.options, "environment/"+environmentID, controlplane.OperationReadResource,
		)
	})
	if err != nil {
		return application.ObjectPage{}, err
	}
	set := nestedSet(args)
	readOps := 1 + len(args.Links)
	objects, err := ontologydomain.Resolve(catalog, set)
	if err != nil {
		return application.ObjectPage{}, err
	}
	total := len(objects)
	if len(objects) > 20 {
		objects = objects[:20]
	}
	hits := make([]application.ObjectHit, 0, len(objects))
	for _, object := range objects {
		status := object.Properties["status"]
		hits = append(hits, application.ObjectHit{
			ID: object.PrimaryKey, Type: object.Type, Label: object.Title(), Status: status,
		})
	}
	return application.ObjectPage{Hits: hits, ReadOps: readOps, Total: total}, nil
}

func nestedSet(args assistantdomain.QueryArgs) ontologydomain.ObjectSet {
	set := ontologydomain.ObjectSet{Type: "base", ObjectType: args.ObjectType}
	if args.Filter != nil {
		inner := set
		set = ontologydomain.ObjectSet{
			Type: "filter", Inner: &inner,
			Where: &ontologydomain.Where{Type: "eq", Field: args.Filter.Field, Value: args.Filter.Value},
		}
	}
	for _, link := range args.Links {
		inner := set
		set = ontologydomain.ObjectSet{Type: "searchAround", Inner: &inner, Link: link}
	}
	return set
}

type actionGate struct {
	repository controlplane.Repository
	hub        Options
}

func (gate actionGate) Evaluate(ctx context.Context, actor, action, resource string, roles []string) (application.Decision, error) {
	facts, err := actionFacts(ctx, gate.hub, nil, action, resource)
	if err != nil {
		return application.Decision{}, err
	}
	if action == "approve_change_request" || action == "reject_change_request" {
		request, err := gate.repository.GetChangeRequest(ctx, strings.TrimPrefix(resource, "change-request/"))
		if err == nil {
			facts.ChangeStatus = request.Status
		}
	}
	if strings.HasPrefix(resource, "packaging-run/") && gate.hub.PackagingStatus != nil {
		if run, err := gate.hub.PackagingStatus.Read(ctx, strings.TrimPrefix(resource, "packaging-run/")); err == nil {
			facts = assistantPackagingFacts(run)
		}
	}
	facts.Roles = append([]string{}, roles...)
	decision := ontologydomain.Decide(action, facts)
	verdict := governance.DecisionDeny
	if decision.Allowed {
		verdict = governance.DecisionAllow
	}
	eventName := decision.AuditEvent
	if eventName == "" {
		eventName = "assistant.unknown_action"
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), actor, eventName, resource, verdict, decision.Reason, map[string]any{"source": "assistant"},
	)
	saved, err := gate.repository.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return application.Decision{}, err
	}
	return application.Decision{
		Known: decision.Known, Allowed: decision.Allowed, Reason: decision.Reason,
		AuditEvent: eventName, AuditEventID: saved.ID,
	}, nil
}

type requestAuthor struct {
	repository controlplane.Repository
}

func (author requestAuthor) Author(ctx context.Context, id string) (string, error) {
	request, err := author.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return "", err
	}
	return request.Author.Username, nil
}

type changeReviewer struct {
	service *controlplane.GovernanceService
}

func (reviewer changeReviewer) Approve(ctx context.Context, id, actor, rationale string, roles []string) (string, error) {
	principal, err := operatorFrom(ctx)
	if err != nil || principal.Username != actor {
		return "", controlplane.ErrForbidden
	}
	request, _, err := reviewer.service.Approve(ctx, id, principal, rationale)
	if err != nil {
		if request.Status == controlplane.ChangeApproved && request.Execution != nil {
			return request.Status, nil
		}
		return "", err
	}
	return request.Status, nil
}

func (reviewer changeReviewer) Reject(ctx context.Context, id, actor, rationale string, roles []string) (string, error) {
	principal, err := operatorFrom(ctx)
	if err != nil || principal.Username != actor {
		return "", controlplane.ErrForbidden
	}
	request, _, err := reviewer.service.Reject(ctx, id, principal, rationale)
	if err != nil {
		return "", err
	}
	return request.Status, nil
}

type changeOpener struct {
	service *controlplane.GovernanceService
	actions assistantActions
}

func (opener changeOpener) Open(ctx context.Context, proposal application.Proposal) (application.OpenedChange, error) {
	return opener.actions.open(ctx, opener.service, proposal)
}

type changeReader struct {
	repository controlplane.Repository
}

func (reader changeReader) Read(ctx context.Context, id string) (application.ChangeDiff, error) {
	request, err := reader.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return application.ChangeDiff{}, err
	}
	hunks := []application.Hunk{}
	for _, change := range request.Changes {
		for path, entry := range change.Diff {
			hunks = append(hunks, application.Hunk{Path: path, Before: shown(entry.Before), After: shown(entry.After)})
		}
	}
	sort.Slice(hunks, func(i, j int) bool { return hunks[i].Path < hunks[j].Path })
	return application.ChangeDiff{ID: request.ID, Title: request.Title, Status: request.Status, Hunks: hunks}, nil
}

func shown(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

type toolAudit struct {
	repository controlplane.Repository
}

func (audit toolAudit) Record(ctx context.Context, record application.AuditRecord) (string, error) {
	decision := governance.DecisionDeny
	if record.Decision == "ALLOW" {
		decision = governance.DecisionAllow
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), record.Actor, "assistant."+record.Tool, record.Resource, decision, record.Reason,
		map[string]any{
			"tool": record.Tool, "argsSha256": assistantdomain.Digest(record.Args), "resultSha256": assistantdomain.Digest(record.Result),
			"origin": toolOrigin(ctx),
		},
	)
	saved, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}

func loadAssistantModel() (assistantdomain.Model, error) {
	return assistantModel(os.Getenv)
}
