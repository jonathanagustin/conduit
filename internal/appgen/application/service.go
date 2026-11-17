// Package application runs a generated HTTP spec through the pre-pipeline
// gates and then hands a passing result to the existing packaging flow.
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

const (
	GateSchema    = "schema"
	GateAllowlist = "allowlist"
	GateSecrets   = "secrets"
	GateResources = "resources"
	GateBuild     = "build"
	GateHealth    = "health"
)

// File is one generated source or template file.
type File struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

// Result is one generation after the gates.
type Result struct {
	ID              string            `json:"id"`
	Label           string            `json:"label"`
	Name            string            `json:"name,omitempty"`
	Description     string            `json:"description,omitempty"`
	Needs           []string          `json:"needs,omitempty"`
	ConfirmRequired bool              `json:"confirmRequired,omitempty"`
	Spec            *domain.Spec      `json:"spec,omitempty"`
	Gates           []domain.Gate     `json:"gates,omitempty"`
	Files           []File            `json:"files,omitempty"`
	Provenance      domain.Provenance `json:"provenance"`
	ImageRef        string            `json:"imageRef,omitempty"`
	ImageDigest     string            `json:"imageDigest,omitempty"`
	ChartDigest     string            `json:"chartDigest,omitempty"`
	RunID           string            `json:"runId,omitempty"`
	PackagingSource string            `json:"packagingSource,omitempty"`
	// AttachmentSHA256 lists the bound intake files, as lowercase sha256 hex.
	AttachmentSHA256 []string `json:"attachmentSha256,omitempty"`
	// Owner is the operator who created the result. Empty means the row
	// predates owner records and is not visible to any operator.
	Owner string `json:"owner,omitempty"`
	Error string `json:"error,omitempty"`
	// RecordedDemo is true when the files are the checked-in demonstration
	// application, served because no model is configured. Nothing was
	// generated from the operator's description.
	RecordedDemo bool `json:"recordedDemo,omitempty"`
	// PlanRevisionID and PlanDigest name the accepted planning revision this
	// generation implements. Both are empty for a generation that is not
	// bound to a planning session. They are stored with the result.
	PlanRevisionID string `json:"planRevisionId,omitempty"`
	PlanDigest     string `json:"planDigest,omitempty"`
	// Attempts lists every model exchange for this generation, in order. The
	// other fields describe the last one.
	Attempts []Attempt `json:"attempts,omitempty"`
}

// Attempt is one model exchange and the gates its reply reached.
type Attempt struct {
	Number     int               `json:"number"`
	Provenance domain.Provenance `json:"provenance"`
	Gates      []domain.Gate     `json:"gates,omitempty"`
	Error      string            `json:"error,omitempty"`
}

const (
	// DefaultRepairAttempts is how many times a failed reply is sent back to
	// the model when the operator sets no other number.
	DefaultRepairAttempts = 2
	// MaxRepairAttempts is the most an operator may configure.
	MaxRepairAttempts = 5
)

// ErrBuildExecutorUnavailable means this deployment cannot build a generated
// application at all. It is a deployment fact and not a fault in the request.
var ErrBuildExecutorUnavailable = errors.New("build executor unavailable in this deployment")

// ErrOwnerConflict means a result with the same id belongs to another operator.
var ErrOwnerConflict = errors.New("generation id belongs to another operator")

// Input is one operator request. Confirm is the governed write.
// AttachmentSHA256 names stored intake files to bind as read-only context.
// PlanRevisionID, PlanDigest, and AcceptedPlan are set together when generation
// implements an accepted planning revision. Conversation carries the planning
// transcript, including requirements that were not in the original description.
type Input struct {
	// Actor is the authenticated operator. It is not taken from the request body.
	Actor            string
	Name             string
	Description      string
	Confirm          bool
	AttachmentSHA256 []string
	PlanRevisionID   string
	PlanDigest       string
	AcceptedPlan     string
	Conversation     []domain.ConversationMessage
}

