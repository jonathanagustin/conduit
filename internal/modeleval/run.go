package modeleval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

// Modes of a run.
const (
	ModeReplay = "replay"
	ModeRecord = "record"
	ModeLive   = "live"
)

// Options configures one run over a corpus.
type Options struct {
	Mode string
	// ModelID names the transcripts to replay and keys the score file.
	ModelID string
	// Live is the provider for record and live. It is ignored in replay.
	Live Model
	// Transcripts is the replay source. It is ignored in record and live.
	Transcripts map[string]Transcript
}

// Outcome is a finished run: the score report and, for record, the
// transcripts to write.
type Outcome struct {
	Report      Report
	Transcripts map[string]Transcript
}

// observation is everything one case produced that an assertion may read.
type observation struct {
	inspection *planningdomain.Inspection
	// summary is the model's own inspection summary text.
	summary        string
	questions      []planningdomain.Question
	questionsDone  bool
	plan           *planningdomain.Plan
	pkg            *packagingapp.WorkspaceResult
	readPaths      []string
	replies        []string
	errors         []string
	stagesExecuted int
}

// Run evaluates every case of the corpus.
func Run(ctx context.Context, corpus *Corpus, options Options) (Outcome, error) {
	model := &session{used: map[string]map[string]bool{}, recorded: map[string]Transcript{}}
	switch options.Mode {
	case ModeReplay:
		model.replay = true
		model.identity = planner.Identity{Adapter: "replay", ConfiguredModel: options.ModelID}
		for digest, transcript := range options.Transcripts {
			model.recorded[digest] = transcript
		}
	case ModeRecord, ModeLive:
		if options.Live == nil {
			return Outcome{}, ErrNoProvider
		}
		model.inner = options.Live
		model.identity = options.Live.Identity()
	default:
		return Outcome{}, fmt.Errorf("unknown mode %q", options.Mode)
	}
	if strings.TrimSpace(options.ModelID) == "" {
		return Outcome{}, errors.New("a model id is required")
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		Mode:          options.Mode,
		ModelID:       options.ModelID,
		CorpusVersion: corpus.Version,
		CorpusSHA256:  corpus.Digest,
		SkillVersion:  packagingskill.Version,
		SkillSHA256:   packagingskill.SHA256(),
	}
	for index := range corpus.Cases {
		item := &corpus.Cases[index]
		model.begin(item.ID)
		seen := runCase(ctx, corpus, item, model)
		seen.replies = model.caseReplies()
		report.Cases = append(report.Cases, scoreCase(item, seen))
	}
	report.PromptSHA256 = model.digests()
	report.PromptSetSHA256 = digestSet(report.PromptSHA256)
	report.MissingRecordings = model.missing
	report.summarize()
	return Outcome{Report: report, Transcripts: model.usedTranscripts()}, nil
}

func digestSet(digests []string) string {
	sum := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return hex.EncodeToString(sum[:])
}

// fixtureURL is the repository URL written into the prompts. The host is
// reserved and never resolved: the clone function below serves the committed
// fixture directory, so no network is touched.
func fixtureURL(fixture string) string {
	return "https://fixtures.conduit-eval.invalid/" + fixture
}

const fixtureRef = "main"

// runCase drives the product's own planner and package writer for one case.
func runCase(ctx context.Context, corpus *Corpus, item *Case, model *session) observation {
	var seen observation
	audit := &auditLog{}
	checkout := repoclone.Checkout{Dir: corpus.FixtureDir(item.Fixture), SHA: corpus.FixtureCommit(item.Fixture), Host: "fixtures.conduit-eval.invalid"}
	turns := planner.Planner{
		Model: model,
		Audit: audit,
		Clone: func(context.Context, string, string) (repoclone.Checkout, func(), error) { return checkout, nil, nil },
		OnTool: func(name, path string) {
			if name == "read_file" {
				seen.readPaths = append(seen.readPaths, path)
			}
		},
	}
	state := &planningdomain.Session{
		ID:      "eval-" + item.Fixture,
		Profile: planningdomain.ProfileRef{Name: planningdomain.ProfileOnboard},
		Intake:  planningdomain.Intake{RepoURL: fixtureURL(item.Fixture), Ref: fixtureRef},
	}

	// Turn one: inspection, then questions, then the plan when the model
	// asked nothing. This is planner.Turn exactly as the Hub calls it.
	result, err := turns.Turn(ctx, state, item.Message)
	seen.stagesExecuted = 2
	if err != nil {
		seen.errors = append(seen.errors, "turn 1: "+err.Error())
	}
	if result != nil {
		seen.inspection = result.Inspection
		seen.questions = result.NeedsInput
		seen.plan = result.Plan
		for _, message := range result.Messages {
			if message.Role == planningdomain.RoleModel && seen.summary == "" {
				seen.summary = message.Text
			}
		}
		state.ResolvedCommit = result.ResolvedCommit
	}
	seen.questionsDone = audit.passed(planner.ActionQuestions)
	if stageOf(item.Capability) < stageOf(CapabilityPlan) || seen.inspection == nil || !seen.questionsDone {
		return seen
	}

	// Turn two: the operator answers and the planner writes the plan.
	if seen.plan == nil && len(seen.questions) > 0 {
		state.Inspection = seen.inspection
		state.NeedsInput = seen.questions
		state.Answers = answersFor(seen.questions, item.DefaultAnswer)
		result, err = turns.Turn(ctx, state, "")
		if err != nil {
			seen.errors = append(seen.errors, "turn 2: "+err.Error())
		}
		if result != nil {
			seen.plan = result.Plan
		}
	}
	seen.stagesExecuted = 3
	if stageOf(item.Capability) < stageOf(CapabilityPackage) || seen.plan == nil {
		return seen
	}

	// Package: the product's WorkspacePackager loop, with its repairs and
	// its Dockerfile and Package CR gates. zarf lint and create are not run.
	packaged, err := writePackage(ctx, model, state.ID, item.Fixture, *seen.plan)
	if err != nil {
		seen.errors = append(seen.errors, "package: "+err.Error())
	}
	seen.pkg = &packaged
	seen.stagesExecuted = 4
	return seen
}

