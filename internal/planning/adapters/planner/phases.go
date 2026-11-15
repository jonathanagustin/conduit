package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// Audit actions for the three model phases of a planning session.
const (
	ActionInspect   = "planning.inspect"
	ActionQuestions = "planning.questions"
	ActionPlan      = "planning.plan"
)

const (
	phaseRounds      = 8
	maxQuestions     = 8
	maxChoices       = 12
	maxTreeInPrompt  = 200
	readFilesNudge   = "You have not called read_file yet. Call read_file on the README, the package manifests, and any existing build files from the tree before you answer.\n"
	readFilesRequire = `Before you answer you MUST call read_file on the key files that exist in the tree: the README, the package manifests (for example package.json, go.mod, pyproject.toml, requirements.txt, pom.xml, Gemfile, Cargo.toml), and the existing build files (for example Dockerfile, docker-compose.yml, Makefile, CI workflows). Do not guess what a file contains.
You may call tools by replying with {"toolCalls":[{"name":"read_file","arguments":{"path":"relative/path"}},{"name":"inspect_repo","arguments":{}}]}.
Reply with only that JSON object when you need tools, with at most 4 tool calls per reply.
`
)

// afterInspection runs once, in the turn that inspected the repository. It
// asks the model for Needs Input questions and, when it has none, writes
// the plan in the same turn.
func (planner Planner) afterInspection(ctx context.Context, session *domain.Session, message string, checkout repoclone.Checkout, tools *packagingtools.Service, result *application.TurnResult) (*application.TurnResult, error) {
	view := domain.CloneSession(session)
	view.Inspection = result.Inspection
	questions, err := planner.askQuestions(ctx, view, message, checkout, tools)
	if err != nil {
		return result, err
	}
	if len(questions) > 0 {
		result.NeedsInput = questions
		result.Messages = append(result.Messages, domain.Message{
			Role: domain.RoleModel,
			Text: fmt.Sprintf("I have %d question(s) before I can write the plan.", len(questions)),
		})
		return result, nil
	}
	return planner.writeInto(ctx, view, message, checkout, tools, result)
}

// planTurn writes the plan for a session that was already inspected and has
// no unanswered required question.
func (planner Planner) planTurn(ctx context.Context, session *domain.Session, message string, checkout repoclone.Checkout, tools *packagingtools.Service) (*application.TurnResult, error) {
	if session.ResolvedCommit != "" && checkout.SHA != session.ResolvedCommit {
		return nil, fmt.Errorf("the ref %q now resolves to %s, not the inspected commit %s; start a new session", session.Intake.Ref, checkout.SHA, session.ResolvedCommit)
	}
	result := &application.TurnResult{Model: localcmd.CommandName(planner.Argv[0]), Profile: onboardProfile()}
	return planner.writeInto(ctx, session, message, checkout, tools, result)
}

func (planner Planner) writeInto(ctx context.Context, session *domain.Session, message string, checkout repoclone.Checkout, tools *packagingtools.Service, result *application.TurnResult) (*application.TurnResult, error) {
	plan, err := planner.writePlan(ctx, session, message, checkout, tools)
	if err != nil {
		return result, err
	}
	result.Plan = plan
	return result, nil
}

func (planner Planner) askQuestions(ctx context.Context, session *domain.Session, message string, checkout repoclone.Checkout, tools *packagingtools.Service) ([]domain.Question, error) {
	var questions []domain.Question
	err := planner.runPhase(ctx, session, checkout, tools, ActionQuestions, questionsPrompt(session, message), func(reply []byte) error {
		parsed, err := parseQuestions(reply)
		questions = parsed
		return err
	})
	if err != nil {
		return nil, err
	}
	return questions, nil
}

