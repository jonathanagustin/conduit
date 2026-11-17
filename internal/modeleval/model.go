package modeleval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

// Model is the port every evaluated prompt goes through. It is the planner's
// own model port, so the planner under evaluation is the product's planner.
type Model = planner.Model

// PromptDigest is the transcript key: lowercase sha256 hex of the prompt bytes.
func PromptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// Transcript is one recorded exchange. The prompt is kept so a reviewer can
// diff what the product sent; replay refuses a file whose prompt does not
// hash to its key.
type Transcript struct {
	SchemaVersion int    `json:"schemaVersion"`
	PromptSHA256  string `json:"promptSha256"`
	// Cases lists every case that sent this prompt.
	Cases         []string `json:"cases"`
	ProviderModel string   `json:"providerModel,omitempty"`
	Prompt        string   `json:"prompt"`
	Response      string   `json:"response"`
}

// ErrNoRecording is a replayed prompt with no recorded response.
var ErrNoRecording = errors.New("no recorded transcript for this prompt digest")

// MissingRecording names a case whose prompt has no recording. The usual
// cause is a prompt, skill or fixture change since the transcripts were made.
type MissingRecording struct {
	Case         string `json:"case"`
	PromptSHA256 string `json:"promptSha256"`
	// PromptHead is the first line of the prompt, to tell the phases apart.
	PromptHead string `json:"promptHead"`
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

// ModelSlug is the directory and file name for a model id.
func ModelSlug(modelID string) string {
	slug := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(modelID), "-"), "-.")
	if slug == "" {
		slug = "unnamed"
	}
	return slug
}

// TranscriptDir is where a model's transcripts live under a corpus.
func TranscriptDir(corpusDir, modelID string) string {
	return filepath.Join(corpusDir, "transcripts", ModelSlug(modelID))
}

// BaselinePath is a model's committed score file under a corpus.
func BaselinePath(corpusDir, modelID string) string {
	return filepath.Join(corpusDir, "baselines", ModelSlug(modelID)+".json")
}

// LoadTranscripts reads every transcript in dir, keyed by prompt digest.
func LoadTranscripts(dir string) (map[string]Transcript, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("model-eval transcripts: %w", err)
	}
	out := map[string]Transcript{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var transcript Transcript
		if err := json.Unmarshal(raw, &transcript); err != nil {
			return nil, fmt.Errorf("model-eval transcript %s: %w", entry.Name(), err)
		}
		if got := PromptDigest(transcript.Prompt); got != transcript.PromptSHA256 || entry.Name() != got+".json" {
			return nil, fmt.Errorf("model-eval transcript %s: the prompt hashes to %s, not the recorded key", entry.Name(), got)
		}
		out[transcript.PromptSHA256] = transcript
	}
	return out, nil
}