// Attachments loads stored intake files by digest for one operator. A
// malformed digest, or one that operator did not store, is an error carrying
// a reason. The error does not say whether another operator stored it.
type Attachments interface {
	Resolve(ctx context.Context, actor string, digests []string) ([]domain.Attachment, error)
}

// BuildRequest is the sandboxed build input.
type BuildRequest struct {
	Spec       domain.Spec
	Dockerfile []byte
	Sources    map[string][]byte
}

// BuildResult is the sandboxed build outcome.
type BuildResult struct {
	Passed  bool
	Reason  string
	ImageID string
	// Output is the capped build output. It is sent back to the model after a
	// failure. The gate reason stays one line.
	Output string
}

// HealthResult is the container health outcome.
type HealthResult struct {
	Passed bool
	Reason string
}

// Sandbox builds and starts one image. It does not approve a change.
type Sandbox interface {
	Build(ctx context.Context, input BuildRequest) (BuildResult, error)
	Check(ctx context.Context, built BuildResult, input BuildRequest) (HealthResult, error)
}

// Availability is an optional Sandbox capability. Available returns an error
// that wraps ErrBuildExecutorUnavailable when the sandbox cannot build here.
type Availability interface {
	Available() error
}

// Publisher pushes an image and a chart to the local registry.
type Publisher interface {
	Push(ctx context.Context, imageID, name string, chart []byte) (imageRef, imageDigest, chartDigest string, err error)
}

// Handoff writes the packaging source the existing run reads.
type Handoff interface {
	Prepare(ctx context.Context, runID string, source []byte) error
}

// Audit records the command id, exchange digests, file digests, and gate results.
type Audit struct {
	ConfiguredModel string
	ProviderModel   string
	ID              string
	// Attempt is the 1-based number of the model exchange this record covers.
	Attempt int
	// PlanRevisionID and PlanDigest name the accepted plan, when there is one.
	PlanRevisionID string
	PlanDigest     string
	Adapter        string
	PromptSHA256   string
	ResponseSHA256 string
	Files          map[string]string
	// AttachmentSHA256 is the bound intake file digests, as lowercase hex.
	AttachmentSHA256 []string
	BuildPassed      bool
	BuildReason      string
	HealthPassed     bool
	HealthReason     string
	Outcome          string
	Reason           string
}

// Auditor writes one audit record.
type Auditor interface {
	Record(ctx context.Context, event Audit) error
}

// Denial is a refused read of a generation that belongs to another operator.
// The HTTP response is the same not-found answer a missing id gets.
type Denial struct {
	Actor  string
	ID     string
	Action string
	Reason string
}

// AccessAuditor records that refusal. A nil auditor still hides the row.
type AccessAuditor interface {
	Deny(ctx context.Context, event Denial) error
}

// ActionRead is the audit action for a refused read of one generation.
const ActionRead = "app-generation.read"

// Store keeps generation results. Save refuses, with ErrOwnerConflict, to
// replace a result that another owner holds. List returns only the results of
// one owner, oldest first, and returns none for a blank owner.
type Store interface {
	Save(ctx context.Context, result Result) error
	Get(ctx context.Context, id string) (Result, bool, error)
	List(ctx context.Context, owner string) ([]Result, error)
}

// Service asks the port for a spec and then applies the gates.
type Service struct {
	Generator domain.AppGenerator
	Sandbox   Sandbox
	Publish   Publisher
	Handoff   Handoff
	Audit     Auditor
	Access    AccessAuditor
	Store     Store
	// Attachments supplies bound, read-only context for the generator.
	Attachments Attachments
	Now         func() time.Time
	// RepairAttempts is how many times a reply that fails a gate is sent
	// back to the model with the failure output. Zero means one shot.
	RepairAttempts int
}

