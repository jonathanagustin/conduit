// Package planner runs one planning turn: clone the intake, let the model
// call inspect_repo and read_file, and record inspection events and audit.
package planner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

const maxRounds = 4

// Auditor records the model exchange for a planning inspection turn.
type Auditor interface {
	Record(ctx context.Context, record Record) error
}

// Record is the digest and tool list written to the audit log.
type Record struct {
	// Action is the audit action. Empty means planning.inspect.
	Action         string
	SessionID      string
	Adapter        string
	PromptSHA256   string
	ResponseSHA256 string
	RepoURL        string
	ResolvedCommit string
	ToolCalls      []string
	Outcome        string
	Reason         string
}

// CloneFunc checks out a repository URL at a ref.
type CloneFunc func(ctx context.Context, repoURL, ref string) (repoclone.Checkout, func(), error)

// Planner is a planning turn runner backed by a local model command and
// the packaging inspect_repo and read_file tools.
type Planner struct {
	Argv    []string
	Timeout time.Duration
	Audit   Auditor
	Clone   CloneFunc
	// OnTool is notified after a successful inspect_repo or read_file call.
	// Path is set only for read_file.
	OnTool func(name, path string)
}

// Turn clones the session intake, lets the model call inspect_repo and
// read_file, and returns inspection snapshots for the session event log.
func (planner Planner) Turn(ctx context.Context, session *domain.Session, message string) (*application.TurnResult, error) {
	if session == nil {
		return nil, fmt.Errorf("planning session is required")
	}
	if session.Profile.Name == domain.ProfileAppgen {
		return application.BuildAppgenTurnResult(session, message, ""), nil
	}
	if len(planner.Argv) == 0 || strings.TrimSpace(planner.Argv[0]) == "" {
		return nil, domain.ErrModelNotConfigured
	}
	if session.Inspection != nil && domain.PendingRequired(session.NeedsInput, session.Answers) {
		// Required questions are still open; the plan waits for the answers.
		return &application.TurnResult{}, nil
	}
	cloneFn := planner.Clone
	if cloneFn == nil {
		cloneFn = repoclone.Clone
	}
	checkout, cleanup, err := cloneFn(ctx, session.Intake.RepoURL, session.Intake.Ref)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	tools := packagingtools.New(packagingtools.WithRepoRoot(checkout.Dir))
	if session.Inspection != nil {
		return planner.planTurn(ctx, session, message, checkout, tools)
	}
	adapter := localcmd.CommandName(planner.Argv[0])
	model := session.Model
	if model == "" {
		model = domain.DefaultModelLabel
	}
	result := &application.TurnResult{ResolvedCommit: checkout.SHA, Model: model, Profile: onboardProfile()}
	prompt := application.GroundRunPrompt(ctx, inspectionPrompt(session, message))
	promptDigest := digestText([]byte(prompt))
	var (
		calls        []string
		inspection   *domain.Inspection
		lastResponse []byte
		events       []*domain.Inspection
	)
	for round := 0; round < maxRounds; round++ {
		stdout, err := localcmd.Execute(ctx, planner.Argv, planner.Timeout, []byte(prompt))
		lastResponse = stdout
		if err != nil {
			_ = planner.record(ctx, Record{
				SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
				ResponseSHA256: digestText(stdout), RepoURL: session.Intake.RepoURL,
				ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
				Outcome: "fail", Reason: err.Error(),
			})
			return result, modelError(err)
		}
		if reported := extractReportedModel(stdout, planner.Argv); reported != "" {
			result.Model = reported
		}
		requests, skipped, isTools, parseErr := parseModelReply(stdout)
		if isTools {
			if parseErr != nil {
				_ = planner.record(ctx, Record{
					SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
					ResponseSHA256: digestText(stdout), RepoURL: session.Intake.RepoURL,
					ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
					Outcome: "fail", Reason: parseErr.Error(),
				})
				return result, parseErr
			}
		} else if parseErr != nil && errors.Is(parseErr, packagingdomain.ErrProposalInvalid) {
			_ = planner.record(ctx, Record{
				SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
				ResponseSHA256: digestText(stdout), RepoURL: session.Intake.RepoURL,
				ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
				Outcome: "fail", Reason: parseErr.Error(),
			})
			return result, parseErr
		} else {
			text := strings.TrimSpace(string(stdout))
			if text != "" {
				result.Messages = append(result.Messages, domain.Message{Role: domain.RoleModel, Text: text})
			}
			break
		}
		facts, nextCalls, nextInspection, toolErr := runToolRequests(ctx, tools, requests, calls, inspection, planner.OnTool)
		calls = nextCalls
		if nextInspection != nil {
			inspection = nextInspection
			events = append(events, cloneInspection(inspection))
		}
		if toolErr != nil {
			_ = planner.record(ctx, Record{
				SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
				ResponseSHA256: digestText(stdout), RepoURL: session.Intake.RepoURL,
				ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
				Outcome: "fail", Reason: toolErr.Error(),
			})
			return result, toolErr
		}
		prompt = prompt + "\n\nTool results:\n" + facts + skippedNote(skipped) + "\nContinue. You may call tools again the same way, or reply with the short inspection summary.\n"
	}
	result.Inspection = cloneInspection(inspection)
	result.InspectionEvents = events
	reason := "inspected the source repository"
	outcome := "pass"
	if inspection == nil {
		outcome = "fail"
		reason = "the model did not call inspect_repo"
	}
	if err := planner.record(ctx, Record{
		SessionID: session.ID, Adapter: adapter, PromptSHA256: promptDigest,
		ResponseSHA256: digestText(lastResponse), RepoURL: session.Intake.RepoURL,
		ResolvedCommit: checkout.SHA, ToolCalls: append([]string(nil), calls...),
		Outcome: outcome, Reason: reason,
	}); err != nil {
		return result, err
	}
	if inspection == nil {
		return result, fmt.Errorf("the model did not call inspect_repo")
	}
	return planner.afterInspection(ctx, session, message, checkout, tools, result)
}

