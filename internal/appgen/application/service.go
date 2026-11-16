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
}

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
	ID             string
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

// Store keeps generation results for the read-only view.
type Store interface {
	Save(ctx context.Context, result Result) error
	Get(ctx context.Context, id string) (Result, bool, error)
	List(ctx context.Context) ([]Result, error)
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
	spec, provenance, err := service.Generator.Generate(ctx, domain.Request{
		Name: name, Description: description, Attachments: bound,
		PlanRevisionID: input.PlanRevisionID, PlanDigest: input.PlanDigest,
		AcceptedPlan: input.AcceptedPlan, Conversation: append([]domain.ConversationMessage(nil), input.Conversation...),
	})
	result := Result{
		ID: service.identity(actor, name, provenance.PromptSHA256), Label: domain.Label(provenance),
		Name: name, Description: description, Owner: actor, Provenance: provenance, AttachmentSHA256: digests,
	}
	if err != nil {
		result.Error = err.Error()
		result.Gates = failThenSkip(GateSchema, err.Error())
		return service.finish(ctx, result, Audit{
			Adapter: provenance.Adapter, PromptSHA256: provenance.PromptSHA256, ResponseSHA256: provenance.ResponseSHA256,
			Outcome: domain.OutcomeFail, Reason: err.Error(),
		})
	}
	if spec.Name != name {
		reason := "spec name does not match the request"
		result.Spec = &spec
		result.Error = reason
		result.Gates = failThenSkip(GateSchema, reason)
		return service.finish(ctx, result, service.auditFrom(result, false, "", false, ""))
	}
	result.Spec = &spec
	gates := []domain.Gate{{Name: GateSchema, Outcome: domain.OutcomePass, Reason: "spec is one HTTP application object"}}
	if reason, ok := domain.CheckAllowlist(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateAllowlist, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateSecrets, GateResources, GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return service.finish(ctx, result, service.auditFrom(result, false, "", false, ""))
	}
	gates = append(gates, domain.Gate{Name: GateAllowlist, Outcome: domain.OutcomePass, Reason: "source paths and languages are on the allowlist"})
	if reason, ok := domain.ScanSecretsAndSize(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateSecrets, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateResources, GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return service.finish(ctx, result, service.auditFrom(result, false, "", false, ""))
	}
	gates = append(gates, domain.Gate{Name: GateSecrets, Outcome: domain.OutcomePass, Reason: "no secret material and source files are within the size limit"})
	if reason, ok := domain.AssessResources(spec); !ok {
		gates = append(gates, domain.Gate{Name: GateResources, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateBuild, GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return service.finish(ctx, result, service.auditFrom(result, false, "", false, ""))
	}
	gates = append(gates, domain.Gate{Name: GateResources, Outcome: domain.OutcomePass, Reason: "memory and cpu are within the configured ceiling"})
	rendered, err := domain.Render(spec)
	if err != nil {
		result.Gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: err.Error()})
		result.Error = err.Error()
		return service.finish(ctx, result, service.auditFrom(result, false, err.Error(), false, ""))
	}
	result.Files = filesOf(spec, rendered)
	if service.Sandbox == nil {
		reason := "sandbox is not configured"
		gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: reason})
		gates = append(gates, skipped(GateHealth)...)
		result.Gates = gates
		result.Error = reason
		return service.finish(ctx, result, service.auditFrom(result, false, reason, false, ""))
	}
	buildInput := BuildRequest{Spec: spec, Dockerfile: rendered.Dockerfile, Sources: sourceMap(spec)}
	built, err := service.Sandbox.Build(ctx, buildInput)
	if err != nil {
		built = BuildResult{Passed: false, Reason: err.Error()}
	}
	if !built.Passed {
		gates = append(gates, domain.Gate{Name: GateBuild, Outcome: domain.OutcomeFail, Reason: built.Reason})
		gates = append(gates, skipped(GateHealth)...)
		result.Gates = gates
		result.Error = built.Reason
		return service.finish(ctx, result, service.auditFrom(result, false, built.Reason, false, ""))
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
		return service.finish(ctx, result, service.auditFrom(result, true, built.Reason, false, health.Reason))
	}
	gates = append(gates, domain.Gate{Name: GateHealth, Outcome: domain.OutcomePass, Reason: health.Reason})
	result.Gates = gates
	if service.Publish != nil {
		ref, imageDigest, chartDigest, err := service.Publish.Push(ctx, built.ImageID, spec.Name, rendered.ChartValues)
		if err != nil {
			result.Error = err.Error()
			return service.finish(ctx, result, service.auditFrom(result, true, built.Reason, true, health.Reason))
		}
		result.ImageRef = ref
		result.ImageDigest = imageDigest
		result.ChartDigest = chartDigest
		stamped, err := domain.StampImage(rendered, domain.ImageRepository(ref), imageDigest)
		if err != nil {
			result.Error = err.Error()
			return service.finish(ctx, result, service.auditFrom(result, true, built.Reason, true, health.Reason))
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
			return service.finish(ctx, result, service.auditFrom(result, true, built.Reason, true, health.Reason))
		}
	}
	return service.finish(ctx, result, service.auditFrom(result, true, built.Reason, true, health.Reason))
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
	items, err := service.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(items))
	for _, item := range items {
		if owns(actor, item.Owner) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (service Service) finish(ctx context.Context, result Result, event Audit) (Result, error) {
	event.ID = result.ID
	event.AttachmentSHA256 = result.AttachmentSHA256
	event.Files = map[string]string{}
	for _, file := range result.Files {
		event.Files[file.Path] = file.SHA256
	}
	if service.Audit != nil {
		if err := service.Audit.Record(ctx, event); err != nil {
			return Result{}, err
		}
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
		Adapter: result.Provenance.Adapter, PromptSHA256: result.Provenance.PromptSHA256,
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