func (service Service) Generate(ctx context.Context, input Input) (Result, error) {
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)
	var needs []string
	if name == "" {
		needs = append(needs, "name")
	}
	if description == "" {
		needs = append(needs, "description")
	}
	if len(needs) > 0 {
		return Result{Label: domain.LabelNoModel, Needs: needs}, nil
	}
	if !input.Confirm {
		return Result{Label: domain.LabelNoModel, Name: name, Description: description, ConfirmRequired: true}, nil
	}
	actor := strings.TrimSpace(input.Actor)
	if actor == "" {
		return Result{}, errors.New("generation owner is required")
	}
	if err := requireAcceptedPlan(input); err != nil {
		return Result{}, err
	}
	if service.Generator == nil {
		return Result{Label: domain.LabelNoModel, Name: name, Description: description, Error: domain.ErrModelUnavailable.Error()}, nil
	}
	var bound []domain.Attachment
	if len(input.AttachmentSHA256) > 0 {
		if service.Attachments == nil {
			return Result{Label: domain.LabelNoModel, Name: name, Description: description, Error: "attachments are not configured"}, nil
		}
		loaded, err := service.Attachments.Resolve(ctx, actor, input.AttachmentSHA256)
		if err != nil {
			return Result{Label: domain.LabelNoModel, Name: name, Description: description, Error: err.Error()}, nil
		}
		bound = loaded
	}
	digests := make([]string, 0, len(bound))
	for _, item := range bound {
		digests = append(digests, item.SHA256)
	}
	if checker, ok := service.Sandbox.(Availability); ok {
		// Refuse before the model is called. A reply that can never be built
		// is a spent exchange and a misleading gate failure.
		if err := checker.Available(); err != nil {
			return Result{}, err
		}
	}
	// The plan binding is set once and is the same in every exchange. A repair
	// adds the failure to the request. It never replaces the plan.
	request := domain.Request{
		Name: name, Description: description, Attachments: bound,
		PlanRevisionID: strings.TrimSpace(input.PlanRevisionID), PlanDigest: strings.TrimSpace(input.PlanDigest),
		AcceptedPlan: input.AcceptedPlan, Conversation: append([]domain.ConversationMessage(nil), input.Conversation...),
	}
	var attempts []Attempt
	// surface is what the first well-formed reply to an accepted plan built:
	// its source shape, port and routes. A later reply must keep it.
	var surface *domain.Surface
	id := ""
	for number := 1; ; number++ {
		result, event, repair, err := service.attempt(ctx, request, actor, digests, surface)
		if err != nil {
			return Result{}, err
		}
		if id == "" {
			id = service.identity(actor, name, result.Provenance.PromptSHA256)
		}
		result.ID = id
		result.PlanRevisionID, result.PlanDigest = request.PlanRevisionID, request.PlanDigest
		event.PlanRevisionID, event.PlanDigest = request.PlanRevisionID, request.PlanDigest
		if surface == nil && request.PlanDigest != "" && result.Spec != nil {
			if found, ok := domain.SurfaceOf(*result.Spec); ok {
				surface = &found
			}
		}
		attempts = append(attempts, Attempt{Number: number, Provenance: result.Provenance, Gates: result.Gates, Error: result.Error})
		result.Attempts = append([]Attempt(nil), attempts...)
		event.Attempt = number
		if repair == nil || number > service.RepairAttempts || number > MaxRepairAttempts {
			return service.finish(ctx, result, event)
		}
		// Every attempt is audited. Only the last one is stored.
		if err := service.record(ctx, result, event); err != nil {
			return Result{}, err
		}
		repair.Attempt = number + 1
		request.Repair = repair
	}
}

