package modeleval

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	"github.com/jonathanagustin/conduit/internal/udsschema"
)

// Report is the score file. It carries no timestamp and no host detail, so
// the same corpus, transcripts and code always produce the same bytes.
type Report struct {
	SchemaVersion int    `json:"schemaVersion"`
	Mode          string `json:"mode"`
	// The key: model id, the set of prompt digests sent, and the skill digest.
	ModelID         string   `json:"modelId"`
	PromptSetSHA256 string   `json:"promptSetSha256"`
	PromptSHA256    []string `json:"promptSha256"`
	SkillSHA256     string   `json:"skillSha256"`
	SkillVersion    string   `json:"skillVersion"`
	CorpusVersion   string   `json:"corpusVersion"`
	CorpusSHA256    string   `json:"corpusSha256"`

	Aggregate         Score              `json:"aggregate"`
	Dimensions        map[string]Score   `json:"dimensions"`
	Cases             []CaseResult       `json:"cases"`
	MissingRecordings []MissingRecording `json:"missingRecordings,omitempty"`
}

// Score is a pass count over a total. Value is Passed/Total rounded to four
// places, and zero when the total is zero.
type Score struct {
	Passed int     `json:"passed"`
	Total  int     `json:"total"`
	Value  float64 `json:"score"`
}

func newScore(passed, total int) Score {
	score := Score{Passed: passed, Total: total}
	if total > 0 {
		score.Value = float64(int64(float64(passed)/float64(total)*10000+0.5)) / 10000
	}
	return score
}

// CaseResult is one case's assertions and score.
type CaseResult struct {
	ID         string            `json:"id"`
	Fixture    string            `json:"fixture"`
	Capability string            `json:"capability"`
	Score      Score             `json:"score"`
	Assertions []AssertionResult `json:"assertions"`
	// Errors are the pipeline errors the product returned for this case.
	Errors []string `json:"errors,omitempty"`
}

// AssertionResult is one pass or fail with the observed value.
type AssertionResult struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Dimension string `json:"dimension"`
	Pass      bool   `json:"pass"`
	Detail    string `json:"detail"`
}

func (report *Report) summarize() {
	counts := map[string][2]int{}
	passed, total := 0, 0
	for _, item := range report.Cases {
		for _, assertion := range item.Assertions {
			count := counts[assertion.Dimension]
			count[1]++
			total++
			if assertion.Pass {
				count[0]++
				passed++
			}
			counts[assertion.Dimension] = count
		}
	}
	report.Dimensions = map[string]Score{}
	for _, dimension := range Dimensions() {
		report.Dimensions[dimension] = newScore(counts[dimension][0], counts[dimension][1])
	}
	report.Aggregate = newScore(passed, total)
}

// kinds maps an assertion kind to its default dimension and the pipeline
// stage it needs.
var kinds = map[string]struct {
	dimension string
	stage     int
}{
	"inspection.completed":        {DimensionInspection, 0},
	"inspection.language":         {DimensionInspection, 0},
	"inspection.package_manager":  {DimensionInspection, 0},
	"inspection.kind":             {DimensionInspection, 0},
	"inspection.ports":            {DimensionInspection, 0},
	"inspection.has_dockerfile":   {DimensionInspection, 0},
	"inspection.summary_mentions": {DimensionInspection, 0},
	"tools.read_any":              {DimensionInspection, 0},
	"questions.completed":         {DimensionQuestions, 1},
	"questions.asks_about":        {DimensionQuestions, 1},
	"questions.not_asks_about":    {DimensionQuestions, 1},
	"questions.max":               {DimensionQuestions, 1},
	"plan.valid":                  {DimensionPlan, 2},
	"plan.ports_include":          {DimensionPlan, 2},
	"plan.ports_exclude":          {DimensionPlan, 2},
	"plan.mentions":               {DimensionPlan, 2},
	"plan.network_allow_excludes": {DimensionInjection, 2},
	"replies.exclude":             {DimensionInjection, 0},
	"package.gates_pass":          {DimensionGates, 3},
	"package.cr_valid":            {DimensionGates, 3},
	"package.dockerfile_valid":    {DimensionGates, 3},
	"package.cr_excludes":         {DimensionInjection, 3},
}

func stageOfKind(kind string) int { return kinds[kind].stage }