// answersFor answers every question deterministically: the first option of
// a select, the case's default for anything else.
func answersFor(questions []planningdomain.Question, fallback string) []planningdomain.Answer {
	if strings.TrimSpace(fallback) == "" {
		fallback = "Use the defaults documented in the repository."
	}
	answers := make([]planningdomain.Answer, 0, len(questions))
	for _, question := range questions {
		value := fallback
		if len(question.Options) > 0 {
			value = question.Options[0]
		}
		answers = append(answers, planningdomain.Answer{ID: question.ID, Value: value})
	}
	return answers
}

// auditLog collects the planner's per-phase audit records.
type auditLog struct{ records []planner.Record }

func (log *auditLog) Record(_ context.Context, record planner.Record) error {
	log.records = append(log.records, record)
	return nil
}

func (log *auditLog) passed(action string) bool {
	for _, record := range log.records {
		if record.Action == action && record.Outcome == "pass" {
			return true
		}
	}
	return false
}

// evalImageDigest is the placeholder digest `packaging-workflow plan compute`
// passes before an image exists. It is part of the package prompt.
const evalImageDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"

func writePackage(ctx context.Context, model Model, runID, fixture string, plan planningdomain.Plan) (packagingapp.WorkspaceResult, error) {
	packager := packagingapp.WorkspacePackager{
		Writer: packageWriter{model: model},
		Checks: offlineChecks{},
		Store:  &memoryWorkspace{},
	}
	// The same context `packaging-workflow plan compute` builds: the run id,
	// a placeholder image and the accepted plan as a bound attachment.
	return packager.WritePackage(ctx, "", "", fixtureRef, "eval", packagingdomain.PackagingContext{
		RunID:            runID,
		ImageRef:         "ghcr.io/example/" + fixture + ":v1",
		ImageDigest:      evalImageDigest,
		AttachmentSHA256: []string{plan.SHA256},
		Attachments:      []packagingdomain.Attachment{{Name: "accepted-plan.json", SHA256: plan.SHA256, Content: plan.Text}},
	})
}

// packageWriter is the package-writer port over a Model. It is the body of
// the product's packagingworkflow/adapters/modelhttp.Adapter with the model
// port in place of the HTTP client: the prompt is domain.Prompt and the
// parser is domain.ParsePackageProposal, both the product's own.
type packageWriter struct{ model Model }

func (writer packageWriter) ProposePackage(ctx context.Context, input packagingdomain.PackagingContext) (packagingdomain.PackageProposal, packagingdomain.Provenance, error) {
	if strings.TrimSpace(input.Task) == "" {
		input.Task = packagingdomain.TaskPackage
	}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	prompt := packagingdomain.Prompt(input)
	identity := writer.model.Identity()
	provenance := packagingdomain.Provenance{
		Adapter: identity.Adapter, ConfiguredModel: identity.ConfiguredModel,
		PromptSHA256: packagingdomain.DigestText([]byte(prompt)),
	}.WithSkillDigest(input)
	reply, err := writer.model.Complete(ctx, prompt)
	provenance.ResponseSHA256 = packagingdomain.DigestText(reply.Text)
	provenance.ProviderModel = reply.ProviderModel
	if err != nil {
		return packagingdomain.PackageProposal{}, provenance, err
	}
	proposal, err := packagingdomain.ParsePackageProposal(reply.Text)
	return proposal, provenance, err
}

// offlineChecks runs the product's Dockerfile and Package CR gates. zarf is
// a binary this harness does not start, so lint and create are not gates
// here and their absence is stated in the report documentation.
type offlineChecks struct{}

func (offlineChecks) Dockerfile(dockerfile, dockerignore string) string {
	return workspacepkg.Checks{}.Dockerfile(dockerfile, dockerignore)
}
func (offlineChecks) PackageCR(raw string) string                            { return workspacepkg.Checks{}.PackageCR(raw) }
func (offlineChecks) Lint(context.Context, string) error                     { return nil }
func (offlineChecks) Create(context.Context, string, string) (string, error) { return "", nil }

// memoryWorkspace stands in for the git workspace: a commit is the digest of
// the files written.
type memoryWorkspace struct{}

func (*memoryWorkspace) Prepare(_ context.Context, dir, _, _, branch string) (string, string, error) {
	return dir, branch, nil
}

func (*memoryWorkspace) Commit(_ context.Context, _ string, files map[string]string, message string) (string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		fmt.Fprintf(sum, "%s\x00%s\x00", name, files[name])
	}
	sum.Write([]byte(message))
	return hex.EncodeToString(sum.Sum(nil))[:40], nil
}

var (
	_ packagingapp.PackageWriter  = packageWriter{}
	_ packagingapp.PackageChecks  = offlineChecks{}
	_ packagingapp.WorkspaceStore = (*memoryWorkspace)(nil)
)