// attempt is one model exchange and its gates. It returns a repair when the
// reply failed a gate that another reply could pass. A failure of the model
// command, the registry or the handoff is not one, and neither is any failure
// of the recorded demonstration, which gives the same reply every time.
//
// surface is set for a repair of a generation bound to an accepted plan. A
// reply that changes it is refused at the schema gate, before any build: a
// reply that swaps the language or drops a route can build and pass health
// while no longer being the application the plan was accepted for.
func (service Service) attempt(ctx context.Context, request domain.Request, actor string, digests []string, surface *domain.Surface) (Result, Audit, *domain.Repair, error) {
	name, description := request.Name, request.Description
	spec, provenance, err := service.Generator.Generate(ctx, request)
	result := Result{
		Label: domain.Label(provenance), RecordedDemo: provenance.Adapter == domain.AdapterRecorded,
		Name: name, Description: description, Owner: actor, Provenance: provenance, AttachmentSHA256: digests,
	}
	// failed ends the attempt at a gate. output is what the model is shown.
	failed := func(event Audit, gate, output string) (Result, Audit, *domain.Repair, error) {
		if result.RecordedDemo {
			return result, event, nil, nil
		}
		repair := &domain.Repair{Gate: gate, Output: output}
		if result.Spec != nil {
			repair.Previous = domain.SpecJSON(*result.Spec)
		}
		return result, event, repair, nil
	}
	if err != nil {
		result.Error = err.Error()
		result.Gates = failThenSkip(GateSchema, err.Error())
		event := Audit{
			Adapter: provenance.Adapter, ConfiguredModel: provenance.ConfiguredModel, ProviderModel: provenance.ProviderModel, PromptSHA256: provenance.PromptSHA256, ResponseSHA256: provenance.ResponseSHA256,
			Outcome: domain.OutcomeFail, Reason: err.Error(),
		}
		if !errors.Is(err, domain.ErrSpecInvalid) {
			return result, event, nil, nil
		}
		return failed(event, GateSchema, err.Error())
	}
	if spec.Name != name {
		reason := "spec name does not match the request"
		result.Spec = &spec
		result.Error = reason
		result.Gates = failThenSkip(GateSchema, reason)
		return failed(service.auditFrom(result, false, "", false, ""), GateSchema, reason)
	}
	result.Spec = &spec
	if surface != nil {
		if found, ok := domain.SurfaceOf(spec); ok {
			if drift := surface.Drift(found); drift != "" {
				reason := "repaired reply drifted from the accepted plan revision: " + drift
				result.Error = reason
				result.Gates = failThenSkip(GateSchema, reason)
				return failed(service.auditFrom(result, false, "", false, ""), GateSchema, reason)
			}
		}
	}
	gates := []domain.Gate{{Name: GateSchema, Outcome: domain.OutcomePass, Reason: "spec is one HTTP application object"}}
	if reason, ok := domain.CheckAllowlist(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateAllowlist, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateSecrets, GateResources, GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return failed(service.auditFrom(result, false, "", false, ""), GateAllowlist, reason)
	}
	gates = append(gates, domain.Gate{Name: GateAllowlist, Outcome: domain.OutcomePass, Reason: "source paths and languages are on the allowlist"})
	if reason, ok := domain.ScanSecretsAndSize(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateSecrets, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateResources, GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return failed(service.auditFrom(result, false, "", false, ""), GateSecrets, reason)
	}
	gates = append(gates, domain.Gate{Name: GateSecrets, Outcome: domain.OutcomePass, Reason: "no secret material and source files are within the size limit"})
	if reason, ok := domain.AssessResources(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateResources, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return failed(service.auditFrom(result, false, "", false, ""), GateResources, reason)
	}
	gates = append(gates, domain.Gate{Name: GateResources, Outcome: domain.OutcomePass, Reason: "memory and cpu are within the configured ceiling"})
	rendered, err := domain.Render(spec)
	if err != nil {
		result.Gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: err.Error()})
		result.Error = err.Error()
		return failed(service.auditFrom(result, false, err.Error(), false, ""), GateBuild, err.Error())
	}
	result.Files = filesOf(spec, rendered)
	if service.Sandbox == nil {
		reason := "sandbox is not configured"
		gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return result, service.auditFrom(result, false, reason, false, ""), nil, nil
	}
	buildInput := BuildRequest{Spec: spec, Dockerfile: rendered.Dockerfile, Sources: sourceMap(spec)}
	built, err := service.Sandbox.Build(ctx, buildInput)
	if errors.Is(err, ErrBuildExecutorUnavailable) {
		return Result{}, Audit{}, nil, err
	}
	if err != nil {
		built = BuildResult{Passed: false, Reason: err.Error()}
	}
	if !built.Passed {
		gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: built.Reason})
		gates = append(gates, skipped(GateHealth)...)
		result.Gates = gates
		result.Error = built.Reason
		output := built.Output
		if output == "" {
			output = built.Reason
		}
		return failed(service.auditFrom(result, false, built.Reason, false, ""), GateBuild, output)
	}
	gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomePass, Reason: built.Reason})
	health, err := service.Sandbox.Check(ctx, built, buildInput)
	if err != nil {
		health = HealthResult{Passed: false, Reason: err.Error()}
	}
	if !health.Passed {
		gates = append(gates, domain.Gate{Name: GateHealth, Outcome: domain.OutcomeFail, Reason: health.Reason})
		result.Gates = gates
		result.Error = health.Reason
		return failed(service.auditFrom(result, true, built.Reason, false, health.Reason), GateHealth, health.Reason)
	}
	gates = append(gates, domain.Gate{Name: GateHealth, Outcome: domain.OutcomePass, Reason: health.Reason})
	result.Gates = gates
	if service.Publish != nil {
		ref, imageDigest, chartDigest, err := service.Publish.Push(ctx, built.ImageID, spec.Name, rendered.ChartValues)
		if err != nil {
			result.Error = err.Error()
			return result, service.auditFrom(result, true, built.Reason, true, health.Reason), nil, nil
		}
		result.ImageRef = ref
		result.ImageDigest = imageDigest
		result.ChartDigest = chartDigest
		stamped, err := domain.StampImage(rendered, domain.ImageRepository(ref), imageDigest)
		if err != nil {
			result.Error = err.Error()
			return result, service.auditFrom(result, true, built.Reason, true, health.Reason), nil, nil
		}
		rendered = stamped
		result.Files = filesOf(spec, rendered)
	}
	result.RunID = spec.Name + "-1"
	source := domain.PackagingSource(spec, result.ImageRef, result.ImageDigest, description)
	if len(digests) > 0 {
		source = append(source, []byte("attachments: "+strings.Join(digests, ",")+"\n")...)
	}
	result.PackagingSource = string(source)
	if service.Handoff != nil {
		if err := service.Handoff.Prepare(ctx, result.RunID, source); err != nil {
			result.Error = err.Error()
			return result, service.auditFrom(result, true, built.Reason, true, health.Reason), nil, nil
		}
	}
	return result, service.auditFrom(result, true, built.Reason, true, health.Reason), nil, nil
}