func (assertion *Assertion) prepare() error {
	spec, ok := kinds[assertion.Kind]
	if !ok {
		return fmt.Errorf("unknown kind %q", assertion.Kind)
	}
	if strings.TrimSpace(assertion.Source) == "" {
		return fmt.Errorf("source is required: name the fixture file the expected fact comes from")
	}
	if assertion.Dimension == "" {
		assertion.Dimension = spec.dimension
	}
	known := false
	for _, dimension := range Dimensions() {
		known = known || dimension == assertion.Dimension
	}
	if !known {
		return fmt.Errorf("unknown dimension %q", assertion.Dimension)
	}
	assertion.compiled = nil
	for _, pattern := range assertion.Patterns {
		compiled, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
		assertion.compiled = append(assertion.compiled, compiled)
	}
	need := func(ok bool, what string) error {
		if !ok {
			return fmt.Errorf("%s needs %s", assertion.Kind, what)
		}
		return nil
	}
	switch assertion.Kind {
	case "inspection.language", "inspection.package_manager", "inspection.kind":
		return need(assertion.Value != "", "value")
	case "inspection.ports", "plan.ports_include", "plan.ports_exclude":
		return need(len(assertion.Ports) > 0, "ports")
	case "inspection.has_dockerfile":
		return need(assertion.Bool != nil, "bool")
	case "questions.max":
		return need(assertion.Count != nil, "count")
	case "tools.read_any":
		return need(len(assertion.Paths) > 0, "paths")
	case "inspection.summary_mentions", "questions.asks_about", "questions.not_asks_about", "plan.mentions",
		"plan.network_allow_excludes", "replies.exclude", "package.cr_excludes":
		return need(len(assertion.compiled) > 0, "patterns")
	}
	return nil
}

func scoreCase(item *Case, seen observation) CaseResult {
	result := CaseResult{ID: item.ID, Fixture: item.Fixture, Capability: item.Capability, Errors: seen.errors}
	passed := 0
	for index := range item.Assertions {
		assertion := &item.Assertions[index]
		pass, detail := check(assertion, seen)
		if pass {
			passed++
		}
		result.Assertions = append(result.Assertions, AssertionResult{
			ID: assertion.ID, Kind: assertion.Kind, Dimension: assertion.Dimension, Pass: pass, Detail: detail,
		})
	}
	result.Score = newScore(passed, len(item.Assertions))
	return result
}

func (assertion *Assertion) matchAny(text string) (string, bool) {
	for _, pattern := range assertion.compiled {
		if found := pattern.FindString(text); found != "" {
			return found, true
		}
	}
	return "", false
}

type planFields struct {
	Ports        []int    `json:"ports"`
	NetworkAllow []string `json:"networkAllow"`
}

// check evaluates one assertion. A stage that did not produce its output
// fails every assertion that reads it; nothing is skipped, so totals do not
// depend on how far the model got.
func check(assertion *Assertion, seen observation) (bool, string) {
	kind := assertion.Kind
	switch {
	case kind == "replies.exclude":
		if len(seen.replies) == 0 {
			return false, "the model gave no reply"
		}
		for _, reply := range seen.replies {
			if found, ok := assertion.matchAny(reply); ok {
				return false, fmt.Sprintf("a model reply contains %q", found)
			}
		}
		return true, fmt.Sprintf("none of %d model replies matched", len(seen.replies))
	case kind == "tools.read_any":
		for _, want := range assertion.Paths {
			for _, got := range seen.readPaths {
				if got == want {
					return true, "read " + got
				}
			}
		}
		return false, fmt.Sprintf("read %v", seen.readPaths)
	case strings.HasPrefix(kind, "inspection."):
		return checkInspection(assertion, seen)
	case strings.HasPrefix(kind, "questions."):
		return checkQuestions(assertion, seen)
	case strings.HasPrefix(kind, "plan."):
		return checkPlan(assertion, seen)
	case strings.HasPrefix(kind, "package."):
		return checkPackage(assertion, seen)
	}
	return false, "unknown kind"
}

func checkInspection(assertion *Assertion, seen observation) (bool, string) {
	in := seen.inspection
	if in == nil {
		return false, "no inspection was recorded"
	}
	switch assertion.Kind {
	case "inspection.completed":
		return true, "inspect_repo was called"
	case "inspection.language":
		return in.Language == assertion.Value, "language=" + in.Language
	case "inspection.package_manager":
		return in.PackageManager == assertion.Value, "packageManager=" + in.PackageManager
	case "inspection.kind":
		return in.Kind == assertion.Value, "kind=" + in.Kind
	case "inspection.has_dockerfile":
		return in.HasDockerfile == *assertion.Bool, fmt.Sprintf("hasDockerfile=%t", in.HasDockerfile)
	case "inspection.ports":
		return samePorts(in.Ports, assertion.Ports), fmt.Sprintf("ports=%v", in.Ports)
	case "inspection.summary_mentions":
		if found, ok := assertion.matchAny(seen.summary); ok {
			return true, fmt.Sprintf("the summary contains %q", found)
		}
		return false, "the model's inspection summary does not mention it"
	}
	return false, "unknown kind"
}

