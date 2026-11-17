// Package modeleval scores model output for the planning and packaging
// prompts the product builds. It drives the real planner and the real
// package writer loop against a model port that is either a live provider or
// a replay of recorded transcripts keyed by the sha256 of the prompt bytes.
package modeleval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SchemaVersion is the version of the corpus, transcript and score files.
const SchemaVersion = 1

// Capabilities a case can put under test. Each one runs the pipeline up to
// and including that stage, because the product cannot reach a later stage
// without the earlier ones.
const (
	CapabilityInspection = "planning.inspection"
	CapabilityQuestions  = "planning.questions"
	CapabilityPlan       = "planning.plan"
	CapabilityPackage    = "packaging.package"
)

// Score dimensions.
const (
	DimensionInspection = "inspection_accuracy"
	DimensionQuestions  = "question_quality"
	DimensionPlan       = "plan_validity"
	DimensionGates      = "gate_pass_rate"
	DimensionInjection  = "injection_resistance"
)

// Dimensions lists every dimension in report order.
func Dimensions() []string {
	return []string{DimensionInspection, DimensionQuestions, DimensionPlan, DimensionGates, DimensionInjection}
}

// Corpus is a versioned set of cases over committed fixture repositories.
//
// CONDUIT-INVENTED: modeleval.Harness
// Displaced-by: a published or sanitized Apollo model evaluation contract defining cases, scoring and gating.
type Corpus struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	// GateModel is the model id whose transcripts and baseline the replay
	// gate uses when no model is named.
	GateModel string `json:"gateModel"`
	Cases     []Case `json:"cases"`

	// Dir is the directory the corpus was loaded from.
	Dir string `json:"-"`
	// Digest covers corpus.json and every fixture file.
	Digest string `json:"-"`
	// fixtureDigests maps a fixture name to its tree digest.
	fixtureDigests map[string]string
}

// Case pins one fixture, one capability and the facts expected of it.
type Case struct {
	ID         string `json:"id"`
	Fixture    string `json:"fixture"`
	Capability string `json:"capability"`
	// Message is the operator message for the first planning turn.
	Message string `json:"message,omitempty"`
	// DefaultAnswer answers every free-text question the model asks. A
	// select question is answered with its first option.
	DefaultAnswer string      `json:"defaultAnswer,omitempty"`
	Assertions    []Assertion `json:"assertions"`
}

// Assertion is one machine-checkable expected fact. Which fields are read
// depends on Kind; see check in score.go.
type Assertion struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Dimension string `json:"dimension,omitempty"`
	// Source says which fixture file the expected fact was read from. It is
	// required, so no expectation is written down from a model's answer.
	Source   string   `json:"source"`
	Value    string   `json:"value,omitempty"`
	Bool     *bool    `json:"bool,omitempty"`
	Ports    []int    `json:"ports,omitempty"`
	Count    *int     `json:"count,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Patterns []string `json:"patterns,omitempty"`

	compiled []*regexp.Regexp
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)

// LoadCorpus reads corpus.json and digests the fixtures under dir.
func LoadCorpus(dir string) (*Corpus, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "corpus.json"))
	if err != nil {
		return nil, fmt.Errorf("model-eval corpus: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var corpus Corpus
	if err := decoder.Decode(&corpus); err != nil {
		return nil, fmt.Errorf("model-eval corpus: %w", err)
	}
	if corpus.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("model-eval corpus: schemaVersion %d is not supported (want %d)", corpus.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(corpus.Version) == "" || len(corpus.Cases) == 0 {
		return nil, fmt.Errorf("model-eval corpus: version and at least one case are required")
	}
	corpus.Dir = dir
	corpus.fixtureDigests = map[string]string{}
	seen := map[string]bool{}
	for index := range corpus.Cases {
		item := &corpus.Cases[index]
		if !idPattern.MatchString(item.ID) || seen[item.ID] {
			return nil, fmt.Errorf("model-eval corpus: case id %q is invalid or repeated", item.ID)
		}
		seen[item.ID] = true
		if !idPattern.MatchString(item.Fixture) {
			return nil, fmt.Errorf("model-eval corpus: case %s: fixture %q is invalid", item.ID, item.Fixture)
		}
		if stageOf(item.Capability) < 0 {
			return nil, fmt.Errorf("model-eval corpus: case %s: unknown capability %q", item.ID, item.Capability)
		}
		if len(item.Assertions) == 0 {
			return nil, fmt.Errorf("model-eval corpus: case %s has no assertions", item.ID)
		}
		seenAssertion := map[string]bool{}
		for a := range item.Assertions {
			assertion := &item.Assertions[a]
			if !idPattern.MatchString(assertion.ID) || seenAssertion[assertion.ID] {
				return nil, fmt.Errorf("model-eval corpus: case %s: assertion id %q is invalid or repeated", item.ID, assertion.ID)
			}
			seenAssertion[assertion.ID] = true
			if err := assertion.prepare(); err != nil {
				return nil, fmt.Errorf("model-eval corpus: case %s: assertion %s: %w", item.ID, assertion.ID, err)
			}
			if stageOfKind(assertion.Kind) > stageOf(item.Capability) {
				return nil, fmt.Errorf("model-eval corpus: case %s: assertion %s (%s) needs a later stage than %s", item.ID, assertion.ID, assertion.Kind, item.Capability)
			}
		}
		if _, ok := corpus.fixtureDigests[item.Fixture]; !ok {
			digest, err := treeDigest(corpus.FixtureDir(item.Fixture))
			if err != nil {
				return nil, fmt.Errorf("model-eval corpus: case %s: %w", item.ID, err)
			}
			corpus.fixtureDigests[item.Fixture] = digest
		}
	}
	sum := sha256.New()
	sum.Write(raw)
	names := make([]string, 0, len(corpus.fixtureDigests))
	for name := range corpus.fixtureDigests {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(sum, "\x00%s\x00%s", name, corpus.fixtureDigests[name])
	}
	corpus.Digest = hex.EncodeToString(sum.Sum(nil))
	return &corpus, nil
}

// FixtureDir is the committed repository tree for a fixture.
func (corpus *Corpus) FixtureDir(name string) string {
	return filepath.Join(corpus.Dir, "fixtures", name)
}

// FixtureCommit is a stable 40-hex stand-in for a commit id, derived from
// the fixture's files. A fixture is plain files, not a git repository.
func (corpus *Corpus) FixtureCommit(name string) string {
	return corpus.fixtureDigests[name][:40]
}

// treeDigest hashes every file path and content under root in sorted order.
func treeDigest(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("fixture %s is not a directory", root)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("fixture %s contains a symlink: %s", root, path)
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("fixture %s is empty", root)
	}
	sort.Strings(files)
	sum := sha256.New()
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(root, path)
		content := sha256.Sum256(data)
		fmt.Fprintf(sum, "%s\x00%s\n", filepath.ToSlash(rel), hex.EncodeToString(content[:]))
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func stageOf(capability string) int {
	switch capability {
	case CapabilityInspection:
		return 0
	case CapabilityQuestions:
		return 1
	case CapabilityPlan:
		return 2
	case CapabilityPackage:
		return 3
	}
	return -1
}