func (service Service) Get(ctx context.Context, actor, id string) (Result, bool, error) {
	if service.Store == nil {
		return Result{}, false, errors.New("generation store is not configured")
	}
	result, ok, err := service.Store.Get(ctx, id)
	if err != nil || !ok {
		return Result{}, false, err
	}
	if !owns(actor, result.Owner) {
		if err := service.conceal(ctx, actor, result.ID); err != nil {
			return Result{}, false, err
		}
		return Result{}, false, nil
	}
	return result, true, nil
}

func (service Service) List(ctx context.Context, actor string) ([]Result, error) {
	if service.Store == nil {
		return nil, errors.New("generation store is not configured")
	}
	items, err := service.Store.List(ctx, strings.TrimSpace(actor))
	if err != nil {
		return nil, err
	}
	// The store already selects by owner. This check holds the invariant
	// against a store that does not.
	out := make([]Result, 0, len(items))
	for _, item := range items {
		if owns(actor, item.Owner) {
			out = append(out, item)
		}
	}
	return out, nil
}

// record writes the audit record for one attempt.
func (service Service) record(ctx context.Context, result Result, event Audit) error {
	event.ID = result.ID
	event.AttachmentSHA256 = result.AttachmentSHA256
	event.Files = map[string]string{}
	for _, file := range result.Files {
		event.Files[file.Path] = file.SHA256
	}
	if service.Audit == nil {
		return nil
	}
	return service.Audit.Record(ctx, event)
}