func (planner Planner) record(ctx context.Context, record Record) error {
	if planner.Audit == nil {
		return nil
	}
	return planner.Audit.Record(ctx, record)
}

func runToolRequests(ctx context.Context, tools *packagingtools.Service, requests []packagingdomain.ToolRequest, calls []string, current *domain.Inspection, onTool func(name, path string)) (string, []string, *domain.Inspection, error) {
	var body strings.Builder
	inspection := current
	for _, request := range requests {
		callID, observeErr := application.ToolStarted(ctx, request.Name, request.Arguments)
		if observeErr != nil {
			return body.String(), calls, inspection, observeErr
		}
		switch request.Name {
		case "inspect_repo":
			got, err := tools.InspectRepo(ctx)
			if observeErr := application.ToolFinished(ctx, callID, err == nil); observeErr != nil {
				return body.String(), calls, inspection, observeErr
			}
			if err != nil {
				fmt.Fprintf(&body, "inspect_repo error: %v\n", err)
				continue
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				fmt.Fprintf(&body, "inspect_repo error: %v\n", err)
				continue
			}
			fmt.Fprintf(&body, "inspect_repo: %s\n", encoded)
			calls = append(calls, "inspect_repo")
			inspection = fromRepo(got, calls)
			if onTool != nil {
				onTool("inspect_repo", "")
			}
		case "read_file":
			var args struct {
				Path string `json:"path"`
			}
			if len(request.Arguments) > 0 {
				if err := json.Unmarshal(request.Arguments, &args); err != nil {
					if observeErr := application.ToolFinished(ctx, callID, false); observeErr != nil {
						return body.String(), calls, inspection, observeErr
					}
					fmt.Fprintf(&body, "read_file error: %v\n", err)
					continue
				}
			}
			got, err := tools.ReadFile(ctx, args.Path)
			if observeErr := application.ToolFinished(ctx, callID, err == nil); observeErr != nil {
				return body.String(), calls, inspection, observeErr
			}
			if err != nil {
				fmt.Fprintf(&body, "read_file error: %v\n", err)
				continue
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				fmt.Fprintf(&body, "read_file error: %v\n", err)
				continue
			}
			fmt.Fprintf(&body, "read_file: %s\n", encoded)
			calls = append(calls, "read_file")
			if onTool != nil {
				onTool("read_file", got.Path)
			}
			// Only inspect_repo creates an inspection; a read before it is
			// recorded in calls but leaves the inspection unset.
			if inspection != nil {
				inspection = cloneInspection(inspection)
				inspection.ToolCalls = append([]string(nil), calls...)
			}
		default:
			if observeErr := application.ToolFinished(ctx, callID, false); observeErr != nil {
				return body.String(), calls, inspection, observeErr
			}
			fmt.Fprintf(&body, "%s error: tool is not allowed during planning inspection\n", request.Name)
		}
	}
	return body.String(), calls, inspection, nil
}

func fromRepo(got packagingdomain.RepoInspect, calls []string) *domain.Inspection {
	ports := append([]int(nil), got.Ports...)
	if ports == nil {
		ports = []int{}
	}
	tree := append([]string(nil), got.Tree...)
	if tree == nil {
		tree = []string{}
	}
	return &domain.Inspection{
		Language:       got.Language,
		PackageManager: got.PackageManager,
		Kind:           got.Kind,
		StartCommand:   got.StartCommand,
		Ports:          ports,
		HasDockerfile:  got.HasDockerfile,
		Tree:           tree,
		ToolCalls:      append([]string(nil), calls...),
	}
}

func cloneInspection(inspection *domain.Inspection) *domain.Inspection {
	if inspection == nil {
		return nil
	}
	out := *inspection
	out.Ports = append([]int(nil), inspection.Ports...)
	if out.Ports == nil {
		out.Ports = []int{}
	}
	out.Tree = append([]string(nil), inspection.Tree...)
	if out.Tree == nil {
		out.Tree = []string{}
	}
	out.ToolCalls = append([]string(nil), inspection.ToolCalls...)
	if out.ToolCalls == nil {
		out.ToolCalls = []string{}
	}
	return &out
}