// WriteTranscripts replaces dir with the given transcripts, one file each.
func WriteTranscripts(dir string, transcripts map[string]Transcript) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for digest, transcript := range transcripts {
		transcript.SchemaVersion = SchemaVersion
		sort.Strings(transcript.Cases)
		if err := writeJSON(filepath.Join(dir, digest+".json"), transcript); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// session is the per-run model wrapper. It names the case that is running,
// answers from recordings in replay, records what a live model said, and
// keeps every prompt digest and reply for scoring.
type session struct {
	mu sync.Mutex
	// inner is the live model. It is nil in replay.
	inner Model
	// recorded is the replay source and, in record mode, what was captured.
	recorded map[string]Transcript
	replay   bool
	identity planner.Identity

	currentCase string
	// used maps each prompt digest sent in this run to the cases that sent it.
	used    map[string]map[string]bool
	missing []MissingRecording
	// replies holds the model replies of the current case, in order.
	replies []string
}

func (model *session) Identity() planner.Identity { return model.identity }

func (model *session) begin(caseID string) {
	model.mu.Lock()
	defer model.mu.Unlock()
	model.currentCase = caseID
	model.replies = nil
}

func (model *session) caseReplies() []string {
	model.mu.Lock()
	defer model.mu.Unlock()
	return append([]string(nil), model.replies...)
}

func (model *session) Complete(ctx context.Context, prompt string) (planner.Reply, error) {
	digest := PromptDigest(prompt)
	model.mu.Lock()
	caseID := model.currentCase
	if model.used[digest] == nil {
		model.used[digest] = map[string]bool{}
	}
	model.used[digest][caseID] = true
	transcript, known := model.recorded[digest]
	model.mu.Unlock()

	if !known {
		if model.replay {
			model.mu.Lock()
			model.missing = append(model.missing, MissingRecording{Case: caseID, PromptSHA256: digest, PromptHead: firstLine(prompt)})
			model.mu.Unlock()
			return planner.Reply{}, fmt.Errorf("%w: case %s, prompt sha256 %s", ErrNoRecording, caseID, digest)
		}
		reply, err := model.inner.Complete(ctx, prompt)
		if err != nil {
			return reply, err
		}
		transcript = Transcript{PromptSHA256: digest, Prompt: prompt, Response: string(reply.Text), ProviderModel: reply.ProviderModel}
		model.mu.Lock()
		// One response per digest within a run: a later case that sends the
		// same prompt is scored on the same reply, as replay would score it.
		model.recorded[digest] = transcript
		model.mu.Unlock()
	}
	model.mu.Lock()
	model.replies = append(model.replies, transcript.Response)
	model.mu.Unlock()
	return planner.Reply{Text: []byte(transcript.Response), ProviderModel: transcript.ProviderModel}, nil
}

// usedTranscripts returns the transcripts this run sent, with their cases.
func (model *session) usedTranscripts() map[string]Transcript {
	model.mu.Lock()
	defer model.mu.Unlock()
	out := map[string]Transcript{}
	for digest, cases := range model.used {
		transcript, ok := model.recorded[digest]
		if !ok {
			continue
		}
		transcript.Cases = transcript.Cases[:0:0]
		for id := range cases {
			transcript.Cases = append(transcript.Cases, id)
		}
		sort.Strings(transcript.Cases)
		out[digest] = transcript
	}
	return out
}

func (model *session) digests() []string {
	model.mu.Lock()
	defer model.mu.Unlock()
	out := make([]string, 0, len(model.used))
	for digest := range model.used {
		out = append(out, digest)
	}
	sort.Strings(out)
	return out
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

// Provider is a configured live model and the id its scores are stored under.
type Provider struct {
	Model Model
	ID    string
}

// ErrNoProvider means neither model transport variable is set.
var ErrNoProvider = errors.New("no model provider is configured: set CONDUIT_MODEL_BASE_URL or CONDUIT_MODEL_CMD, exactly as for the Hub")

const defaultCommandTimeout = 90 * time.Second

// ProviderFromEnv resolves the live model with the Hub's own rule:
// CONDUIT_MODEL_BASE_URL selects the OpenAI-compatible HTTP transport,
// CONDUIT_MODEL_CMD a local command, both set is an error.
func ProviderFromEnv(getenv func(string) string) (Provider, error) {
	client, err := modelhttp.FromEnv(getenv)
	if err != nil {
		return Provider{}, err
	}
	if client != nil {
		name := client.RequestedModel()
		if name == "" {
			name = "unnamed"
		}
		return Provider{Model: planner.HTTPModel{Client: client}, ID: modelhttp.AdapterName + "/" + name}, nil
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return Provider{}, ErrNoProvider
	}
	argv, err := localcmd.ParseArgv(raw)
	if err != nil {
		return Provider{}, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
	}
	timeout := defaultCommandTimeout
	if text := strings.TrimSpace(getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			return Provider{}, fmt.Errorf("CONDUIT_MODEL_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	name := strings.TrimSpace(getenv("CONDUIT_MODEL_NAME"))
	model := planner.CLIModel{Argv: argv, Timeout: timeout, Name: name}
	id := model.Identity().Adapter + "/"
	switch preset, ok := localcmd.PresetFromArgv(argv, getenv("CONDUIT_AGENT_PROVIDER")); {
	case ok:
		// agent-exec names its engine; the built-in one is "deterministic".
		id = "agent-exec/" + preset.Name
	case name != "":
		id += name
	default:
		id += "unnamed"
	}
	return Provider{Model: model, ID: id}, nil
}