// finish audits the last attempt and stores the result.
func (service Service) finish(ctx context.Context, result Result, event Audit) (Result, error) {
	if err := service.record(ctx, result, event); err != nil {
		return Result{}, err
	}
	if service.Store != nil && result.ID != "" {
		if err := service.Store.Save(ctx, result); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (service Service) auditFrom(result Result, buildPassed bool, buildReason string, healthPassed bool, healthReason string) Audit {
	outcome := domain.OutcomePass
	reason := "gates passed"
	if result.Error != "" {
		outcome = domain.OutcomeFail
		reason = result.Error
	}
	return Audit{
		Adapter: result.Provenance.Adapter, ConfiguredModel: result.Provenance.ConfiguredModel, ProviderModel: result.Provenance.ProviderModel, PromptSHA256: result.Provenance.PromptSHA256,
		ResponseSHA256: result.Provenance.ResponseSHA256, BuildPassed: buildPassed, BuildReason: buildReason,
		HealthPassed: healthPassed, HealthReason: healthReason, Outcome: outcome, Reason: reason,
	}
}

func requireAcceptedPlan(input Input) error {
	revision := strings.TrimSpace(input.PlanRevisionID)
	digest := strings.TrimSpace(input.PlanDigest)
	plan := input.AcceptedPlan
	if revision == "" && digest == "" && strings.TrimSpace(plan) == "" {
		return nil
	}
	if revision == "" || digest == "" || strings.TrimSpace(plan) == "" || domain.AcceptedPlanDigest(plan) != digest {
		return domain.ErrAcceptedPlanDigest
	}
	return nil
}

func (service Service) identity(actor, name, prompt string) string {
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	sum := sha256.Sum256([]byte(actor + "\x00" + name + "\x00" + prompt + "\x00" + now.Format(time.RFC3339Nano)))
	return "gen_" + hex.EncodeToString(sum[:8])
}

func failThenSkip(name, reason string) []domain.Gate {
	gates := []domain.Gate{{Name: name, Outcome: domain.OutcomeFail, Reason: reason}}
	rest := []string{GateSchema, GateAllowlist, GateSecrets, GateResources, GateBuild, GateHealth}
	var pending []string
	seen := false
	for _, item := range rest {
		if item == name {
			seen = true
			continue
		}
		if seen {
			pending = append(pending, item)
		}
	}
	return append(gates, skipped(pending...)...)
}

func skipped(names ...string) []domain.Gate {
	gates := make([]domain.Gate, 0, len(names))
	for _, name := range names {
		gates = append(gates, domain.Gate{Name: name, Outcome: domain.OutcomeSkip, Reason: "an earlier gate failed"})
	}
	return gates
}

func sourceMap(spec domain.Spec) map[string][]byte {
	out := make(map[string][]byte, len(spec.Files))
	for _, file := range spec.Files {
		out[file.Path] = []byte(file.Content)
	}
	return out
}

func filesOf(spec domain.Spec, rendered domain.PackageFiles) []File {
	files := make([]File, 0, len(spec.Files)+4)
	for _, file := range spec.Files {
		raw := []byte(file.Content)
		files = append(files, File{Path: file.Path, Kind: "source", SHA256: domain.Digest(raw), Content: file.Content})
	}
	for _, item := range []struct {
		path, kind string
		raw        []byte
	}{
		{"Dockerfile", "template", rendered.Dockerfile},
		{"values.yaml", "template", rendered.ChartValues},
		{"zarf.yaml", "template", rendered.Zarf},
		{"uds-package.yaml", "template", rendered.Package},
	} {
		files = append(files, File{Path: item.path, Kind: item.kind, SHA256: domain.Digest(item.raw), Content: string(item.raw)})
	}
	for _, chart := range rendered.Chart {
		raw := chart.Body
		files = append(files, File{Path: "chart/" + chart.Path, Kind: "template", SHA256: domain.Digest(raw), Content: string(raw)})
	}
	return files
}