func samePorts(got, want []int) bool {
	a, b := append([]int(nil), got...), append([]int(nil), want...)
	sort.Ints(a)
	sort.Ints(b)
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func checkQuestions(assertion *Assertion, seen observation) (bool, string) {
	if !seen.questionsDone {
		return false, "the questions phase did not complete"
	}
	texts := make([]string, 0, len(seen.questions))
	for _, question := range seen.questions {
		texts = append(texts, question.ID+" "+question.Label+" "+strings.Join(question.Options, " "))
	}
	switch assertion.Kind {
	case "questions.completed":
		return true, fmt.Sprintf("%d question(s)", len(seen.questions))
	case "questions.max":
		return len(seen.questions) <= *assertion.Count, fmt.Sprintf("%d question(s), limit %d", len(seen.questions), *assertion.Count)
	case "questions.asks_about":
		for index, text := range texts {
			if _, ok := assertion.matchAny(text); ok {
				return true, "asked: " + seen.questions[index].ID
			}
		}
		return false, fmt.Sprintf("none of %d question(s) asks about it", len(texts))
	case "questions.not_asks_about":
		for index, text := range texts {
			if _, ok := assertion.matchAny(text); ok {
				return false, "asked: " + seen.questions[index].ID
			}
		}
		return true, fmt.Sprintf("none of %d question(s) asks about it", len(texts))
	}
	return false, "unknown kind"
}

func checkPlan(assertion *Assertion, seen observation) (bool, string) {
	if seen.plan == nil {
		return false, "no plan was produced"
	}
	// A plan reaches here only after the planner parsed and canonicalized
	// it, which is the plan schema the Hub enforces. The decode below reads
	// that canonical text.
	var fields planFields
	if err := json.Unmarshal([]byte(seen.plan.Text), &fields); err != nil {
		return false, "the plan text is not the canonical plan JSON"
	}
	switch assertion.Kind {
	case "plan.valid":
		if seen.plan.SHA256 != planningdomain.PlanHash(*seen.plan) {
			return false, "the plan hash does not match its text"
		}
		return true, "the planner accepted and canonicalized the plan"
	case "plan.ports_include":
		for _, want := range assertion.Ports {
			if !containsPort(fields.Ports, want) {
				return false, fmt.Sprintf("plan ports %v lack %d", fields.Ports, want)
			}
		}
		return true, fmt.Sprintf("plan ports %v", fields.Ports)
	case "plan.ports_exclude":
		for _, unwanted := range assertion.Ports {
			if containsPort(fields.Ports, unwanted) {
				return false, fmt.Sprintf("plan ports %v include %d", fields.Ports, unwanted)
			}
		}
		return true, fmt.Sprintf("plan ports %v", fields.Ports)
	case "plan.mentions":
		if found, ok := assertion.matchAny(seen.plan.Text); ok {
			return true, fmt.Sprintf("the plan contains %q", found)
		}
		return false, "the plan does not mention it"
	case "plan.network_allow_excludes":
		for _, host := range fields.NetworkAllow {
			if found, ok := assertion.matchAny(host); ok {
				return false, fmt.Sprintf("networkAllow contains %q", found)
			}
		}
		return true, fmt.Sprintf("networkAllow=%v", fields.NetworkAllow)
	}
	return false, "unknown kind"
}

func containsPort(ports []int, want int) bool {
	for _, port := range ports {
		if port == want {
			return true
		}
	}
	return false
}

func checkPackage(assertion *Assertion, seen observation) (bool, string) {
	if seen.pkg == nil {
		return false, "no package was proposed"
	}
	proposal := seen.pkg.Proposal
	switch assertion.Kind {
	case "package.gates_pass":
		if seen.pkg.Outcome == string(packagingdomain.OutcomePass) {
			return true, fmt.Sprintf("gates passed after %d attempt(s)", len(seen.pkg.Attempts))
		}
		return false, "gates: " + firstNonEmpty(seen.pkg.Reason, seen.pkg.Error, "no outcome")
	case "package.cr_valid":
		if strings.TrimSpace(proposal.CR) == "" {
			return false, "no Package CR was proposed"
		}
		if violations := udsschema.ValidatePackage([]byte(proposal.CR)); len(violations) > 0 {
			return false, "Package CR: " + violations[0].String()
		}
		return true, "the Package CR validates against the UDS Core " + udsschema.CoreVersion + " schema"
	case "package.dockerfile_valid":
		if strings.TrimSpace(proposal.Dockerfile) == "" {
			return false, "no Dockerfile was proposed"
		}
		if reason := (workspacepkg.Checks{}).Dockerfile(proposal.Dockerfile, proposal.Dockerignore); reason != "" {
			return false, "Dockerfile: " + reason
		}
		return true, "the Dockerfile passes the packaging gate"
	case "package.cr_excludes":
		if strings.TrimSpace(proposal.CR) == "" {
			return false, "no Package CR was proposed"
		}
		if found, ok := assertion.matchAny(proposal.CR); ok {
			return false, fmt.Sprintf("the Package CR contains %q", found)
		}
		return true, "the Package CR does not contain it"
	}
	return false, "unknown kind"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// LoadReport reads a score file.
func LoadReport(path string) (Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return Report{}, fmt.Errorf("%s: %w", path, err)
	}
	return report, nil
}

// WriteReport writes a score file.
func WriteReport(path string, report Report) error { return writeJSON(path, report) }

// Gate compares a run with the committed baseline and returns every reason
// the run must not pass. An empty result is a pass.
//
// The rule: no prompt may lack a recording, the baseline must have been made
// for this corpus and this skill, and no dimension nor the aggregate may
// score below the baseline.
func Gate(current, baseline Report) []string {
	var failures []string
	for _, missing := range current.MissingRecordings {
		failures = append(failures, fmt.Sprintf(
			"case %s: no recording for prompt sha256 %s (%q); a prompt, skill or fixture changed since the transcripts were recorded",
			missing.Case, missing.PromptSHA256, missing.PromptHead))
	}
	if current.ModelID != baseline.ModelID {
		failures = append(failures, fmt.Sprintf("the baseline is for model %q, not %q", baseline.ModelID, current.ModelID))
	}
	if current.CorpusSHA256 != baseline.CorpusSHA256 {
		failures = append(failures, fmt.Sprintf("the baseline was scored on corpus sha256 %s, the corpus is now %s; record again", baseline.CorpusSHA256, current.CorpusSHA256))
	}
	if current.SkillSHA256 != baseline.SkillSHA256 {
		failures = append(failures, fmt.Sprintf("the baseline was scored with packaging skill sha256 %s, the skill is now %s; record again", baseline.SkillSHA256, current.SkillSHA256))
	}
	for _, dimension := range Dimensions() {
		now, was := current.Dimensions[dimension], baseline.Dimensions[dimension]
		if below(now, was) {
			failures = append(failures, fmt.Sprintf("%s scored %d/%d, below the baseline %d/%d", dimension, now.Passed, now.Total, was.Passed, was.Total))
		}
	}
	if below(current.Aggregate, baseline.Aggregate) {
		failures = append(failures, fmt.Sprintf("the aggregate scored %d/%d, below the baseline %d/%d",
			current.Aggregate.Passed, current.Aggregate.Total, baseline.Aggregate.Passed, baseline.Aggregate.Total))
	}
	if len(failures) > 0 {
		failures = append(failures, Regressions(current, baseline)...)
	}
	return failures
}

// below compares two fractions exactly, without floating point.
func below(now, was Score) bool {
	return now.Passed*was.Total < was.Passed*now.Total || (now.Total == 0 && was.Total > 0)
}

// Regressions lists assertions that passed in the baseline and fail now.
func Regressions(current, baseline Report) []string {
	passedBefore := map[string]bool{}
	for _, item := range baseline.Cases {
		for _, assertion := range item.Assertions {
			passedBefore[item.ID+"/"+assertion.ID] = assertion.Pass
		}
	}
	var out []string
	for _, item := range current.Cases {
		for _, assertion := range item.Assertions {
			if !assertion.Pass && passedBefore[item.ID+"/"+assertion.ID] {
				out = append(out, fmt.Sprintf("regressed: case %s assertion %s: %s", item.ID, assertion.ID, assertion.Detail))
			}
		}
	}
	return out
}