func inspectionPrompt(session *domain.Session, message string) string {
	var b strings.Builder
	b.WriteString("Inspect the bound source repository for this planning session.\n")
	fmt.Fprintf(&b, "Repository: %s\n", session.Intake.RepoURL)
	fmt.Fprintf(&b, "Ref: %s\n", session.Intake.Ref)
	if strings.TrimSpace(message) != "" {
		fmt.Fprintf(&b, "Operator message: %s\n", strings.TrimSpace(message))
	}
	b.WriteString(`You may call tools by replying with {"toolCalls":[{"name":"inspect_repo","arguments":{}},{"name":"read_file","arguments":{"path":"relative/path"}}]}.
Call inspect_repo first. Each reply may contain at most 4 tool calls; extra calls are skipped and you can ask for them again. Use read_file for files you need to understand the project.
Reply with only that JSON object when you need tools.
When you are done inspecting, reply with a short text summary of the language, package manager, kind, start command, ports, and whether a Dockerfile is present. Do not write a plan, package, or Dockerfile.
`)
	return b.String()
}

func digestText(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// maxToolCallsPerReply matches the packaging tool parser's cap. A model that
// asks for more in one reply gets the first ones now and can ask again.
const maxToolCallsPerReply = 4

// capToolCalls trims an oversized {"toolCalls":[...]} reply to the cap so a
// model reading many files at once keeps making progress instead of stalling.
// It also returns how many calls were dropped.
func capToolCalls(raw []byte) ([]byte, int) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &body); err != nil || len(body) != 1 {
		return raw, 0
	}
	var calls []json.RawMessage
	if err := json.Unmarshal(body["toolCalls"], &calls); err != nil || len(calls) <= maxToolCallsPerReply {
		return raw, 0
	}
	trimmed, err := json.Marshal(map[string][]json.RawMessage{"toolCalls": calls[:maxToolCallsPerReply]})
	if err != nil {
		return raw, 0
	}
	return trimmed, len(calls) - maxToolCallsPerReply
}

// skippedNote tells the model how many of its tool calls were dropped by the
// per-reply cap so it can ask for them again.
func skippedNote(skipped int) string {
	if skipped <= 0 {
		return ""
	}
	return fmt.Sprintf("Note: only the first %d tool calls in your reply were run; %d further call(s) were skipped. Ask for them again in your next reply if you still need them.\n", maxToolCallsPerReply, skipped)
}

func parseModelReply(raw []byte) ([]packagingdomain.ToolRequest, int, bool, error) {
	capped, skipped := capToolCalls(raw)
	requests, isTools, err := packagingdomain.ParseToolRequests(capped)
	if isTools || (err != nil && errors.Is(err, packagingdomain.ErrProposalInvalid)) {
		return requests, skipped, isTools, err
	}
	for _, candidate := range jsonCandidates(raw) {
		capped, skipped = capToolCalls(candidate)
		requests, isTools, err = packagingdomain.ParseToolRequests(capped)
		if isTools {
			return requests, skipped, true, err
		}
		if err != nil && errors.Is(err, packagingdomain.ErrProposalInvalid) {
			return requests, skipped, isTools, err
		}
	}
	return nil, 0, false, nil
}

func jsonCandidates(raw []byte) [][]byte {
	text := string(bytes.TrimSpace(raw))
	text = strings.ReplaceAll(text, "```json", "```")
	parts := strings.Split(text, "```")
	var out [][]byte
	seen := map[string]bool{}
	add := func(body string) {
		body = strings.TrimSpace(body)
		if body == "" || seen[body] {
			return
		}
		if !strings.HasPrefix(body, "{") {
			return
		}
		seen[body] = true
		out = append(out, []byte(body))
	}
	for _, part := range parts {
		add(part)
	}
	for _, obj := range balancedObjects([]byte(text)) {
		add(string(obj))
	}
	// Prefer later objects: models often print a final JSON after logs.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func balancedObjects(raw []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		depth := 0
		inString := false
		escape := false
		for j := i; j < len(raw); j++ {
			c := raw[j]
			if inString {
				if escape {
					escape = false
					continue
				}
				if c == '\\' {
					escape = true
					continue
				}
				if c == '"' {
					inString = false
				}
				continue
			}
			switch c {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, raw[i:j+1])
					i = j
					j = len(raw)
				}
			}
		}
	}
	return out
}

func modelError(err error) error {
	switch {
	case errors.Is(err, localcmd.ErrTimeout):
		return fmt.Errorf("planning model timed out: %w", err)
	case errors.Is(err, localcmd.ErrNotFound), errors.Is(err, localcmd.ErrEmpty):
		return fmt.Errorf("%w: %v", domain.ErrModelNotConfigured, err)
	default:
		return fmt.Errorf("planning model failed: %w", err)
	}
}
