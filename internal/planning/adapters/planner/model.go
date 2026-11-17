package planner

import (
	"context"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// Model is the planner's model port: one prompt in, one reply out. The
// planner builds the prompt, frames tool results as untrusted data and
// digests both sides; a Model only carries the text.
type Model interface {
	// Identity is fixed at startup from operator configuration.
	Identity() Identity
	Complete(ctx context.Context, prompt string) (Reply, error)
}

// Identity names the transport and the model the operator configured.
// Neither value is ever taken from text the model wrote.
type Identity struct {
	// Adapter is the transport: the command's base name, or openai-http.
	Adapter string
	// ConfiguredModel is CONDUIT_MODEL_NAME. It is empty when a model CLI is
	// configured without one.
	ConfiguredModel string
}

// Reply is one model reply. ProviderModel is the id from the provider's
// response metadata; a model CLI has none and leaves it empty.
type Reply struct {
	Text          []byte
	ProviderModel string
}

// CLIModel retains the retired command adapter shape. Complete always refuses
// without invoking its executor or returning a model reply.
type CLIModel struct {
	Argv    []string
	Timeout time.Duration
	// Name is the operator-configured model name, or empty.
	Name string
	// Exec is retained for source compatibility and is never invoked.
	Exec localcmd.ExecFunc
}

func (model CLIModel) Identity() Identity {
	adapter := ""
	if len(model.Argv) > 0 {
		adapter = localcmd.CommandName(model.Argv[0])
	}
	return Identity{Adapter: adapter, ConfiguredModel: cleanModelID(model.Name)}
}

func (model CLIModel) Complete(ctx context.Context, prompt string) (Reply, error) {
	return Reply{}, localcmd.ErrLocalCommandUnsupported
}

// HTTPModel calls an OpenAI-compatible endpoint with the whole prompt in one
// user message.
type HTTPModel struct {
	Client *modelhttp.Client
}

func (model HTTPModel) Identity() Identity {
	return Identity{Adapter: modelhttp.AdapterName, ConfiguredModel: cleanModelID(model.Client.RequestedModel())}
}

func (model HTTPModel) Complete(ctx context.Context, prompt string) (Reply, error) {
	completion, err := model.Client.Complete(ctx, prompt)
	if err != nil {
		return Reply{}, err
	}
	return Reply{Text: []byte(completion.Text), ProviderModel: completion.Model}, nil
}

// turnModel wraps the configured Model for one turn and keeps the last
// provider-reported id, so every audit record and the turn result carry it.
type turnModel struct {
	Model
	provider string
	called   bool
}

func (model *turnModel) Complete(ctx context.Context, prompt string) (Reply, error) {
	reply, err := model.Model.Complete(ctx, prompt)
	model.called = true
	model.provider = cleanModelID(reply.ProviderModel)
	return reply, err
}

// label is the model name bound at Accept.
func (model *turnModel) label() string {
	return ModelLabel(model.Identity().ConfiguredModel, model.provider)
}

// ModelLabel is the model name a planning turn reports, which Accept binds.
// It is built only from operator configuration and provider response
// metadata. With neither it is the explicit unverified marker.
func ModelLabel(configured, provider string) string {
	configured, provider = cleanModelID(configured), cleanModelID(provider)
	switch {
	case configured == "" && provider == "":
		return domain.UnverifiedModelLabel
	case provider == "" || provider == configured:
		return configured
	case configured == "":
		return provider + " (provider reported)"
	default:
		return configured + " (provider reported " + provider + ")"
	}
}

const maxModelIDLength = 128

// cleanModelID bounds an id and drops control characters. A provider's
// response metadata is recorded, but it is still remote input.
func cleanModelID(id string) string {
	id = strings.Join(strings.FieldsFunc(id, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r == ' '
	}), " ")
	if len(id) > maxModelIDLength {
		id = strings.ToValidUTF8(id[:maxModelIDLength], "")
	}
	return id
}