func (planner Planner) writePlan(ctx context.Context, session *domain.Session, message string, checkout repoclone.Checkout, tools *packagingtools.Service) (*domain.Plan, error) {
	var plan *domain.Plan
	err := planner.runPhase(ctx, session, checkout, tools, ActionPlan, planPrompt(session, message), func(reply []byte) error {
		parsed, err := parsePlan(reply)
		plan = parsed
		return err
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// runPhase drives one model phase: it feeds tool results back until the
// model gives a final reply, hands that reply to parse, and writes one
// audit record whatever the outcome.
func (planner Planner) runPhase(ctx context.Context, session *domain.Session, checkout repoclone.Checkout, tools *packagingtools.Service, action, prompt string, parse func([]byte) error) error {
	adapter := localcmd.CommandName(planner.Argv[0])
	promptDigest := digestText([]byte(prompt))
	var (
		calls    []string
		reads    int
		nudged   bool
		last     []byte
		inspect  = session.Inspection
		needRead = session.Inspection != nil && len(session.Inspection.Tree) > 0
	)
	fail := func(err error) error {
		_ = planner.record(ctx, Record{
			Action: action, SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
			ResponseSHA256: digestText(last), RepoURL: session.Intake.RepoURL,
			ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
			Outcome: "fail", Reason: err.Error(),
		})
		return err
	}
	for round := 0; round < phaseRounds; round++ {
		stdout, err := localcmd.Execute(ctx, planner.Argv, planner.Timeout, []byte(prompt))
		last = stdout
		if err != nil {
			return fail(modelError(err))
		}
		requests, skipped, isTools, parseErr := parseModelReply(stdout)
		if isTools {
			if parseErr != nil {
				return fail(parseErr)
			}
			before := len(calls)
			facts, nextCalls, nextInspect, toolErr := runToolRequests(ctx, tools, requests, calls, inspect, planner.OnTool)
			if toolErr != nil {
				return fail(toolErr)
			}
			calls, inspect = nextCalls, nextInspect
			for _, name := range calls[before:] {
				if name == "read_file" {
					reads++
				}
			}
			prompt += "\n\nTool results:\n" + facts + skippedNote(skipped) + "\nContinue. Call tools again the same way, or give your final answer.\n"
			continue
		}
		if needRead && reads == 0 && !nudged {
			nudged = true
			prompt += "\n\n" + readFilesNudge
			continue
		}
		if err := parse(stdout); err != nil {
			return fail(err)
		}
		return planner.record(ctx, Record{
			Action: action, SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
			ResponseSHA256: digestText(stdout), RepoURL: session.Intake.RepoURL,
			ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
			Outcome: "pass", Reason: phaseReason(action),
		})
	}
	return fail(fmt.Errorf("the model did not finish within %d rounds", phaseRounds))
}

func phaseReason(action string) string {
	if action == ActionQuestions {
		return "asked Needs Input questions"
	}
	return "wrote the packaging plan"
}

func sessionContext(session *domain.Session, message string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\nRef: %s\n", session.Intake.RepoURL, session.Intake.Ref)
	if session.ResolvedCommit != "" {
		fmt.Fprintf(&b, "Commit: %s\n", session.ResolvedCommit)
	}
	if strings.TrimSpace(message) != "" {
		fmt.Fprintf(&b, "Operator message: %s\n", strings.TrimSpace(message))
	}
	if in := session.Inspection; in != nil {
		fmt.Fprintf(&b, "Inspection: language=%s packageManager=%s kind=%s startCommand=%q ports=%v hasDockerfile=%t\n",
			in.Language, in.PackageManager, in.Kind, in.StartCommand, in.Ports, in.HasDockerfile)
		tree := in.Tree
		if len(tree) > maxTreeInPrompt {
			tree = tree[:maxTreeInPrompt]
		}
		fmt.Fprintf(&b, "Tree:\n%s\n", strings.Join(tree, "\n"))
	}
	return b.String()
}

func questionsPrompt(session *domain.Session, message string) string {
	var b strings.Builder
	b.WriteString("You are preparing to package a source repository. Decide what you must ask the operator before you can write a packaging plan.\n")
	b.WriteString(sessionContext(session, message))
	b.WriteString(readFilesRequire)
	b.WriteString(`Ask only what the files cannot tell you. Never ask for a secret, token, password, or key.
Your final answer is one JSON object and nothing else:
{"questions":[{"id":"short_snake_case_id","prompt":"the question","choices":["optional","fixed answers"]}]}
Use "choices" only when the answer is one of a few fixed values. Reply {"questions":[]} when you need nothing.
Do not write the plan, a Dockerfile, or any package.
`)
	return b.String()
}

func planPrompt(session *domain.Session, message string) string {
	var b strings.Builder
	b.WriteString("Write the packaging plan for this source repository.\n")
	b.WriteString(sessionContext(session, message))
	if len(session.NeedsInput) > 0 {
		b.WriteString("Operator answers to your questions:\n")
		answers := map[string]string{}
		for _, answer := range session.Answers {
			answers[answer.ID] = answer.Value
		}
		for _, question := range session.NeedsInput {
			value, ok := answers[question.ID]
			if !ok {
				value = "(not answered)"
			}
			fmt.Fprintf(&b, "- %s: %s\n", question.Label, value)
		}
	}
	if session.Plan != nil && strings.TrimSpace(session.Plan.Text) != "" {
		fmt.Fprintf(&b, "Previous plan, to revise using the operator message:\n%s\n", session.Plan.Text)
	}
	b.WriteString(readFilesRequire)
	b.WriteString(`Your final answer is one JSON object and nothing else:
{"summary":"what the package is and how it runs","baseImage":"the base image approach","buildSteps":["ordered build step"],"ports":[8080],"networkAllow":["host or host:port the service must reach"],"sso":{"required":false,"notes":""},"risks":["known risk"]}
Do not include secrets. Do not write a Dockerfile or run a build; this is a plan the operator reviews and accepts first.
`)
	return b.String()
}

type questionsReply struct {
	Questions *[]struct {
		ID       string   `json:"id"`
		Prompt   string   `json:"prompt"`
		Choices  []string `json:"choices"`
		Required *bool    `json:"required"`
	} `json:"questions"`
}

func parseQuestions(raw []byte) ([]domain.Question, error) {
	var lastErr error
	for _, candidate := range replyObjects(raw) {
		var reply questionsReply
		if err := json.Unmarshal(candidate, &reply); err != nil || reply.Questions == nil {
			continue
		}
		if len(*reply.Questions) > maxQuestions {
			return nil, fmt.Errorf("the model asked %d questions; the limit is %d", len(*reply.Questions), maxQuestions)
		}
		out := []domain.Question{}
		seen := map[string]bool{}
		for _, item := range *reply.Questions {
			id := strings.TrimSpace(item.ID)
			prompt := collapse(item.Prompt)
			if id == "" || prompt == "" {
				lastErr = errors.New("a question needs an id and a prompt")
				break
			}
			if seen[id] {
				lastErr = fmt.Errorf("question id %q is repeated", id)
				break
			}
			seen[id] = true
			choices := uniqueTrimmed(item.Choices)
			if len(choices) > maxChoices {
				choices = choices[:maxChoices]
			}
			question := domain.Question{ID: id, Name: id, Label: prompt, Type: domain.QuestionString, Required: item.Required == nil || *item.Required}
			if len(choices) > 0 {
				question.Type = domain.QuestionSelect
				question.Options = choices
			}
			out = append(out, question)
		}
		if lastErr != nil {
			return nil, fmt.Errorf("the model returned invalid questions: %w", lastErr)
		}
		return out, nil
	}
	return nil, errors.New("the model did not return a questions object")
}

type planReply struct {
	Summary      string   `json:"summary"`
	BaseImage    string   `json:"baseImage"`
	BuildSteps   []string `json:"buildSteps"`
	Ports        []int    `json:"ports"`
	NetworkAllow []string `json:"networkAllow"`
	SSO          struct {
		Required bool   `json:"required"`
		Notes    string `json:"notes"`
	} `json:"sso"`
	Risks []string `json:"risks"`
}

// canonicalPlan fixes field order and shape so the same plan always
// serializes, and so hashes, to the same bytes.
type canonicalPlan struct {
	Summary      string       `json:"summary"`
	BaseImage    string       `json:"baseImage"`
	BuildSteps   []string     `json:"buildSteps"`
	Ports        []int        `json:"ports"`
	NetworkAllow []string     `json:"networkAllow"`
	SSO          canonicalSSO `json:"sso"`
	Risks        []string     `json:"risks"`
}

type canonicalSSO struct {
	Required bool   `json:"required"`
	Notes    string `json:"notes"`
}

func parsePlan(raw []byte) (*domain.Plan, error) {
	for _, candidate := range replyObjects(raw) {
		var reply planReply
		if err := json.Unmarshal(candidate, &reply); err != nil || collapse(reply.Summary) == "" {
			continue
		}
		return canonicalize(reply)
	}
	return nil, errors.New("the model did not return a plan object")
}

func canonicalize(reply planReply) (*domain.Plan, error) {
	plan := canonicalPlan{
		Summary:      collapse(reply.Summary),
		BaseImage:    collapse(reply.BaseImage),
		BuildSteps:   []string{},
		Ports:        []int{},
		NetworkAllow: uniqueTrimmed(reply.NetworkAllow),
		SSO:          canonicalSSO{Required: reply.SSO.Required, Notes: collapse(reply.SSO.Notes)},
		Risks:        []string{},
	}
	if plan.BaseImage == "" {
		return nil, errors.New("the plan needs a base image approach")
	}
	for _, step := range reply.BuildSteps {
		if step = collapse(step); step != "" {
			plan.BuildSteps = append(plan.BuildSteps, step)
		}
	}
	if len(plan.BuildSteps) == 0 {
		return nil, errors.New("the plan needs at least one build step")
	}
	for _, risk := range reply.Risks {
		if risk = collapse(risk); risk != "" {
			plan.Risks = append(plan.Risks, risk)
		}
	}
	seen := map[int]bool{}
	for _, port := range reply.Ports {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("the plan has an invalid port %d", port)
		}
		if !seen[port] {
			seen[port] = true
			plan.Ports = append(plan.Ports, port)
		}
	}
	sort.Ints(plan.Ports)
	sort.Strings(plan.NetworkAllow)
	text, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	out := domain.Plan{Text: string(text)}
	out.SHA256 = domain.PlanHash(out)
	return &out, nil
}

// replyObjects returns the JSON objects in a model reply, the whole reply
// first and then fenced or embedded objects, latest first.
func replyObjects(raw []byte) [][]byte {
	var out [][]byte
	if trimmed := strings.TrimSpace(string(raw)); strings.HasPrefix(trimmed, "{") {
		out = append(out, []byte(trimmed))
	}
	return append(out, jsonCandidates(raw)...)
}

func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func uniqueTrimmed(items []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		item = collapse(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// onboardProfile snapshots the packaging skill the planner works from, so
// Accept can bind the exact skill version and hash the plan was written under.
func onboardProfile() domain.ProfileRef {
	return domain.ProfileRef{Name: domain.ProfileOnboard, Version: packagingskill.Version, Hash: packagingskill.SHA256()}
}
