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

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/modelcli"
	assistantmodelhttp "github.com/jonathanagustin/conduit/internal/assistant/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadmemory"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	assistantdomain "github.com/jonathanagustin/conduit/internal/assistant/domain"
	assistanthttp "github.com/jonathanagustin/conduit/internal/assistant/httpapi"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	ontologyrepo "github.com/jonathanagustin/conduit/internal/ontology/adapters/repository"
	ontologydomain "github.com/jonathanagustin/conduit/internal/ontology/domain"
)

func mountAssistant(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options, model assistantdomain.Model, cases evalAdder,
) http.Handler {
	governanceService := controlplane.NewGovernanceService(repository)
	threads := hub.AssistantThreads
	if threads == nil {
		// No durable store was handed in. Threads still work, in this process
		// only: they are lost on restart and not shared between replicas.
		threads = threadmemory.New()
	}
	api := assistantAPI{
		repository: repository, governance: governanceService, options: options, hub: hub, model: model, cases: cases,
		threads: threads,
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
	mux.Handle("/", legacy)
	return mux
}

func assistantModel(getenv func(string) string) (assistantdomain.Model, error) {
	getenv = modelconfig.ForCapability(getenv, modelconfig.Assistant)
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if client, err := modelHTTPClient(getenv); err != nil {
		return nil, err
	} else if client != nil {
		return assistantmodelhttp.Adapter{Client: client}, nil
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return nil, nil
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
	return modelcli.Runner{Argv: argv, Timeout: timeout, ModelName: configuredModelName(getenv)}, nil
}

type assistantAPI struct {
	repository controlplane.Repository
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
	hub        Options
	model      assistantdomain.Model
	cases      evalAdder
	threads    application.Threads
}

type evalAdder interface {
	Add(scenario, bound, value, expected string) (string, error)
}

func (api assistantAPI) service(actor application.Actor) application.Service {
	return application.Service{
		Objects:  catalogQuery{governance: api.governance, options: api.options, repository: api.repository},
		Governor: actionGate{repository: api.repository, hub: api.hub},
		Changes:  changeOpener{service: api.governance},
		Diffs:    changeReader{repository: api.repository},
		Audit:    toolAudit{repository: api.repository},
		Model:    api.model,
		Cases:    api.cases,
		Authors:  requestAuthor{repository: api.repository},
		Reviews:  changeReviewer{service: api.governance},
		Threads:  api.threads,
	}
}

func (api assistantAPI) Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error) {
	return api.service(actor).Invoke(ctx, actor, mode, name, arguments)
}

func (api assistantAPI) Converse(ctx context.Context, actor application.Actor, request application.TurnRequest, observer application.Observer) (application.Turn, error) {
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
	request, _, err := reviewer.service.Approve(ctx, id, controlplane.Principal{Username: actor, Teams: roles}, rationale)
	if err != nil {
		return "", err
	}
	return request.Status, nil
}

func (reviewer changeReviewer) Reject(ctx context.Context, id, actor, rationale string, roles []string) (string, error) {
	request, _, err := reviewer.service.Reject(ctx, id, controlplane.Principal{Username: actor, Teams: roles}, rationale)
	if err != nil {
		return "", err
	}
	return request.Status, nil
}

type changeOpener struct {
	service *controlplane.GovernanceService
}

func (opener changeOpener) Open(ctx context.Context, proposal application.Proposal) (application.OpenedChange, error) {
	environmentID := strings.TrimSpace(proposal.Environment)
	installationID := strings.TrimSpace(proposal.InstallationID)
	productID := strings.TrimSpace(proposal.ProductID)
	request, err := opener.service.ProposeHumanChange(ctx, "PLAN", proposal.TargetEntityID+":"+proposal.ActionID, environmentID, []controlplane.EntityChange{{
		ID: "governed-action", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: installationID,
		Request: map[string]any{"actionId": proposal.ActionID, "targetEntityId": proposal.TargetEntityID},
		Diff: map[string]controlplane.ChangeDiffEntry{
			"action": {After: proposal.ActionID},
		},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + environmentID, Name: environmentID},
			"installation": {RID: "installation/" + environmentID + "/" + installationID, Name: installationID},
			"product":      {RID: "product/" + productID, Name: productID},
		},
	}}, controlplane.Principal{Username: proposal.Author}, "Proposed "+proposal.ActionID+" on "+proposal.TargetEntityID, proposal.Reason)
	if err != nil {
		return application.OpenedChange{}, err
	}
	return application.OpenedChange{ID: request.ID, Status: request.Status}, nil
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
